# PRD: EMT MVP Phase 3

## Context

Phase 2 added Codex history import, but the current import action imports every session under `~/.codex/sessions`. For users with long Codex history, this makes EMT noisy immediately: too many old sessions appear, directory groups become hard to scan, and cleanup becomes manual.

Phase 3 focuses on making import controlled and reversible at the EMT index level.

## Goal

Replace one-click full import with a preview-first import flow.

Users should be able to inspect, filter, select, import, clear imported sessions, and recover deleted imported sessions without touching Codex's original JSONL history.

## Goals

1. Preview Codex history before importing it into EMT.
2. Let users filter and select which sessions to import.
3. Avoid duplicate imports by showing already-added sessions.
4. Provide a fast way to clear all imported sessions from EMT.
5. Let deleted imported sessions be recoverable by importing them again.

## Non-Goals

- No recycle bin or tombstone model.
- No `deleted_at` state.
- No permanent deletion of Codex JSONL files.
- No semantic search.
- No LLM-generated session summaries.
- No background incremental indexer.
- No Project/Workspace/Agent entity.
- No import preview persistence.

## Product Decisions

### Import Must Be Preview-First

Clicking `Import` no longer writes to `sessions.json` immediately. It opens an import dialog populated by a scan of `~/.codex/sessions`.

### Deleted Sessions Recover Through Import

Deleting an imported session removes only the EMT index entry. Since the source JSONL remains, the session appears again in Import Preview and can be re-imported.

### Clear Imported Is Index-Only

`Clear Imported` removes only sessions with `source=imported` from EMT's index. It preserves:

- EMT-created sessions
- Codex JSONL files
- existing Codex history outside EMT

## Import Preview UX

### Entry Point

The existing `Import` button opens `ImportDialog`.

Initial behavior:

1. Scan `~/.codex/sessions`.
2. Show preview rows.
3. Default filter to recent sessions, such as last 30 days.
4. Do not import anything until the user clicks `Import selected`.

### Preview Row

Each candidate row should show:

- checkbox
- session name candidate
- working directory
- Codex session id
- last active time
- status: `new` or `existing`

Status behavior:

- `new`: selectable
- `existing`: already present in EMT, not selectable by default

### Filters

Phase 3 filters:

- text search across session name, working directory, and Codex session id
- working directory filter
- time filter:
  - recent 7 days
  - recent 30 days
  - all
- toggle to show/hide existing sessions

Default:

- recent 30 days
- hide existing sessions

### Bulk Actions

Import dialog should support:

- `Select visible`
- `Clear selection`
- `Import selected`

Do not add advanced saved filters.

### Import Result

After import, show concise result:

```text
Imported 12, skipped 3, failed 1
```

Then refresh the main session list.

## Clear Imported UX

Add a `Clear Imported` action near Import.

Confirmation copy should be explicit:

```text
Remove all imported sessions from EMT? Codex history files and EMT-created sessions will not be deleted.
```

After confirm:

1. Close running PTYs for imported sessions.
2. Remove `source=imported` sessions from `sessions.json`.
3. Refresh the main session list.
4. Show result: `Cleared 42 imported sessions`.

If no imported sessions exist, show `No imported sessions to clear`.

## Backend Requirements

### Data Shape

No new business entity is added.

Add preview DTOs only:

```go
type ImportPreviewSession struct {
    CodexSessionID   string    `json:"codex_session_id"`
    CodexSessionPath string    `json:"codex_session_path"`
    Name             string    `json:"name"`
    WorkingDir       string    `json:"working_dir"`
    CreatedAt        time.Time `json:"created_at"`
    LastActiveAt     time.Time `json:"last_active_at"`
    Status           string    `json:"status"`
}

type ImportPreviewResult struct {
    Sessions []ImportPreviewSession `json:"sessions"`
    Failed   int                    `json:"failed"`
}
```

Allowed preview `Status` values:

- `new`
- `existing`

Import result can reuse the existing `ImportResult`.

Add clear result:

```go
type ClearImportedResult struct {
    Cleared int `json:"cleared"`
}
```

These are response DTOs, not persisted entities.

### API

Add:

```go
PreviewCodexSessions() (ImportPreviewResult, error)
ImportSelectedCodexSessions(codexSessionIDs []string) (ImportResult, error)
ClearImportedSessions() (ClearImportedResult, error)
```

Change frontend usage:

- `ImportCodexSessions()` should no longer be used by the main Import button.
- It can remain internally or be removed during implementation if no longer needed.

### Preview Logic

`PreviewCodexSessions`:

1. Walks `~/.codex/sessions`.
2. Parses each `.jsonl` with existing session metadata parsing.
3. Marks a candidate as `existing` if its `codex_session_id` is already in EMT.
4. Returns all parsed candidates sorted by `last_active_at` descending.
5. Counts parse failures.
6. Does not mutate `sessions.json`.

### Import Selected Logic

`ImportSelectedCodexSessions(codexSessionIDs []string)`:

1. Treats empty input as no-op.
2. Scans Codex sessions or uses the same parser helper as preview.
3. Imports only candidates whose `codex_session_id` is in the requested set.
4. Skips ids that already exist in EMT.
5. Saves once after import.
6. Returns imported/skipped/failed counts.

If a requested id cannot be found, count it as `failed`.

### Clear Imported Logic

`ClearImportedSessions`:

1. Finds sessions where `source=imported`.
2. Closes running PTYs for those sessions.
3. Removes them from the EMT session list.
4. Saves once.
5. Returns the cleared count.

It must not delete `codex_session_path`.

## Frontend Requirements

### Import Button

`Import` opens `ImportDialog` instead of importing immediately.

### ImportDialog

State:

- loading
- preview sessions
- selected ids
- search
- time filter
- working directory filter
- show existing toggle
- failed count

The dialog should be dense and utilitarian. No wizard flow is needed.

### Filtering

Filtering can be frontend-only for Phase 3.

Rules:

- Apply time filter to `last_active_at`.
- Apply directory filter to exact `working_dir`.
- Apply text search to name, working dir, and Codex session id.
- Hide `existing` by default.

### Selection

Rules:

- Only `new` rows are selectable.
- `Select visible` selects only currently visible `new` rows.
- `Clear selection` clears all selected ids.
- `Import selected` is disabled when selection is empty.

### Clear Imported

Add `Clear Imported` button near Import.

Rules:

- Ask for confirmation.
- Call `ClearImportedSessions`.
- Refresh sessions.
- If selected session was cleared, select first remaining session or clear selection.

## Recovery Flow

To recover a deleted imported session:

1. Open Import.
2. Search or filter for the session.
3. Select it.
4. Click `Import selected`.

No separate Restore feature is added.

## Success Criteria

1. Clicking Import opens preview and does not mutate `sessions.json`.
2. Preview shows new and existing candidates.
3. Existing candidates are not imported again.
4. User can filter by text, directory, and time window.
5. User can select visible candidates and import selected only.
6. Re-running preview after import marks those sessions as existing.
7. Clear Imported removes only `source=imported` sessions.
8. Clear Imported does not remove EMT-created sessions.
9. Deleted imported sessions can be imported again from preview.
10. Codex JSONL files are never deleted.

## Validation Plan

Automated tests:

- Preview does not mutate store.
- Preview marks existing sessions.
- Preview sorts by last active time descending.
- Import selected imports only requested ids.
- Import selected skips already-existing ids.
- Import selected counts missing requested ids as failed.
- Clear imported removes only `source=imported`.
- Clear imported leaves Codex JSONL files untouched.

Manual checks:

1. Click Import and close dialog without importing; main list should not change.
2. Preview recent 30 days by default.
3. Switch to all and confirm more candidates appear.
4. Select visible candidates and import.
5. Reopen Import and confirm imported candidates are marked existing.
6. Delete one imported session and confirm it appears as new in preview again.
7. Clear Imported and confirm EMT-created sessions remain.
8. Confirm original JSONL files still exist.
