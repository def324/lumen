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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestStdioLifecycle_Helper(t *testing.T) {
	if os.Getenv("LUMEN_TEST_STDIO_HELPER") != "1" {
		t.Skip("helper only runs when invoked as subprocess")
	}
	if err := runStdio(nil, nil); err != nil {
		t.Fatalf("runStdio: %v", err)
	}
}

func TestRunStdio_LogsLifecycleOnEOF(t *testing.T) {
	tmpDir := t.TempDir()
	cmd := newStdioLifecycleHelper(t, tmpDir)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	_ = stdin.Close()

	waitForExit(t, cmd, 5*time.Second)

	logs := readLifecycleLog(t, tmpDir)
	assertLifecycleLog(t, logs, "stdio starting", cmd.Process.Pid)
	assertLifecycleLog(t, logs, "stdio stopping", cmd.Process.Pid)
	if !strings.Contains(logs, `"reason":"mcp_returned"`) {
		t.Fatalf("expected shutdown reason mcp_returned in logs, got:\n%s\nstderr:\n%s", logs, stderr.String())
	}
}

func newStdioLifecycleHelper(t *testing.T, dataHome string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestStdioLifecycle_Helper")
	cmd.Env = append(os.Environ(),
		"LUMEN_TEST_STDIO_HELPER=1",
		"XDG_DATA_HOME="+dataHome,
		"XDG_CONFIG_HOME="+dataHome,
		"LUMEN_EMBED_MODEL=all-minilm",
	)
	return cmd
}

func waitForExit(t *testing.T, cmd *exec.Cmd, timeout time.Duration) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("process exited with error: %v", err)
		}
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("process did not exit within %s", timeout)
	}
}

func readLifecycleLog(t *testing.T, dataHome string) string {
	t.Helper()
	path := filepath.Join(dataHome, "lumen", "debug.log")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read debug log: %v", err)
	}
	return string(b)
}

func assertLifecycleLog(t *testing.T, logs, msg string, pid int) {
	t.Helper()
	if !strings.Contains(logs, `"msg":"`+msg+`"`) {
		t.Fatalf("expected %q lifecycle log, got:\n%s", msg, logs)
	}
	if !strings.Contains(logs, `"pid":`+strconv.Itoa(pid)) {
		t.Fatalf("expected pid %d in lifecycle log, got:\n%s", pid, logs)
	}
	if !strings.Contains(logs, `"ppid":`) {
		t.Fatalf("expected ppid in lifecycle log, got:\n%s", logs)
	}
}
