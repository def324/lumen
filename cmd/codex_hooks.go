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
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

const codexSessionStartMatcher = "startup|resume|clear"
const codexHookStatusMessage = "Starting Lumen background indexing"

func codexLauncherPath(pluginRoot string) string {
	return codexLauncherPathForGOOS(pluginRoot, runtime.GOOS)
}

func codexLauncherPathForGOOS(pluginRoot, goos string) string {
	if goos == "windows" {
		return strings.TrimRight(pluginRoot, `\/`) + `\scripts\run.cmd`
	}
	return filepath.Join(pluginRoot, "scripts", "run.sh")
}

func codexSessionStartCommand(launcher string) string {
	return shellQuoteCommandPath(launcher) + " hook session-start lumen --host claude"
}

func mergeCodexSessionStartHook(raw []byte, command string) ([]byte, bool, error) {
	doc := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, false, err
		}
		if doc == nil {
			return nil, false, fmt.Errorf("codex hooks document must be an object")
		}
	}

	hooks := map[string]any{}
	if rawHooks, ok := doc["hooks"]; ok {
		var hooksOK bool
		hooks, hooksOK = rawHooks.(map[string]any)
		if !hooksOK {
			return nil, false, fmt.Errorf("codex hooks document field %q must be an object", "hooks")
		}
	}

	sessionStart := []any{}
	if rawSessionStart, ok := hooks["SessionStart"]; ok {
		groups, groupsOK := rawSessionStart.([]any)
		if !groupsOK {
			return nil, false, fmt.Errorf("codex hooks document field %q must be an array", "hooks.SessionStart")
		}
		for _, rawGroup := range groups {
			group, ok := rawGroup.(map[string]any)
			if !ok {
				return nil, false, fmt.Errorf("codex hooks document field %q must contain objects", "hooks.SessionStart")
			}
			preservedGroup, keep, err := removeLumenCodexHooksFromGroup(group, command)
			if err != nil {
				return nil, false, err
			}
			if !keep {
				continue
			}
			sessionStart = append(sessionStart, preservedGroup)
		}
	}

	hooks["SessionStart"] = append(sessionStart, map[string]any{
		"matcher": codexSessionStartMatcher,
		"hooks": []any{
			map[string]any{
				"type":          "command",
				"command":       command,
				"statusMessage": codexHookStatusMessage,
			},
		},
	})
	doc["hooks"] = hooks

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, false, err
	}
	out = append(out, '\n')
	return out, !bytes.Equal(bytes.TrimSpace(raw), bytes.TrimSpace(out)), nil
}

func codexHookGroupRunsLumenSessionStart(group map[string]any) bool {
	return codexHookGroupRunsLumenSessionStartCommand(group, "")
}

func codexHookGroupRunsLumenSessionStartCommand(group map[string]any, expectedCommand string) bool {
	hooks, ok := group["hooks"].([]any)
	if !ok {
		return false
	}
	for _, rawHook := range hooks {
		hook, ok := rawHook.(map[string]any)
		if !ok {
			continue
		}
		command, ok := hook["command"].(string)
		if !ok {
			continue
		}
		if isOwnedLumenCodexSessionStartCommand(command, hook, expectedCommand) {
			return true
		}
	}
	return false
}

func removeLumenCodexHooksFromGroup(group map[string]any, expectedCommand string) (map[string]any, bool, error) {
	hooks, ok := group["hooks"].([]any)
	if !ok {
		return nil, false, fmt.Errorf("codex hooks document field %q must contain hook arrays", "hooks.SessionStart")
	}

	preservedHooks := make([]any, 0, len(hooks))
	changed := false
	for _, rawHook := range hooks {
		hook, ok := rawHook.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("codex hooks document field %q must contain hook objects", "hooks.SessionStart.hooks")
		}
		command, _ := hook["command"].(string)
		if command != "" && isOwnedLumenCodexSessionStartCommand(command, hook, expectedCommand) {
			changed = true
			continue
		}
		preservedHooks = append(preservedHooks, rawHook)
	}
	if len(preservedHooks) == 0 {
		return nil, false, nil
	}
	if !changed {
		return group, true, nil
	}

	preservedGroup := make(map[string]any, len(group))
	for key, value := range group {
		preservedGroup[key] = value
	}
	preservedGroup["hooks"] = preservedHooks
	return preservedGroup, true, nil
}

func isLumenCodexSessionStartCommand(command string) bool {
	normalized := strings.ReplaceAll(command, `\`, "/")
	if !strings.Contains(normalized, "hook session-start lumen") {
		return false
	}
	return strings.Contains(normalized, "/lumen/scripts/run.sh") ||
		strings.Contains(normalized, "/lumen/scripts/run.cmd")
}

func isOwnedLumenCodexSessionStartCommand(command string, hook map[string]any, expectedCommand string) bool {
	if expectedCommand != "" && command == expectedCommand {
		return true
	}
	if hook["statusMessage"] == codexHookStatusMessage &&
		strings.Contains(command, "hook session-start lumen") {
		return true
	}
	return isLumenCodexSessionStartCommand(command)
}

func shellQuoteCommandPath(path string) string {
	if looksLikeWindowsPath(path) {
		return windowsCommandQuote(path)
	}
	return posixShellQuote(path)
}

func looksLikeWindowsPath(path string) bool {
	return strings.Contains(path, `\`) || (len(path) >= 2 && path[1] == ':')
}

func posixShellQuote(path string) string {
	if path == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

func windowsCommandQuote(path string) string {
	replacer := strings.NewReplacer(
		`^`, `^^`,
		`%`, `^%`,
		`!`, `^^!`,
		`"`, `^"`,
	)
	return `"` + replacer.Replace(path) + `"`
}
