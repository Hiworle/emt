# PRD: EMT MVP Phase 2

## Context

Phase 1 delivered the first EMT loop: create a Codex session, interact with it in an embedded terminal, persist the session list, and resume EMT-created sessions.

Phase 2 moves EMT from "can run Codex sessions" to "can manage day-to-day Codex work by directory." The scope is intentionally narrow: improve session organization, import existing Codex history, and add basic session maintenance without introducing project, workspace, or multi-agent abstractions.

## Goals

1. Let users choose a working directory when creating a Codex session.
2. Import existing Codex sessions from `~/.codex/sessions`.
3. Make the session list usable at larger scale with directory grouping, search, rename, delete, and clearer session status.

## Non-Goals

- No multi-agent support.
- No `Project`, `Workspace`, or `Agent` entity.
- No IDE/file jump integration.
- No semantic search or summaries.
- No deletion of Codex original JSONL files.
- No Windows PTY support.
- No project settings page, project aliases, or directory favorites.

## Product Decisions

### Keep One Business Entity

Phase 2 still has one business entity: `Session`.

The UI may group sessions by `working_dir`, but this is only a derived view. EMT does not persist a `Project` entity.

### Delete Means Local Index Delete

Deleting a session removes it from EMT's `~/.emt/sessions.json` only. It never deletes files under `~/.codex/sessions`.

### Import Is Bulk and Idempotent

Import scans all Codex JSONL files and adds missing sessions. It does not show a preview or let users pick individual sessions in Phase 2. Running import multiple times must not create duplicates.

## Data Model

Extend `Session` with the minimum fields needed for import and display:

```go
type Session struct {
    ID               string    `json:"id"`
    Name             string    `json:"name"`
    CodexSessionID   string    `json:"codex_session_id"`
    CodexSessionPath string    `json:"codex_session_path"`
    WorkingDir       string    `json:"working_dir"`
    Source           string    `json:"source"`
    CreatedAt        time.Time `json:"created_at"`
    LastActiveAt     time.Time `json:"last_active_at"`
    Status           string    `json:"status"`
}
```

Allowed values:

- `Source`: `emt`, `imported`
- `Status`: `running`, `idle`, `error`

Compatibility:

- Existing Phase 1 sessions without `source` load as `source=emt`.
- Existing Phase 1 sessions without `codex_session_path` remain valid.
- `codex_session_id` stays the resume key. `codex_session_path` is metadata for import, debugging, and duplicate detection support.

Persisted shape remains:

```json
{
  "sessions": []
}
```

No `projects`, `workspaces`, or `agents` arrays are added.

## Backend Requirements

### API

Phase 2 keeps the Phase 1 API and adds the smallest needed methods:

```go
CreateSession(name string, workingDir string) (Session, error)
ListSessions() ([]Session, error)
ResumeSession(id string) error
CloseSession(id string) error
SendInput(id string, data string) error
ResizeTerminal(id string, rows int, cols int) error

ImportCodexSessions() (ImportResult, error)
RenameSession(id string, name string) (Session, error)
DeleteSession(id string) error
ChooseWorkingDir(defaultDir string) (string, error)
```

`ImportResult` is a response DTO:

```go
type ImportResult struct {
    Imported int `json:"imported"`
    Skipped  int `json:"skipped"`
    Failed   int `json:"failed"`
}
```

### Create Session

Rules:

1. If `workingDir` is empty, use the EMT startup directory.
2. If `name` is empty, generate the next `Session N`.
3. Start Codex with `codex -C <workingDir>`.
4. Save the session with `source=emt`.
5. Discover and save `codex_session_id` and `codex_session_path` after Codex writes `session_meta`.

### Resume Session

Rules:

1. Resume with `codex resume <codex_session_id> -C <working_dir>`.
2. If `codex_session_id` is missing, return a visible error.
3. If the session is already running, do not spawn a duplicate PTY.
4. Update `last_active_at` when resume succeeds.

### Import Codex Sessions

Import flow:

1. Walk `~/.codex/sessions` recursively.
2. Inspect only `.jsonl` files.
3. Parse the first valid `session_meta` record.
4. Extract:
   - `payload.id` as `codex_session_id`
   - `payload.cwd` as `working_dir`
   - JSONL path as `codex_session_path`
   - file mod time as `last_active_at`
   - session meta timestamp or file mod time as `created_at`
5. Skip records whose `codex_session_id` already exists in EMT.
6. Create imported sessions with `source=imported` and `status=idle`.
7. Generate names from directory basename and timestamp, for example `emt 2026-05-27 16:30`.
8. Save once after the scan completes.

Failure handling:

- A malformed JSONL increments `failed` and does not stop import.
- A JSONL without `session_meta` increments `failed`.
- A duplicate session increments `skipped`.
- Missing `~/.codex/sessions` returns zero imported/skipped/failed and no fatal error.

### Rename Session

Rules:

1. Trim whitespace.
2. Empty names are rejected.
3. Rename only affects EMT metadata.
4. Return the updated session and emit `session:updated`.

### Delete Session

Rules:

1. If the session has a running PTY, close it first.
2. Remove the session from `sessions.json`.
3. Do not delete `codex_session_path`.
4. Emit a session deletion event or let the frontend refresh the list.

### Choose Working Directory

Use the Wails native directory dialog. If the dialog is unavailable on a platform, the frontend can fall back to a text input path.

## Frontend Requirements

### Main Layout

Keep one primary screen:

```text
Sidebar | Terminal
```

No settings page or project page is added in Phase 2.

### Sidebar

Sidebar structure:

```text
[Search sessions...]

[+ New] [Import]

/home/user/project-a
  Session 3        running
  Fix auth bug     idle

/home/user/project-b
  Imported 16:30   idle
```

Rules:

1. Group sessions by `working_dir`.
2. Sort groups by the newest `last_active_at` in each group.
3. Sort sessions in each group by `last_active_at` descending.
4. Search by session name, working directory, and Codex session id.
5. Hide groups with no matching sessions.
6. Show `source` as a small label: `EMT` or `Imported`.
7. Show `status`: `running`, `idle`, or `error`.

### New Session Dialog

Clicking `+ New` opens a small dialog:

- Session name input, optional.
- Working directory input.
- `Browse` button using `ChooseWorkingDir(defaultDir)`.
- `Create` button.

Default working directory:

1. Most recently active session's `working_dir`.
2. EMT startup directory if no sessions exist.

### Import Interaction

Clicking `Import`:

1. Calls `ImportCodexSessions()`.
2. Refreshes `ListSessions()`.
3. Shows a concise result: `Imported 12, skipped 38, failed 2`.

No import preview is required.

### Session Row Actions

Each row has a compact action menu:

- `Rename`
- `Close`
- `Delete`

Rules:

- `Close` stops a running PTY and keeps the session in the list.
- `Delete` shows confirmation text that says Codex original history will not be deleted.
- `Rename` can be modal or inline. Use the simplest implementation that is reliable.

### Empty and Error States

- No sessions: `Create or import a Codex session`.
- No search results: `No matching sessions`.
- Import result with failures: show counts only. Detailed failure logs can remain backend logs in Phase 2.

## UX Notes

- Keep the UI dense and terminal-focused.
- Avoid marketing-style empty states or large hero areas.
- Avoid adding a left navigation layer.
- Avoid adding project cards. Directory grouping in the sidebar is enough.

## Success Criteria

1. A user can create a new Codex session in a chosen working directory.
2. The session appears under its working directory group after restart.
3. A user can import existing Codex sessions from `~/.codex/sessions`.
4. Re-running import does not create duplicate sessions.
5. A user can resume an imported session.
6. A user can search sessions by name, path, or Codex session id.
7. A user can rename a session.
8. A user can delete a session from EMT without deleting the Codex JSONL file.
9. `sessions.json` remains a single session list and does not add project/workspace/agent collections.

## Validation Plan

Automated tests:

- Session store backward compatibility for Phase 1 records.
- Import parser extracts id, cwd, timestamp, and file path from JSONL.
- Import skips duplicates by `codex_session_id`.
- Import counts malformed files as failed.
- Rename rejects empty names and persists valid names.
- Delete removes only EMT metadata.

Manual checks:

1. Start `wails dev`.
2. Create a session with a chosen working directory.
3. Restart and confirm the session appears under the same directory group.
4. Run import and confirm historical sessions appear.
5. Run import again and confirm no duplicates.
6. Search by directory and session name.
7. Rename and delete a session.
8. Resume an imported session.
