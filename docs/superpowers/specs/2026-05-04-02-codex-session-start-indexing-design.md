# Codex Session-Start Indexing Design

## Context

Lumen already has two indexing paths:

- The MCP `semantic_search` tool lazily calls `EnsureFresh` before searching.
- Claude Code and Cursor run `lumen hook session-start ...`, which spawns a detached background `lumen index <project>` process when the index is missing or older than the session-start staleness threshold.

Codex currently uses the MCP server and skills, but its documented install path does not configure a Codex `SessionStart` hook. As a result, Codex usually discovers stale or missing indexes on the first `semantic_search` call. If reindexing takes longer than the search timeout, the agent receives stale results plus a warning.

Codex now supports lifecycle hooks, including `SessionStart`, and can discover hooks from `~/.codex/hooks.json`, inline `config.toml`, repo-local `.codex/hooks.json`, or bundled plugin hook sources.

## Goal

Give Codex the same proactive project indexing behavior that Claude Code and Cursor already get, without adding file watchers or changing the stdio MCP server lifecycle.

The first implementation should focus on reliable automated setup for Codex and strong local verification before any pull request is created.

## Non-Goals

- Do not add a filesystem watcher inside `lumen stdio`.
- Do not implement OpenCode support in this phase.
- Do not create repo-local `.codex/hooks.json` as the primary install path.
- Do not modify user hook configuration destructively.
- Do not open a pull request until Franz approves the implementation.

## Proposed Approach

Add Codex-specific install/repair support that manages a Lumen-owned user-level `SessionStart` hook.

The installed hook should run the existing Lumen hook command with an absolute launcher path:

```text
/Users/.../.codex/lumen/scripts/run.sh hook session-start lumen --host claude
```

On Windows, it should use the existing cross-platform launcher:

```text
C:\...\lumen\scripts\run.cmd hook session-start lumen --host claude
```

The hook matcher should be:

```text
startup|resume|clear
```

This matches Codex's documented `SessionStart` sources. `compact` should not be included unless we verify that Codex emits it.

## Configuration Strategy

Use `~/.codex/hooks.json` as the default automated target. This makes the hook work across projects and avoids relying on each repository having trusted project-local Codex config.

The installer must:

- Create `~/.codex/hooks.json` if it does not exist.
- Preserve all existing non-Lumen hooks.
- Replace only the Lumen-owned hook entry when updating.
- Avoid duplicate Lumen hook entries.
- Keep valid JSON formatting.
- Fail safely on malformed JSON by reporting the problem and leaving the file untouched.
- Write atomically where practical.

The Lumen-owned hook entry should be identifiable by command shape rather than by deleting every `SessionStart` hook. A command should be considered Lumen-owned only if it runs a Lumen launcher and invokes `hook session-start lumen`.

## Commands

Implement these Codex-specific CLI commands:

- `lumen codex install` installs or repairs Codex MCP, skills, and the SessionStart hook.
- `lumen codex doctor` reports whether MCP, skills, and the SessionStart hook are configured.

The CLI form is the required implementation surface for this phase because it can be tested directly and documented consistently. Shell scripts may be used internally only as thin launchers for platform-specific convenience.

## Runtime Behavior

When Codex starts, resumes, or clears a session:

1. Codex runs the configured `SessionStart` hook with the session `cwd`.
2. Lumen resolves the effective project root using the existing hook logic.
3. If the project index is missing or stale, Lumen spawns a detached `lumen index <project>` process.
4. The detached indexer uses the existing advisory lock, so multiple Codex sessions should not corrupt the index or run concurrent writers for the same project.
5. Codex receives the same Lumen developer-context guidance already used by the existing hook command.

## Error Handling

Hook installation errors should be explicit and non-destructive:

- Missing Codex home: create it.
- Missing Lumen clone root: report the detected root and fail.
- Malformed `hooks.json`: report path and parse error, do not rewrite.
- Existing user hooks: preserve them.
- Existing stale Lumen hook path: replace it with the current absolute launcher path.
- Missing `codex` binary: `doctor` should report it, but hook installation can still write config.

Hook runtime errors should remain best-effort. The existing `hook session-start` command already falls back to lazy MCP indexing if background indexing fails.

## Testing

Unit tests should cover hook JSON manipulation:

- Empty or missing hook file.
- Existing unrelated hooks.
- Existing unrelated `SessionStart` hooks.
- Existing Lumen hook with current path.
- Existing Lumen hook with stale path.
- Multiple duplicate Lumen hooks.
- Malformed JSON.
- Windows command path generation.

Integration tests should cover:

- Installing into a temporary `CODEX_HOME`.
- Running install twice and confirming idempotent output.
- Running `doctor` against the temporary `CODEX_HOME`.
- Invoking `lumen hook session-start lumen --host claude` with a synthetic Codex hook payload and confirming it emits Codex-compatible SessionStart output.

Local manual verification before PR:

- Back up the real `~/.codex/hooks.json` if present.
- Run the installer against the real Codex home.
- Start or resume a Codex session in this repository.
- Confirm Lumen logs show the session-start hook path and, when stale, a background indexer or lock-skip.
- Restore user config if any behavior is wrong.

## Risks

The main risk is clobbering user hooks. The implementation should reduce that risk by limiting writes to a clearly identified Lumen-owned hook entry and by refusing to rewrite malformed JSON.

The second risk is assuming Codex plugin packaging can install hooks automatically. The first implementation should not depend on plugin packaging; it should provide an explicit install/repair command that can be verified locally.

The third risk is hidden duplication when users install both repo-local and user-level hooks. `doctor` should detect multiple Lumen-owned Codex hooks and report them.

## Acceptance Criteria

- Codex users have a documented, automated way to install proactive Lumen indexing.
- Running the installer is idempotent and preserves unrelated hooks.
- Existing Lumen hook behavior is reused rather than duplicated.
- Unit and integration tests pass locally.
- Real local Codex verification demonstrates that `SessionStart` triggers background indexing.
- No pull request is created until Franz approves the implementation.
