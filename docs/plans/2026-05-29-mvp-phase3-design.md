# MVP Phase 3 Design

## Goal

Replace one-click full Codex history import with a preview-first import flow that lets users inspect, filter, select, import, clear imported sessions, and recover deleted imported sessions without modifying Codex JSONL history.

Phase 3 keeps EMT as a small local session manager. It does not add projects, workspaces, agents, preview persistence, tombstones, semantic search, or background indexing.

## Assumptions

- `Session` remains the only persisted business entity.
- `~/.emt/sessions.json` remains a flat session index with no new top-level collections.
- Codex history remains under `~/.codex/sessions`.
- Deleting or clearing imported sessions removes only EMT index entries.
- Deleted imported sessions are recoverable because their source JSONL files remain discoverable by preview.
- Filtering can be frontend-only for this phase.

## Success Criteria

1. Clicking `Import` opens preview and does not mutate `sessions.json`.
2. Preview shows `new` and `existing` candidates.
3. Existing candidates are not selectable by default and are not imported again.
4. Users can filter preview rows by text, exact working directory, and time window.
5. Users can select visible new candidates and import only selected ids.
6. Reopening preview after import marks those imported candidates as existing.
7. `Clear Imported` removes only `source=imported` sessions from EMT.
8. `Clear Imported` does not remove EMT-created sessions or Codex JSONL files.
9. Deleted imported sessions can appear as `new` in preview again.
10. Codex JSONL files are never deleted.

## Architecture

Keep the Phase 2 architecture and extend it surgically:

- `SessionManager` owns the flat session store and Codex history scan logic.
- `App` exposes Wails methods and coordinates PTY cleanup for clear-imported.
- `App.vue` keeps application state local.
- A new `ImportDialog.vue` owns preview filtering and selection UI state.

The key backend change is to make Codex history scanning reusable. Preview and selected import should share one helper so parse behavior, failure counting, duplicate handling, and metadata-derived names stay consistent.

Recommended internal helper:

```go
func scanCodexSessionCandidates(root string) ([]CodexSessionMeta, int, error)
```

The helper walks `root`, parses `.jsonl` files with existing `ParseCodexSessionMeta`, skips non-JSONL files, counts per-file parse/walk failures, and does not mutate `SessionManager.sessions`.

## Data Model

Do not change persisted `Session`.

Add response-only DTOs:

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

type ClearImportedResult struct {
    Cleared int `json:"cleared"`
}
```

Allowed preview status values:

- `new`
- `existing`

`ImportResult` remains:

```go
type ImportResult struct {
    Imported int `json:"imported"`
    Skipped  int `json:"skipped"`
    Failed   int `json:"failed"`
}
```

## Backend Design

### SessionManager

Add:

```go
func (m *SessionManager) PreviewCodexSessions(root string) (ImportPreviewResult, error)
func (m *SessionManager) ImportSelectedCodexSessions(root string, codexSessionIDs []string) (ImportResult, error)
func (m *SessionManager) ClearImportedSessions() (ClearImportedResult, error)
```

`PreviewCodexSessions`:

1. Returns empty result if root is missing or not a directory.
2. Scans candidates using the shared helper.
3. Builds an existing id set from `m.sessions[*].CodexSessionID`.
4. Converts each candidate into `ImportPreviewSession`.
5. Uses `importedSessionName(meta)` for preview name.
6. Sets status to `existing` when `CodexSessionID` is already indexed, otherwise `new`.
7. Sorts by `LastActiveAt` descending.
8. Returns parse failure count.
9. Does not call `SaveSessions`.

`ImportSelectedCodexSessions`:

1. Treats empty input as no-op.
2. Scans candidates using the shared helper.
3. Imports only candidates whose `CodexSessionID` is in the requested set.
4. Skips ids already present in EMT and increments `Skipped`.
5. Appends imported sessions with the same fields as Phase 2 import:
   - `Source=imported`
   - `Status=idle`
   - `CodexSessionPath=meta.Path`
   - `WorkingDir=meta.CWD`
   - `CreatedAt=meta.Timestamp`
   - `LastActiveAt=meta.ModTime`
6. Counts requested ids not found in the scan as `Failed`.
7. Adds scan parse failures to `Failed`.
8. Saves once if any sessions were imported.

`ClearImportedSessions`:

1. Filters out sessions where `Source == SessionSourceImported`.
2. Leaves EMT-created sessions unchanged.
3. Saves once when any imported sessions were removed.
4. Returns `Cleared`.
5. Does not delete `CodexSessionPath`.

The existing `ImportCodexSessions(root string)` can remain for compatibility, but the main UI should not call it.

### App Methods

Expose new Wails methods:

```go
func (a *App) PreviewCodexSessions() (ImportPreviewResult, error)
func (a *App) ImportSelectedCodexSessions(codexSessionIDs []string) (ImportResult, error)
func (a *App) ClearImportedSessions() (ClearImportedResult, error)
```

`PreviewCodexSessions` and `ImportSelectedCodexSessions` resolve `defaultCodexSessionRoot()` and delegate to `SessionManager`.

`ClearImportedSessions` should:

1. Lock and collect current imported session ids.
2. Close matching PTYs best-effort.
3. Lock again and call `SessionManager.ClearImportedSessions`.
4. Return the cleared count.

Closing PTYs should not become a second persisted state model. The important invariant is that cleared sessions disappear from the EMT index and are no longer selectable.

## Frontend Design

### App.vue

Replace direct import behavior with dialog orchestration:

- Replace `importing` one-click state with preview dialog state.
- Keep `sessions`, `selectedId`, `search`, `error`, and `notice` in `App.vue`.
- Add `importDialogOpen`.
- `Import` opens `ImportDialog`.
- `ImportDialog` emits import result or close.
- After successful import, `App.vue` shows `Imported X, skipped Y, failed Z` and calls `loadSessions()`.
- `Clear Imported` calls `ClearImportedSessions()`, refreshes sessions, and adjusts selected session if needed.

### Sidebar.vue

Keep the sidebar dense:

```text
[Search sessions...]

[+ New] [Import] [Clear Imported]
```

`Import` emits an open-dialog event. `Clear Imported` emits a clear event.

Confirmation copy:

```text
Remove all imported sessions from EMT? Codex history files and EMT-created sessions will not be deleted.
```

Result copy:

- `Cleared 42 imported sessions`
- `No imported sessions to clear`

### ImportDialog.vue

Add `frontend/src/components/ImportDialog.vue`.

State:

- `loading`
- `previewSessions`
- `selectedIds`
- `search`
- `timeFilter`
- `workingDirFilter`
- `showExisting`
- `failed`

Default filters:

- recent 30 days
- hide existing
- all directories
- empty search

Layout:

```text
Import Codex sessions                         [x]

[Search name, directory, or Codex id]
[Directory: All v] [Time: Recent 30 days v] [ ] Show existing

Failed to parse: 2

[ ] Name                 Directory          Last active       Status
[ ] project-a 2026...     /tmp/project-a    2026-05-29...    new
[-] project-b 2026...     /tmp/project-b    2026-05-28...    existing

[Select visible] [Clear selection]                  [Import selected]
```

Filtering rules:

- Text search matches `name`, `working_dir`, and `codex_session_id`.
- Working directory filter is exact-match.
- Time filter applies to `last_active_at`.
- Existing sessions are hidden unless `showExisting` is enabled.

Selection rules:

- Only `status === "new"` rows are selectable.
- Existing rows are not checked by default and cannot be selected.
- `Select visible` selects only currently visible new rows.
- `Clear selection` clears all selected ids.
- `Import selected` is disabled when selection is empty.

After import succeeds, close the dialog and refresh the main session list. This keeps synchronization simple and satisfies the concise-result requirement.

## Error Handling

- Missing Codex root returns an empty preview and no error.
- Per-file parse failures increment `Failed` and do not stop the scan.
- Store save failures return errors and leave frontend state unchanged except for showing the error.
- Empty selected import input returns zero counts.
- Requested ids missing from the scan are counted as failed.
- Clear imported with no imported sessions returns `Cleared=0`.
- PTY close during clear is best-effort. A missing or already-exited PTY should not block index cleanup.
- If the selected session is cleared, `loadSessions()` should select the first remaining session or clear selection.

## Testing

Backend tests should cover:

- Preview does not mutate the store.
- Preview marks existing sessions.
- Preview sorts by `last_active_at` descending.
- Preview counts parse failures.
- Import selected imports only requested ids.
- Import selected treats empty ids as no-op.
- Import selected skips already-existing ids.
- Import selected counts missing requested ids as failed.
- Clear imported removes only `source=imported`.
- Clear imported leaves Codex JSONL files untouched.

Frontend/manual checks:

1. Click Import and close the dialog without importing; main list should not change.
2. Confirm preview defaults to recent 30 days and hides existing sessions.
3. Switch to all sessions and confirm more candidates can appear.
4. Filter by text and directory.
5. Select visible rows and import selected.
6. Reopen preview and confirm imported rows are existing.
7. Delete one imported session and confirm it can appear as new again.
8. Clear imported and confirm EMT-created sessions remain.
9. Confirm original JSONL files still exist.

## Non-Goals

- No recycle bin or tombstone model.
- No `deleted_at` state.
- No permanent deletion of Codex JSONL files.
- No semantic search.
- No LLM-generated session summaries.
- No background incremental indexer.
- No Project, Workspace, or Agent entity.
- No persisted import preview.
- No saved filters.
