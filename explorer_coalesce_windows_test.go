//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

type coalesceHelperProcess struct {
	command *exec.Cmd
	output  *bytes.Buffer
}

func TestExplorerCoalesceHelper(t *testing.T) {
	if os.Getenv("CONVERTME_COALESCE_HELPER") != "1" {
		return
	}

	readyPath := os.Getenv("CONVERTME_COALESCE_READY")
	gatePath := os.Getenv("CONVERTME_COALESCE_GATE")
	reportDir := os.Getenv("CONVERTME_COALESCE_REPORT_DIR")
	queueDir := os.Getenv("CONVERTME_COALESCE_QUEUE_DIR")
	inputPath := os.Getenv("CONVERTME_COALESCE_INPUT")
	mode := os.Getenv("CONVERTME_COALESCE_MODE")
	if readyPath == "" || gatePath == "" || reportDir == "" || queueDir == "" || inputPath == "" || mode == "" {
		t.Fatal("coalescing helper environment is incomplete")
	}

	if err := os.WriteFile(readyPath, []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(gatePath); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for coalescing test gate")
		}
		time.Sleep(10 * time.Millisecond)
	}

	request, coordinator, err := coalesceExplorerQueue(LaunchRequest{
		Mode:  mode,
		Files: []string{inputPath},
	}, queueDir, mode)
	if err != nil {
		t.Fatal(err)
	}
	if !coordinator {
		return
	}

	data, err := json.Marshal(request.Files)
	if err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(reportDir, fmt.Sprintf("owner-%d.json", os.Getpid()))
	if err := os.WriteFile(reportPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestExplorerLaunchesMergeIntoOneCoordinator(t *testing.T) {
	root := t.TempDir()
	gatePath := filepath.Join(root, "start")
	reportDir := filepath.Join(root, "reports")
	queueDir := filepath.Join(root, "queue")
	if err := os.Mkdir(reportDir, 0o700); err != nil {
		t.Fatal(err)
	}

	mode := fmt.Sprintf("coalesce-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	inputs := []string{
		filepath.Join(root, "first image.png"),
		filepath.Join(root, "second image.png"),
		filepath.Join(root, "third image.png"),
	}
	commands := make([]coalesceHelperProcess, 0, len(inputs))
	for index, input := range inputs {
		readyPath := filepath.Join(root, fmt.Sprintf("ready-%d", index))
		command := exec.Command(os.Args[0], "-test.run=^TestExplorerCoalesceHelper$")
		output := &bytes.Buffer{}
		command.Stdout = output
		command.Stderr = output
		command.Env = append(os.Environ(),
			"CONVERTME_COALESCE_HELPER=1",
			"CONVERTME_COALESCE_READY="+readyPath,
			"CONVERTME_COALESCE_GATE="+gatePath,
			"CONVERTME_COALESCE_REPORT_DIR="+reportDir,
			"CONVERTME_COALESCE_QUEUE_DIR="+queueDir,
			"CONVERTME_COALESCE_INPUT="+input,
			"CONVERTME_COALESCE_MODE="+mode,
		)
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, coalesceHelperProcess{command: command, output: output})
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		ready := true
		for index := range inputs {
			if _, err := os.Stat(filepath.Join(root, fmt.Sprintf("ready-%d", index))); err != nil {
				ready = false
				break
			}
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for helper processes")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.WriteFile(gatePath, []byte("start"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, helper := range commands {
		if err := helper.command.Wait(); err != nil {
			t.Fatalf("coalescing helper failed: %v\n%s", err, helper.output.String())
		}
	}

	reports, err := filepath.Glob(filepath.Join(reportDir, "owner-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("expected exactly one coordinator, got %d", len(reports))
	}
	data, err := os.ReadFile(reports[0])
	if err != nil {
		t.Fatal(err)
	}
	var collected []string
	if err := json.Unmarshal(data, &collected); err != nil {
		t.Fatal(err)
	}

	sort.Strings(inputs)
	sort.Strings(collected)
	if len(collected) != len(inputs) {
		t.Fatalf("expected %d coalesced paths, got %d: %v", len(inputs), len(collected), collected)
	}
	for index := range inputs {
		expected, err := filepath.Abs(inputs[index])
		if err != nil {
			t.Fatal(err)
		}
		if collected[index] != expected {
			t.Fatalf("expected coalesced paths %v, got %v", inputs, collected)
		}
	}
}
