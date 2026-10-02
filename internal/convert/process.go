package convert

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Runner starts one engine process and waits for it. Standard output is returned (or sent
// line by line to onLine when it is set). The error carries the end of standard error.
type Runner func(ctx context.Context, executable string, args []string, onLine func(string)) ([]byte, error)

const (
	maxCapturedOutput = 8 << 20
	maxErrorTail      = 64 << 10
)

// ProcessError is a failed engine run together with what the engine printed.
type ProcessError struct {
	Err    error
	Stderr string
}

func (e *ProcessError) Error() string {
	if e.Stderr == "" {
		return e.Err.Error()
	}
	return e.Err.Error() + ": " + e.Stderr
}

func (e *ProcessError) Unwrap() error { return e.Err }

// boundedOutput keeps engine output from growing without limit. With onLine set it passes
// complete lines on and keeps nothing. Otherwise it keeps the first limit bytes when head
// is set, or the last limit bytes when it is not.
type boundedOutput struct {
	mu       sync.Mutex
	limit    int
	head     bool
	data     []byte
	overflow bool
	pending  []byte
	onLine   func(string)
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.onLine != nil {
		w.pending = append(w.pending, p...)
		for {
			i := bytes.IndexAny(w.pending, "\r\n")
			if i < 0 {
				break
			}
			if i > 0 {
				w.onLine(string(w.pending[:i]))
			}
			w.pending = w.pending[i+1:]
		}
		if len(w.pending) > maxErrorTail {
			w.pending = nil
		}
		return len(p), nil
	}
	if w.head {
		room := w.limit - len(w.data)
		if room < len(p) {
			w.overflow = true
			if room > 0 {
				w.data = append(w.data, p[:room]...)
			}
		} else {
			w.data = append(w.data, p...)
		}
		return len(p), nil
	}
	w.data = append(w.data, p...)
	if len(w.data) > w.limit {
		w.data = w.data[len(w.data)-w.limit:]
	}
	return len(p), nil
}

// RunProcess is the real Runner. The engine is started directly with an argument list:
// no shell is involved, so file names are never interpreted as commands.
func RunProcess(ctx context.Context, executable string, args []string, onLine func(string)) ([]byte, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	configureProcess(cmd)
	cmd.Dir = filepath.Dir(executable)
	cmd.Env = childEnvironment(os.Environ())
	cmd.WaitDelay = 3 * time.Second
	stdout := &boundedOutput{limit: maxCapturedOutput, head: true, onLine: onLine}
	stderr := &boundedOutput{limit: maxErrorTail}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	adoptProcess(cmd)
	err := cmd.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, &ProcessError{Err: err, Stderr: strings.TrimSpace(string(stderr.data))}
	}
	if stdout.overflow {
		return nil, errors.New("the engine produced more output than expected")
	}
	return stdout.data, nil
}

// childEnvironment drops variables that change how the engine behaves, such as FFREPORT,
// which makes it write log files next to wherever it runs.
func childEnvironment(env []string) []string {
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "FF") || strings.HasPrefix(upper, "AV_LOG") {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}
