//go:build !windows

// Copyright 2026 Aeneas Rekkas
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"bytes"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunStdio_LogsLifecycleOnSignal(t *testing.T) {
	tmpDir := t.TempDir()
	cmd := newStdioLifecycleHelper(t, tmpDir)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	defer stdin.Close()

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	waitForLifecycleLog(t, tmpDir, "stdio starting", 5*time.Second)

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("signal helper: %v", err)
	}

	waitForExit(t, cmd, 5*time.Second)

	logs := readLifecycleLog(t, tmpDir)
	assertLifecycleLog(t, logs, "stdio stopping", cmd.Process.Pid)
	if !strings.Contains(logs, `"reason":"signal: terminated"`) {
		t.Fatalf("expected signal shutdown reason in logs, got:\n%s\nstderr:\n%s", logs, stderr.String())
	}
}

func waitForLifecycleLog(t *testing.T, dataHome, msg string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		path := dataHome + "/lumen/debug.log"
		b, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(b), `"msg":"`+msg+`"`) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for lifecycle log %q", msg)
}
