//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const (
	explorerBatchQuietPeriod = 750 * time.Millisecond
	explorerBatchMaxWait     = 5 * time.Second
	explorerRecordMaxAge     = 30 * time.Second
)

type explorerLaunchRecord struct {
	CreatedAt time.Time     `json:"createdAt"`
	Request   LaunchRequest `json:"request"`
}

func coalesceExplorerLaunch(request LaunchRequest) (LaunchRequest, bool, error) {
	if (request.Mode != "convert" && request.Mode != "custom") || len(request.Files) == 0 {
		return request, true, nil
	}

	queueDir, operationKey, err := explorerQueue(request)
	if err != nil {
		return LaunchRequest{}, false, err
	}
	return coalesceExplorerQueue(request, queueDir, operationKey)
}

func coalesceExplorerQueue(request LaunchRequest, queueDir, operationKey string) (LaunchRequest, bool, error) {
	recordPath, err := enqueueExplorerLaunch(queueDir, request)
	if err != nil {
		return LaunchRequest{}, false, err
	}

	// Windows mutex ownership is tied to the calling OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	mutexName, err := windows.UTF16PtrFromString(`Local\ConvertMe.Explorer.` + operationKey)
	if err != nil {
		return LaunchRequest{}, false, err
	}
	mutex, mutexErr := windows.CreateMutex(nil, true, mutexName)
	if mutexErr != nil && !errors.Is(mutexErr, windows.ERROR_ALREADY_EXISTS) {
		if mutex != 0 {
			windows.CloseHandle(mutex)
		}
		return LaunchRequest{}, false, fmt.Errorf("create Explorer batch mutex: %w", mutexErr)
	}
	defer windows.CloseHandle(mutex)

	ownsMutex := mutexErr == nil
	if !ownsMutex {
		waitResult, err := windows.WaitForSingleObject(mutex, uint32((explorerBatchMaxWait+explorerBatchQuietPeriod)/time.Millisecond))
		if err != nil {
			return LaunchRequest{}, false, fmt.Errorf("wait for Explorer batch coordinator: %w", err)
		}
		switch waitResult {
		case windows.WAIT_OBJECT_0, windows.WAIT_ABANDONED:
			ownsMutex = true
		case uint32(windows.WAIT_TIMEOUT):
			return LaunchRequest{}, false, errors.New("timed out waiting for Explorer batch coordinator")
		default:
			return LaunchRequest{}, false, fmt.Errorf("unexpected Explorer batch wait result: %d", waitResult)
		}

		if _, err := os.Stat(recordPath); errors.Is(err, os.ErrNotExist) {
			if err := windows.ReleaseMutex(mutex); err != nil {
				return LaunchRequest{}, false, fmt.Errorf("release Explorer batch mutex: %w", err)
			}
			return LaunchRequest{}, false, nil
		} else if err != nil {
			windows.ReleaseMutex(mutex)
			return LaunchRequest{}, false, err
		}
	}

	aggregated, collectErr := collectExplorerLaunches(queueDir, request)
	releaseErr := windows.ReleaseMutex(mutex)
	if collectErr != nil {
		return LaunchRequest{}, false, collectErr
	}
	if releaseErr != nil {
		return LaunchRequest{}, false, fmt.Errorf("release Explorer batch mutex: %w", releaseErr)
	}
	return aggregated, true, nil
}

func explorerQueue(request LaunchRequest) (directory, operationKey string, err error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", "", err
	}
	operationKey = request.Mode
	if request.Mode == "convert" {
		format := NormalizeFormat(request.Format)
		if format == "" {
			return "", "", fmt.Errorf("unsupported output format %q", request.Format)
		}
		operationKey += "-" + string(format)
	}
	return filepath.Join(base, appName, "explorer-queue", operationKey), operationKey, nil
}

func enqueueExplorerLaunch(queueDir string, request LaunchRequest) (string, error) {
	if err := os.MkdirAll(queueDir, 0o755); err != nil {
		return "", err
	}
	data, err := json.Marshal(explorerLaunchRecord{CreatedAt: time.Now(), Request: request})
	if err != nil {
		return "", err
	}

	temp, err := os.CreateTemp(queueDir, "request-*.tmp")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return "", err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}

	recordPath := strings.TrimSuffix(tempPath, ".tmp") + ".json"
	if err := os.Rename(tempPath, recordPath); err != nil {
		return "", err
	}
	return recordPath, nil
}

func collectExplorerLaunches(queueDir string, seed LaunchRequest) (LaunchRequest, error) {
	deadline := time.Now().Add(explorerBatchMaxWait)
	quietSince := time.Now()
	lastSignature := ""

	for {
		signature, err := explorerQueueSignature(queueDir)
		if err != nil {
			return LaunchRequest{}, err
		}
		if signature != lastSignature {
			lastSignature = signature
			quietSince = time.Now()
		}
		if time.Since(quietSince) >= explorerBatchQuietPeriod || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	entries, err := os.ReadDir(queueDir)
	if err != nil {
		return LaunchRequest{}, err
	}
	aggregated := LaunchRequest{Mode: seed.Mode, Format: seed.Format}
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(queueDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return LaunchRequest{}, err
		}
		var record explorerLaunchRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return LaunchRequest{}, fmt.Errorf("read Explorer batch record %s: %w", entry.Name(), err)
		}
		if err := os.Remove(path); err != nil {
			return LaunchRequest{}, err
		}
		if now.Sub(record.CreatedAt) > explorerRecordMaxAge {
			continue
		}
		if record.Request.Mode != seed.Mode || NormalizeFormat(record.Request.Format) != NormalizeFormat(seed.Format) {
			continue
		}
		aggregated.Files = append(aggregated.Files, record.Request.Files...)
	}
	aggregated.Files = uniquePaths(aggregated.Files)
	if len(aggregated.Files) == 0 {
		return LaunchRequest{}, errors.New("Explorer did not provide any current image selections")
	}
	_ = os.Remove(queueDir)
	return aggregated, nil
}

func explorerQueueSignature(queueDir string) (string, error) {
	entries, err := os.ReadDir(queueDir)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return strings.Join(names, "\x00"), nil
}
