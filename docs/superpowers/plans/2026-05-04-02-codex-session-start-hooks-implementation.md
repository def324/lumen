# Codex Session-Start Hooks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Codex-only automated setup that installs and verifies a Lumen-owned Codex `SessionStart` hook for proactive background indexing.

**Architecture:** Keep the runtime indexing behavior unchanged. Add a small Codex install/doctor CLI surface that manages user-level Codex configuration and delegates indexing to the existing `lumen hook session-start` path. Split pure hook JSON merging from filesystem and command execution so the risky config mutation logic is heavily unit-tested.

**Tech Stack:** Go, Cobra, JSON, Codex CLI hooks, existing Lumen `hook session-start`, existing shell launchers, Go tests.

---

## File Structure

- Create `cmd/codex_hooks.go`: pure functions for building the Codex hook command, detecting Lumen-owned hook entries, and merging `hooks.json` without touching the filesystem.
- Create `cmd/codex_hooks_test.go`: table-driven tests for hook JSON merge behavior.
- Create `cmd/codex.go`: Cobra commands `lumen codex install` and `lumen codex doctor`, path detection, hook file installation, skills symlink setup, and Codex MCP setup through a command runner abstraction.
- Create `cmd/codex_test.go`: tests for path detection, install idempotency in temp homes, doctor status, fake Codex CLI interactions, and launcher command generation.
- Modify `scripts/run.sh`: export `LUMEN_PLUGIN_ROOT` before `exec` so the binary can find the checkout root.
- Modify `scripts/run.bat`: set `LUMEN_PLUGIN_ROOT` before invoking the Windows binary.
- Modify `.codex/INSTALL.md`: document the new `lumen codex install` and `lumen codex doctor` workflow.
- Modify `README.md`: update Codex install notes to include SessionStart hook setup.

## Task 1: Pure Codex Hook JSON Merge

**Files:**
- Create: `cmd/codex_hooks.go`
- Create: `cmd/codex_hooks_test.go`

- [ ] **Step 1: Write failing tests for missing and empty hook files**

Create `cmd/codex_hooks_test.go` with these initial tests:

```go
package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMergeCodexSessionStartHook_MissingDocument(t *testing.T) {
	command := `/Users/franz/.codex/lumen/scripts/run.sh hook session-start lumen --host claude`

	got, changed, err := mergeCodexSessionStartHook(nil, command)
	if err != nil {
		t.Fatalf("mergeCodexSessionStartHook returned error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true for missing document")
	}
	if !json.Valid(got) {
		t.Fatalf("merged document is not valid JSON:\n%s", got)
	}
	text := string(got)
	if !strings.Contains(text, `"SessionStart"`) {
		t.Fatalf("expected SessionStart hook, got:\n%s", text)
	}
	if !strings.Contains(text, command) {
		t.Fatalf("expected command %q in:\n%s", command, text)
	}
	if strings.Contains(text, "compact") {
		t.Fatalf("compact should not be installed as a matcher source:\n%s", text)
	}
}

func TestMergeCodexSessionStartHook_EmptyDocument(t *testing.T) {
	command := `/Users/franz/.codex/lumen/scripts/run.sh hook session-start lumen --host claude`

	got, changed, err := mergeCodexSessionStartHook([]byte("{}"), command)
	if err != nil {
		t.Fatalf("mergeCodexSessionStartHook returned error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true for empty document")
	}
	if !json.Valid(got) {
		t.Fatalf("merged document is not valid JSON:\n%s", got)
	}
	if !strings.Contains(string(got), `"matcher": "startup|resume|clear"`) {
		t.Fatalf("expected startup|resume|clear matcher, got:\n%s", got)
	}
}

func TestCodexLauncherPathForGOOS(t *testing.T) {
	if got := codexLauncherPathForGOOS(`/Users/franz/.codex/lumen`, "darwin"); got != `/Users/franz/.codex/lumen/scripts/run.sh` {
		t.Fatalf("darwin launcher = %q", got)
	}
	if got := codexLauncherPathForGOOS(`C:\Users\franz\.codex\lumen`, "windows"); got != `C:\Users\franz\.codex\lumen\scripts\run.cmd` {
		t.Fatalf("windows launcher = %q", got)
	}
}

func TestCodexSessionStartCommandQuotesLauncher(t *testing.T) {
	launcher := `/Users/franz/Code/lumen checkout/scripts/run.sh`
	got := codexSessionStartCommand(launcher)
	want := `"/Users/franz/Code/lumen checkout/scripts/run.sh" hook session-start lumen --host claude`
	if got != want {
		t.Fatalf("codexSessionStartCommand() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run the new tests and verify they fail**

Run:

```bash
go test ./cmd -run 'TestMergeCodexSessionStartHook_(MissingDocument|EmptyDocument)' -count=1
```

Expected: FAIL with `undefined: mergeCodexSessionStartHook`.
The compiler may also report `undefined: codexLauncherPathForGOOS` and `undefined: codexSessionStartCommand`; those are implemented in the next step.

- [ ] **Step 3: Implement the minimal hook merge logic**

Create `cmd/codex_hooks.go`:

```go
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

const codexSessionStartMatcher = "startup|resume|clear"
const codexHookStatusMessage = "Starting Lumen background indexing"

func codexLauncherPath(pluginRoot string) string {
	return codexLauncherPathForGOOS(pluginRoot, runtime.GOOS)
}

func codexLauncherPathForGOOS(pluginRoot, goos string) string {
	if goos == "windows" {
		return pluginRoot + `\scripts\run.cmd`
	}
	return pluginRoot + "/scripts/run.sh"
}

func codexSessionStartCommand(launcher string) string {
	return strconv.Quote(launcher) + " hook session-start lumen --host claude"
}

func mergeCodexSessionStartHook(raw []byte, command string) ([]byte, bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, false, fmt.Errorf("parse hooks.json: %w", err)
	}
	if doc == nil {
		doc = make(map[string]json.RawMessage)
	}

	hooksObj := make(map[string]json.RawMessage)
	if existing, ok := doc["hooks"]; ok && len(bytes.TrimSpace(existing)) > 0 {
		if err := json.Unmarshal(existing, &hooksObj); err != nil {
			return nil, false, fmt.Errorf("parse hooks object: %w", err)
		}
	}

	var sessionStart []map[string]any
	if existing, ok := hooksObj["SessionStart"]; ok && len(bytes.TrimSpace(existing)) > 0 {
		if err := json.Unmarshal(existing, &sessionStart); err != nil {
			return nil, false, fmt.Errorf("parse SessionStart hooks: %w", err)
		}
	}

	filtered := sessionStart[:0]
	removed := false
	for _, group := range sessionStart {
		if codexHookGroupRunsLumenSessionStart(group) {
			removed = true
			continue
		}
		filtered = append(filtered, group)
	}

	newGroup := map[string]any{
		"matcher": codexSessionStartMatcher,
		"hooks": []map[string]any{
			{
				"type":          "command",
				"command":       command,
				"statusMessage": codexHookStatusMessage,
			},
		},
	}
	filtered = append(filtered, newGroup)

	sessionRaw, err := json.Marshal(filtered)
	if err != nil {
		return nil, false, fmt.Errorf("marshal SessionStart hooks: %w", err)
	}
	hooksObj["SessionStart"] = sessionRaw

	hooksRaw, err := json.Marshal(hooksObj)
	if err != nil {
		return nil, false, fmt.Errorf("marshal hooks object: %w", err)
	}
	doc["hooks"] = hooksRaw

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("marshal hooks.json: %w", err)
	}
	out = append(out, '\n')

	changed := removed || !bytes.Equal(raw, bytes.TrimSpace(out))
	return out, changed, nil
}

func codexHookGroupRunsLumenSessionStart(group map[string]any) bool {
	hooks, ok := group["hooks"].([]any)
	if !ok {
		return false
	}
	for _, item := range hooks {
		hook, ok := item.(map[string]any)
		if !ok {
			continue
		}
		command, _ := hook["command"].(string)
		if isLumenCodexSessionStartCommand(command) {
			return true
		}
	}
	return false
}

func isLumenCodexSessionStartCommand(command string) bool {
	normalized := strings.ReplaceAll(command, `\`, "/")
	return strings.Contains(normalized, "hook session-start lumen") &&
		(strings.Contains(normalized, "/lumen/scripts/run.sh") ||
			strings.Contains(normalized, "/lumen/scripts/run.cmd"))
}
```

- [ ] **Step 4: Run the initial hook merge tests**

Run:

```bash
go test ./cmd -run 'TestMergeCodexSessionStartHook_(MissingDocument|EmptyDocument)' -count=1
```

Expected: PASS.

- [ ] **Step 5: Add preservation, replacement, duplicate, ownership, and malformed JSON tests**

Append these tests to `cmd/codex_hooks_test.go`:

```go
func TestMergeCodexSessionStartHook_PreservesUnrelatedHooks(t *testing.T) {
	command := `/Users/franz/.codex/lumen/scripts/run.sh hook session-start lumen --host claude`
	input := []byte(`{
	  "hooks": {
	    "PreToolUse": [
	      {
	        "matcher": "Bash",
	        "hooks": [
	          {"type": "command", "command": "python3 /tmp/check.py"}
	        ]
	      }
	    ],
	    "SessionStart": [
	      {
	        "matcher": "startup",
	        "hooks": [
	          {"type": "command", "command": "python3 /tmp/session.py"}
	        ]
	      }
	    ]
	  }
	}`)

	got, _, err := mergeCodexSessionStartHook(input, command)
	if err != nil {
		t.Fatalf("mergeCodexSessionStartHook returned error: %v", err)
	}
	text := string(got)
	for _, want := range []string{"python3 /tmp/check.py", "python3 /tmp/session.py", command} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected preserved command %q in:\n%s", want, text)
		}
	}
}

func TestMergeCodexSessionStartHook_ReplacesStaleLumenHook(t *testing.T) {
	command := `/Users/franz/.codex/lumen/scripts/run.sh hook session-start lumen --host claude`
	input := []byte(`{
	  "hooks": {
	    "SessionStart": [
	      {
	        "matcher": "startup|resume|clear|compact",
	        "hooks": [
	          {"type": "command", "command": "\"/old/lumen/scripts/run.sh\" hook session-start lumen --host claude"}
	        ]
	      }
	    ]
	  }
	}`)

	got, _, err := mergeCodexSessionStartHook(input, command)
	if err != nil {
		t.Fatalf("mergeCodexSessionStartHook returned error: %v", err)
	}
	text := string(got)
	if strings.Contains(text, "/old/lumen") {
		t.Fatalf("stale Lumen hook should be removed:\n%s", text)
	}
	if strings.Count(text, "hook session-start lumen") != 1 {
		t.Fatalf("expected exactly one Lumen SessionStart hook:\n%s", text)
	}
	if strings.Contains(text, "compact") {
		t.Fatalf("updated matcher must not include compact:\n%s", text)
	}
}

func TestMergeCodexSessionStartHook_DeduplicatesLumenHooks(t *testing.T) {
	command := `/Users/franz/.codex/lumen/scripts/run.sh hook session-start lumen --host claude`
	input := []byte(`{
	  "hooks": {
	    "SessionStart": [
	      {"matcher": "startup", "hooks": [{"type": "command", "command": "\"/a/lumen/scripts/run.sh\" hook session-start lumen --host claude"}]},
	      {"matcher": "resume", "hooks": [{"type": "command", "command": "\"/b/lumen/scripts/run.sh\" hook session-start lumen --host claude"}]}
	    ]
	  }
	}`)

	got, _, err := mergeCodexSessionStartHook(input, command)
	if err != nil {
		t.Fatalf("mergeCodexSessionStartHook returned error: %v", err)
	}
	if count := strings.Count(string(got), "hook session-start lumen"); count != 1 {
		t.Fatalf("expected one Lumen hook, got %d:\n%s", count, got)
	}
}

func TestMergeCodexSessionStartHook_PreservesForeignSessionStartCommands(t *testing.T) {
	command := `/Users/franz/.codex/lumen/scripts/run.sh hook session-start lumen --host claude`
	foreign := `python3 /tmp/hook-session-start-lumen.py hook session-start lumen --host claude`
	input := []byte(`{
	  "hooks": {
	    "SessionStart": [
	      {"matcher": "startup", "hooks": [{"type": "command", "command": "` + foreign + `"}]}
	    ]
	  }
	}`)

	got, _, err := mergeCodexSessionStartHook(input, command)
	if err != nil {
		t.Fatalf("mergeCodexSessionStartHook returned error: %v", err)
	}
	text := string(got)
	if !strings.Contains(text, foreign) {
		t.Fatalf("foreign SessionStart command should be preserved:\n%s", text)
	}
	if count := strings.Count(text, "hook session-start lumen"); count != 2 {
		t.Fatalf("expected foreign command plus Lumen command, got %d:\n%s", count, text)
	}
}

func TestMergeCodexSessionStartHook_MalformedJSON(t *testing.T) {
	_, changed, err := mergeCodexSessionStartHook([]byte(`{"hooks":`), "cmd")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if changed {
		t.Fatal("malformed JSON must not report changed=true")
	}
}
```

- [ ] **Step 6: Run all hook merge tests**

Run:

```bash
go test ./cmd -run 'Test(CodexLauncher|CodexSessionStartCommand|MergeCodexSessionStartHook)' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit hook merge logic**

Run:

```bash
git add cmd/codex_hooks.go cmd/codex_hooks_test.go
git commit -m "feat(codex): merge session-start hook config"
```

## Task 2: Codex Install and Doctor Commands

**Files:**
- Create: `cmd/codex.go`
- Create or modify: `cmd/codex_test.go`
- Modify: `scripts/run.sh`
- Modify: `scripts/run.bat`

- [ ] **Step 1: Write failing tests for path calculation and launcher commands**

Create `cmd/codex_test.go`:

```go
package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexPathsFromEnv(t *testing.T) {
	home := t.TempDir()
	pluginRoot := filepath.Join(home, ".codex", "lumen")
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("LUMEN_PLUGIN_ROOT", pluginRoot)

	paths, err := resolveCodexPaths()
	if err != nil {
		t.Fatalf("resolveCodexPaths: %v", err)
	}
	if paths.codexHome != filepath.Join(home, ".codex") {
		t.Fatalf("codexHome = %q", paths.codexHome)
	}
	if paths.pluginRoot != pluginRoot {
		t.Fatalf("pluginRoot = %q", paths.pluginRoot)
	}
	if !strings.Contains(paths.hookCommand, "hook session-start lumen --host claude") {
		t.Fatalf("hookCommand = %q", paths.hookCommand)
	}
	if paths.hooksPath != filepath.Join(home, ".codex", "hooks.json") {
		t.Fatalf("hooksPath = %q", paths.hooksPath)
	}
}
```

- [ ] **Step 2: Run the path test and verify it fails**

Run:

```bash
go test ./cmd -run TestCodexPathsFromEnv -count=1
```

Expected: FAIL with `undefined: resolveCodexPaths`.

- [ ] **Step 3: Implement path resolution**

Create `cmd/codex.go` with path helpers only:

```go
package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type codexPaths struct {
	codexHome   string
	pluginRoot  string
	launcher    string
	hookCommand string
	hooksPath   string
	skillsSrc   string
	skillsDst   string
}

func resolveCodexPaths() (codexPaths, error) {
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return codexPaths{}, fmt.Errorf("resolve home directory: %w", err)
		}
		codexHome = filepath.Join(home, ".codex")
	}

	pluginRoot, err := resolveLumenPluginRoot()
	if err != nil {
		return codexPaths{}, err
	}

	launcher := codexLauncherPath(pluginRoot)
	home, _ := os.UserHomeDir()
	return codexPaths{
		codexHome:   codexHome,
		pluginRoot:  pluginRoot,
		launcher:    launcher,
		hookCommand: codexSessionStartCommand(launcher),
		hooksPath:   filepath.Join(codexHome, "hooks.json"),
		skillsSrc:   filepath.Join(pluginRoot, "skills"),
		skillsDst:   filepath.Join(home, ".agents", "skills", "lumen"),
	}, nil
}

func resolveLumenPluginRoot() (string, error) {
	for _, env := range []string{"LUMEN_PLUGIN_ROOT", "CLAUDE_PLUGIN_ROOT", "CURSOR_PLUGIN_ROOT"} {
		if v := os.Getenv(env); v != "" {
			return filepath.Abs(v)
		}
	}
	exe, err := os.Executable()
	if err == nil {
		exe = filepath.Clean(exe)
		if filepath.Base(filepath.Dir(exe)) == "bin" {
			root := filepath.Dir(filepath.Dir(exe))
			if fileExists(filepath.Join(root, "scripts", "run.sh")) || fileExists(filepath.Join(root, "scripts", "run.cmd")) {
				return root, nil
			}
		}
	}
	cwd, err := os.Getwd()
	if err == nil {
		if fileExists(filepath.Join(cwd, "scripts", "run.sh")) && fileExists(filepath.Join(cwd, "skills")) {
			return cwd, nil
		}
	}
	return "", errors.New("could not detect Lumen checkout root; run through scripts/run.sh or set LUMEN_PLUGIN_ROOT")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
```

- [ ] **Step 4: Run the path test**

Run:

```bash
go test ./cmd -run TestCodexPathsFromEnv -count=1
```

Expected: PASS.

- [ ] **Step 5: Export `LUMEN_PLUGIN_ROOT` from launchers**

Modify `scripts/run.sh` just before the final `exec`:

```bash
export LUMEN_PLUGIN_ROOT="${PLUGIN_ROOT}"
exec "$BINARY" "$@"
```

Modify `scripts/run.bat` just before invoking the binary:

```bat
set "LUMEN_PLUGIN_ROOT=%PLUGIN_ROOT%"
"%BINARY%" %*
```

- [ ] **Step 6: Add tests for hook file install idempotency**

Replace the import block in `cmd/codex_test.go` and append the tests:

```go
import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallCodexHookFile_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "hooks.json")
	command := `/Users/franz/.codex/lumen/scripts/run.sh hook session-start lumen --host claude`

	changed, err := installCodexHookFile(path, command)
	if err != nil {
		t.Fatalf("first installCodexHookFile: %v", err)
	}
	if !changed {
		t.Fatal("first install should report changed")
	}

	changed, err = installCodexHookFile(path, command)
	if err != nil {
		t.Fatalf("second installCodexHookFile: %v", err)
	}
	if changed {
		t.Fatal("second install should be idempotent")
	}
}

func TestInstallCodexHookFile_MalformedJSONLeavesFileUntouched(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "hooks.json")
	before := []byte(`{"hooks":`)
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := installCodexHookFile(path, "cmd")
	if err == nil {
		t.Fatal("expected malformed JSON error")
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != string(before) {
		t.Fatalf("malformed file should be untouched, got %q", after)
	}
}
```

- [ ] **Step 7: Run the hook file tests and verify they fail**

Run:

```bash
go test ./cmd -run TestInstallCodexHookFile -count=1
```

Expected: FAIL with `undefined: installCodexHookFile`.

- [ ] **Step 8: Implement atomic hook file writes**

Add to `cmd/codex.go`:

```go
func installCodexHookFile(path, command string) (bool, error) {
	var raw []byte
	if b, err := os.ReadFile(path); err == nil {
		raw = b
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	merged, changed, err := mergeCodexSessionStartHook(raw, command)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create hooks directory: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, merged, 0o644); err != nil {
		return false, fmt.Errorf("write temp hooks file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, fmt.Errorf("replace hooks file: %w", err)
	}
	return true, nil
}
```

- [ ] **Step 9: Run hook file tests**

Run:

```bash
go test ./cmd -run TestInstallCodexHookFile -count=1
```

Expected: PASS.

- [ ] **Step 10: Add tests for install orchestration with fake Codex CLI**

Replace the import block in `cmd/codex_test.go` and append the test:

```go
import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls     []string
	getOK     bool
	getOutput string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if len(args) >= 3 && args[0] == "mcp" && args[1] == "get" && args[2] == "lumen" {
		if f.getOK {
			if f.getOutput != "" {
				return []byte(f.getOutput), nil
			}
			return []byte("lumen\n  command: /already/configured\n"), nil
		}
		return nil, exec.ErrNotFound
	}
	return []byte("ok\n"), nil
}

func TestRunCodexInstall_WritesHookAndAddsMCPWhenMissing(t *testing.T) {
	home := t.TempDir()
	pluginRoot := filepath.Join(home, ".codex", "lumen")
	if err := os.MkdirAll(filepath.Join(pluginRoot, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginRoot, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("LUMEN_PLUGIN_ROOT", pluginRoot)

	runner := &fakeRunner{}
	if err := runCodexInstall(context.Background(), io.Discard, io.Discard, runner); err != nil {
		t.Fatalf("runCodexInstall: %v", err)
	}

	hooks, err := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	if !strings.Contains(string(hooks), "hook session-start lumen") {
		t.Fatalf("hooks.json missing Lumen hook:\n%s", hooks)
	}
	if got := strings.Join(runner.calls, "\n"); !strings.Contains(got, "codex mcp add lumen --") {
		t.Fatalf("expected codex mcp add call, got:\n%s", got)
	}
}

func TestRunCodexInstall_ReplacesMismatchedMCP(t *testing.T) {
	home := t.TempDir()
	pluginRoot := filepath.Join(home, ".codex", "lumen")
	if err := os.MkdirAll(filepath.Join(pluginRoot, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginRoot, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("LUMEN_PLUGIN_ROOT", pluginRoot)

	runner := &fakeRunner{
		getOK:     true,
		getOutput: "lumen\n  command: /old/lumen/scripts/run.sh\n  args: stdio\n",
	}
	if err := runCodexInstall(context.Background(), io.Discard, io.Discard, runner); err != nil {
		t.Fatalf("runCodexInstall: %v", err)
	}
	got := strings.Join(runner.calls, "\n")
	if !strings.Contains(got, "codex mcp remove lumen") {
		t.Fatalf("expected codex mcp remove for mismatched config, got:\n%s", got)
	}
	if !strings.Contains(got, "codex mcp add lumen --") {
		t.Fatalf("expected codex mcp add after remove, got:\n%s", got)
	}
}
```

- [ ] **Step 11: Run install orchestration test and verify it fails**

Run:

```bash
go test ./cmd -run TestRunCodexInstall_WritesHookAndAddsMCPWhenMissing -count=1
```

Expected: FAIL with `undefined: runCodexInstall`.

- [ ] **Step 12: Implement install orchestration**

Add imports and helpers to `cmd/codex.go`.

Update the import block:

```go
import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)
```

Add this code after `fileExists`:

```go
type commandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type realCommandRunner struct{}

func (realCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

func runCodexInstall(ctx context.Context, stdout, stderr io.Writer, runner commandRunner) error {
	paths, err := resolveCodexPaths()
	if err != nil {
		return err
	}
	if changed, err := installCodexHookFile(paths.hooksPath, paths.hookCommand); err != nil {
		return err
	} else if changed {
		_, _ = fmt.Fprintf(stdout, "Installed Codex SessionStart hook at %s\n", paths.hooksPath)
	} else {
		_, _ = fmt.Fprintf(stdout, "Codex SessionStart hook already installed at %s\n", paths.hooksPath)
	}

	if err := ensureCodexSkills(paths, stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "Warning: skills setup incomplete: %v\n", err)
	}
	if err := ensureCodexMCP(ctx, paths, runner, stdout, stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "Warning: MCP setup incomplete: %v\n", err)
	}
	return nil
}

func ensureCodexSkills(paths codexPaths, stderr io.Writer) error {
	if _, err := os.Stat(paths.skillsSrc); err != nil {
		return fmt.Errorf("skills source missing: %w", err)
	}
	if info, err := os.Lstat(paths.skillsDst); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%s exists and is not a symlink", paths.skillsDst)
		}
		target, err := os.Readlink(paths.skillsDst)
		if err != nil {
			return fmt.Errorf("read skills symlink: %w", err)
		}
		if target == paths.skillsSrc {
			return nil
		}
		if err := os.Remove(paths.skillsDst); err != nil {
			return fmt.Errorf("remove stale skills link: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(paths.skillsDst), 0o755); err != nil {
		return fmt.Errorf("create skills directory: %w", err)
	}
	if err := os.Symlink(paths.skillsSrc, paths.skillsDst); err != nil {
		return fmt.Errorf("create skills symlink: %w", err)
	}
	return nil
}

func ensureCodexMCP(ctx context.Context, paths codexPaths, runner commandRunner, stdout, stderr io.Writer) error {
	if out, err := runner.Run(ctx, "codex", "mcp", "get", "lumen"); err == nil {
		if codexMCPOutputMatches(out, paths) {
			_, _ = fmt.Fprintln(stdout, "Codex MCP server lumen already registered")
			return nil
		}
		if _, err := runner.Run(ctx, "codex", "mcp", "remove", "lumen"); err != nil {
			return fmt.Errorf("codex mcp remove lumen: %w", err)
		}
	} else {
		_, _ = fmt.Fprintf(stderr, "Codex MCP server lumen not registered: %v\n", err)
	}
	if _, err := runner.Run(ctx, "codex", "mcp", "add", "lumen", "--", paths.launcher, "stdio"); err != nil {
		return fmt.Errorf("codex mcp add lumen: %w", err)
	}
	_, _ = fmt.Fprintln(stdout, "Registered Codex MCP server lumen")
	return nil
}

func codexMCPOutputMatches(out []byte, paths codexPaths) bool {
	text := string(out)
	return strings.Contains(text, "command: "+paths.launcher) &&
		strings.Contains(text, "args: stdio")
}
```

Also add `strings` to the `cmd/codex.go` import block used in this step:

```go
import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)
```

- [ ] **Step 13: Run install orchestration tests**

Run:

```bash
go test ./cmd -run 'TestRunCodexInstall|TestInstallCodexHookFile|TestCodexPathsFromEnv' -count=1
```

Expected: PASS.

- [ ] **Step 14: Add doctor tests**

Append to `cmd/codex_test.go`:

```go
func TestRunCodexDoctorReportsHookStatus(t *testing.T) {
	home := t.TempDir()
	pluginRoot := filepath.Join(home, ".codex", "lumen")
	if err := os.MkdirAll(filepath.Join(pluginRoot, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginRoot, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("LUMEN_PLUGIN_ROOT", pluginRoot)

	paths, err := resolveCodexPaths()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installCodexHookFile(paths.hooksPath, paths.hookCommand); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	runner := &fakeRunner{
		getOK:     true,
		getOutput: "lumen\n  command: " + paths.launcher + "\n  args: stdio\n",
	}
	if err := runCodexDoctor(context.Background(), &out, io.Discard, runner); err != nil {
		t.Fatalf("runCodexDoctor: %v", err)
	}
	text := out.String()
	for _, want := range []string{"Codex home:", "MCP lumen: ok", "SessionStart hook: ok"} {
		if !strings.Contains(text, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, text)
		}
	}
}

func TestRunCodexDoctorReportsMismatchedMCP(t *testing.T) {
	home := t.TempDir()
	pluginRoot := filepath.Join(home, ".codex", "lumen")
	if err := os.MkdirAll(filepath.Join(pluginRoot, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginRoot, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("LUMEN_PLUGIN_ROOT", pluginRoot)

	var out strings.Builder
	runner := &fakeRunner{
		getOK:     true,
		getOutput: "lumen\n  command: /old/lumen/scripts/run.sh\n  args: stdio\n",
	}
	if err := runCodexDoctor(context.Background(), &out, io.Discard, runner); err != nil {
		t.Fatalf("runCodexDoctor: %v", err)
	}
	if !strings.Contains(out.String(), "MCP lumen: mismatch") {
		t.Fatalf("expected MCP mismatch, got:\n%s", out.String())
	}
}
```

- [ ] **Step 15: Run doctor test and verify it fails**

Run:

```bash
go test ./cmd -run 'TestRunCodexDoctorReports' -count=1
```

Expected: FAIL with `undefined: runCodexDoctor`.

- [ ] **Step 16: Implement doctor**

Update the `cmd/codex.go` import block to include `encoding/json`, then add the Cobra command registration and doctor helpers:

```go
func init() {
	rootCmd.AddCommand(codexCmd)
	codexCmd.AddCommand(codexInstallCmd)
	codexCmd.AddCommand(codexDoctorCmd)
}

var codexCmd = &cobra.Command{
	Use:   "codex",
	Short: "Install and diagnose Lumen's Codex integration",
}

var codexInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install or repair Lumen's Codex MCP, skills, and hooks",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runCodexInstall(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), realCommandRunner{})
	},
}

var codexDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check Lumen's Codex MCP, skills, and hooks",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runCodexDoctor(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), realCommandRunner{})
	},
}

func runCodexDoctor(ctx context.Context, stdout, stderr io.Writer, runner commandRunner) error {
	paths, err := resolveCodexPaths()
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Codex home: %s\n", paths.codexHome)
	_, _ = fmt.Fprintf(stdout, "Lumen root: %s\n", paths.pluginRoot)

	if out, err := runner.Run(ctx, "codex", "mcp", "get", "lumen"); err == nil {
		if codexMCPOutputMatches(out, paths) {
			_, _ = fmt.Fprintln(stdout, "MCP lumen: ok")
		} else {
			_, _ = fmt.Fprintln(stdout, "MCP lumen: mismatch")
		}
	} else {
		_, _ = fmt.Fprintln(stdout, "MCP lumen: missing")
	}

	if has, duplicates, err := codexHookStatus(paths.hooksPath); err != nil {
		_, _ = fmt.Fprintf(stdout, "SessionStart hook: error: %v\n", err)
	} else if !has {
		_, _ = fmt.Fprintln(stdout, "SessionStart hook: missing")
	} else if duplicates > 1 {
		_, _ = fmt.Fprintf(stdout, "SessionStart hook: duplicate (%d Lumen hooks)\n", duplicates)
	} else {
		_, _ = fmt.Fprintln(stdout, "SessionStart hook: ok")
	}

	if target, err := os.Readlink(paths.skillsDst); err == nil && target == paths.skillsSrc {
		_, _ = fmt.Fprintln(stdout, "Skills: ok")
	} else {
		_, _ = fmt.Fprintln(stdout, "Skills: missing")
	}
	return nil
}

func codexHookStatus(path string) (bool, int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, 0, nil
		}
		return false, 0, err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false, 0, err
	}
	hooksRaw, ok := doc["hooks"]
	if !ok {
		return false, 0, nil
	}
	var hooksObj map[string]json.RawMessage
	if err := json.Unmarshal(hooksRaw, &hooksObj); err != nil {
		return false, 0, err
	}
	sessionRaw, ok := hooksObj["SessionStart"]
	if !ok {
		return false, 0, nil
	}
	var sessionStart []map[string]any
	if err := json.Unmarshal(sessionRaw, &sessionStart); err != nil {
		return false, 0, err
	}
	count := 0
	for _, group := range sessionStart {
		if codexHookGroupRunsLumenSessionStart(group) {
			count++
		}
	}
	return count > 0, count, nil
}
```

The final import block for `cmd/codex.go` should include:

```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)
```

- [ ] **Step 17: Run all Codex command tests**

Run:

```bash
go test ./cmd -run 'Test(Codex|RunCodex|InstallCodex|MergeCodex)' -count=1
```

Expected: PASS.

- [ ] **Step 18: Commit Codex command implementation**

Run:

```bash
git add cmd/codex.go cmd/codex_test.go scripts/run.sh scripts/run.bat
git commit -m "feat(codex): install session-start hook"
```

## Task 3: Documentation and Install Flow

**Files:**
- Modify: `.codex/INSTALL.md`
- Modify: `README.md`

- [ ] **Step 1: Update Codex install docs**

Replace the manual Codex install section in `.codex/INSTALL.md` with this flow:

```markdown
## Installation

1. Clone the repository:
   ```bash
   CODEX_HOME="${CODEX_HOME:-$HOME/.codex}"
   git clone https://github.com/ory/lumen.git "$CODEX_HOME/lumen"
   ```

2. Install or repair Codex integration:
   ```bash
   "$CODEX_HOME/lumen/scripts/run.sh" codex install
   ```

   This registers the `lumen` MCP server, links the shared Lumen skills, and
   installs a Codex `SessionStart` hook that pre-warms the project index in the
   background on startup, resume, and clear.

3. Restart Codex.
```

Update the Windows section:

```powershell
$codexHome = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $env:USERPROFILE ".codex" }
git clone https://github.com/ory/lumen.git "$codexHome\lumen"
cmd /c "$codexHome\lumen\scripts\run.cmd" codex install
```

Add verify commands:

```bash
"${CODEX_HOME:-$HOME/.codex}/lumen/scripts/run.sh" codex doctor
codex mcp get lumen
```

- [ ] **Step 2: Update README Codex section**

Change the Codex quick install block in `README.md` so it points users at `.codex/INSTALL.md` and mentions:

```markdown
The Codex installer configures the MCP server, shared skills, and a user-level
Codex `SessionStart` hook. The hook reuses Lumen's existing background indexer
so Codex sessions get the same proactive index warmup as Claude Code and Cursor.
```

- [ ] **Step 3: Run doc-adjacent tests**

Run:

```bash
go test ./cmd -run 'Test(Codex|RunCodex|InstallCodex|MergeCodex)' -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit docs**

Run:

```bash
git add .codex/INSTALL.md README.md
git commit -m "docs(codex): document session-start hook install"
```

## Task 4: Full Verification and Local Codex Smoke Test

**Files:**
- No source files expected.
- May temporarily back up and restore: `~/.codex/hooks.json`

- [ ] **Step 1: Run focused Go tests**

Run:

```bash
go test ./cmd -run 'Test(Codex|RunCodex|InstallCodex|MergeCodex|HookSessionStart)' -count=1
```

Expected: PASS.

- [ ] **Step 2: Run the full Go suite**

Run:

```bash
make test
```

Expected: PASS.

- [ ] **Step 3: Build the local binary**

Run:

```bash
make build-local
```

Expected: exit 0 and `bin/lumen` exists.

- [ ] **Step 4: Verify temp CODEX_HOME install without touching real config**

Run:

```bash
tmp_home="$(mktemp -d)"
CODEX_HOME="$tmp_home/.codex" LUMEN_PLUGIN_ROOT="$PWD" ./bin/lumen codex install
CODEX_HOME="$tmp_home/.codex" LUMEN_PLUGIN_ROOT="$PWD" ./bin/lumen codex doctor
cat "$tmp_home/.codex/hooks.json"
```

Expected:

- `codex install` exits 0.
- `codex doctor` reports `SessionStart hook: ok`.
- `hooks.json` contains one `hook session-start lumen --host claude` command.
- `hooks.json` matcher is `startup|resume|clear`.

- [ ] **Step 5: Verify real local install with backup**

Run:

```bash
backup=""
if [ -f "$HOME/.codex/hooks.json" ]; then
  backup="$HOME/.codex/hooks.json.backup.$(date +%Y%m%d%H%M%S)"
  cp "$HOME/.codex/hooks.json" "$backup"
fi
LUMEN_PLUGIN_ROOT="$PWD" ./bin/lumen codex install
LUMEN_PLUGIN_ROOT="$PWD" ./bin/lumen codex doctor
```

Expected:

- `codex install` exits 0.
- `codex doctor` reports `MCP lumen: ok` and `SessionStart hook: ok`.
- If anything looks wrong, restore with `cp "$backup" "$HOME/.codex/hooks.json"` when `backup` is non-empty.

- [ ] **Step 6: Verify Codex actually runs the SessionStart hook**

Before starting the smoke test, record the current log length:

```bash
start_line="$(wc -l < "$HOME/.local/share/lumen/debug.log")"
codex -a never exec -C "$PWD" --sandbox read-only "This is a hook smoke test. Say one sentence and do not edit files."
end_line="$(wc -l < "$HOME/.local/share/lumen/debug.log")"
sed -n "$((start_line + 1)),${end_line}p" "$HOME/.local/share/lumen/debug.log" | rg 'indexing|index skipped|lumen config|index already fresh|background'
```

Expected:

- Codex command exits 0.
- The debug log shows hook-driven Lumen activity or a lock-skip/already-fresh path for this project.
- No extra stale `lumen stdio` processes are left by this smoke test.

- [ ] **Step 7: Run final status checks**

Run:

```bash
git status --short --branch
git log --oneline --decorate --max-count=5
```

Expected:

- Working tree is clean.
- Branch is `feat/codex-session-start-hooks`.
- Commits are the spec, hook merge, Codex installer, and docs commits.

- [ ] **Step 8: Stop before PR**

Do not create a pull request. Report verification results to Franz and wait for explicit approval.
