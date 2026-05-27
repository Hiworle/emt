# MVP Phase 2 Design

## Goal

Move EMT from a minimal Codex terminal wrapper to a practical day-to-day session manager organized by working directory.

Phase 2 adds:

- working directory selection when creating sessions
- import of existing Codex JSONL sessions
- directory-grouped session list with search, rename, close, and delete

It does not add `Project`, `Workspace`, `Agent`, or multi-agent abstractions.

## Current Baseline

Phase 1 has:

- `App` as the Wails binding surface
- `SessionManager` for `~/.emt/sessions.json`
- `PTYManager` for Codex PTY processes
- one `Session` entity
- `App.vue` holding session state locally
- `Sidebar.vue` and `TerminalPanel.vue`

Phase 2 should extend these files and patterns rather than introducing a new architecture.

## Data Model

Keep `Session` as the only business entity.

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

Constants:

- `SessionSourceEMT = "emt"`
- `SessionSourceImported = "imported"`
- existing status constants remain `running`, `idle`, `error`

Compatibility rules:

- Empty `Source` loads as `emt`.
- Empty `CodexSessionPath` remains valid.
- `CodexSessionID` remains the resume key.
- `CodexSessionPath` is metadata for import, duplicate diagnosis, and user confidence.

The JSON store remains:

```json
{
  "sessions": []
}
```

No additional top-level arrays are added.

## Backend Design

### SessionManager

`SessionManager` remains responsible for loading and saving the flat session list.

Add minimal methods:

```go
func (m *SessionManager) RenameSession(id string, name string) (Session, error)
func (m *SessionManager) DeleteSession(id string) (Session, error)
func (m *SessionManager) ImportCodexSessions(root string) (ImportResult, error)
```

These methods should update `m.sessions` and persist changes through the existing `SaveSessions` path.

`LoadSessions` should normalize loaded data:

- convert any persisted `running` status to `idle`
- fill empty `Source` with `emt`

This keeps startup normalization in one place and avoids spreading migration logic through `App`.

### Codex JSONL Parsing

Extend `CodexSessionMeta`:

```go
type CodexSessionMeta struct {
    ID        string
    CWD       string
    Timestamp time.Time
    Path      string
    ModTime   time.Time
}
```

`ParseCodexSessionMeta(path string)` should:

1. read JSONL line by line
2. ignore malformed lines until a valid `session_meta` is found
3. extract `payload.id`, `payload.cwd`, and top-level `timestamp`
4. return an error if no valid `session_meta` with id exists

Timestamp fallback:

- use parsed top-level `timestamp` when available
- otherwise use file mod time

### Import Flow

`ImportCodexSessions(root string)` walks `~/.codex/sessions` recursively.

Rules:

1. Missing root is not fatal and returns zero counts.
2. Only `.jsonl` files are inspected.
3. Existing sessions are indexed by `CodexSessionID`.
4. Duplicate ids increment `Skipped`.
5. Parse failures increment `Failed`.
6. Imported sessions are appended with:
   - `Source=imported`
   - `Status=idle`
   - `CodexSessionPath=path`
   - `WorkingDir=meta.CWD`
   - `CreatedAt=meta.Timestamp or mod time`
   - `LastActiveAt=mod time`
7. Save once after the scan completes.

Session id generation for imports can stay local and deterministic enough:

```text
imported-<codex-session-id>
```

If an id collision occurs with an existing EMT id, append a short suffix. Do not introduce a separate id mapping table.

Imported name:

```text
<working-dir-basename> <yyyy-mm-dd hh:mm>
```

If `working_dir` is empty, use `Imported <yyyy-mm-dd hh:mm>`.

### App Methods

Expose Phase 2 Wails methods:

```go
CreateSession(name string, workingDir string) (Session, error)
ImportCodexSessions() (ImportResult, error)
RenameSession(id string, name string) (Session, error)
DeleteSession(id string) error
ChooseWorkingDir(defaultDir string) (string, error)
```

Keep existing methods:

```go
ListSessions() ([]Session, error)
ResumeSession(id string) error
CloseSession(id string) error
SendInput(id string, data string) error
ResizeTerminal(id string, rows int, cols int) error
```

`CreateSession` changes:

- accepts `workingDir`
- trims `name`
- uses startup directory when `workingDir` is empty
- validates that `workingDir` exists and is a directory
- starts Codex with `codex -C <workingDir>`
- saves with `Source=emt`
- discovers both `CodexSessionID` and `CodexSessionPath`

Important fix:

`discoverCodexSessionID` must match against the session's `WorkingDir`, not `a.workDir`.

`DeleteSession`:

- closes the PTY if running
- removes only the EMT index entry
- does not delete `CodexSessionPath`

`ChooseWorkingDir`:

- uses Wails directory dialog
- returns an empty string if user cancels
- frontend keeps manual text entry as fallback

## Frontend Design

Keep state in `App.vue`. Do not add Pinia or routing in Phase 2.

Core state:

```ts
const sessions = ref<Session[]>([])
const selectedId = ref('')
const search = ref('')
const error = ref('')
const notice = ref('')
const importing = ref(false)
const newDialogOpen = ref(false)
```

Derived state:

```ts
type SessionGroup = {
  workingDir: string
  latestActiveAt: number
  sessions: Session[]
}
```

`groupedSessions` should:

1. filter by session name, working directory, or Codex session id
2. group by `working_dir`
3. sort sessions by `last_active_at` descending
4. sort groups by each group's latest session activity

### Components

Keep existing:

- `App.vue`
- `Sidebar.vue`
- `TerminalPanel.vue`

Add:

- `NewSessionDialog.vue`

Optional if implementation stays simpler:

- `RenameSessionDialog.vue`

Do not add a project page, settings page, or global store.

### Sidebar

Sidebar layout:

```text
[Search sessions...]

[+ New] [Import]

/path/to/project
  Session name       running   EMT
  Imported session   idle      Imported
```

`Sidebar.vue` receives grouped sessions rather than computing backend state.

Suggested props:

```ts
defineProps<{
  groups: SessionGroup[]
  selectedId: string
  search: string
  importing: boolean
}>()
```

Suggested emits:

```ts
defineEmits<{
  (event: 'update:search', value: string): void
  (event: 'new-session'): void
  (event: 'import-sessions'): void
  (event: 'select-session', id: string): void
  (event: 'rename-session', id: string): void
  (event: 'close-session', id: string): void
  (event: 'delete-session', id: string): void
}>()
```

Session row actions can use a compact `...` action area. Avoid a reusable menu abstraction unless the simple version becomes awkward.

### New Session Dialog

Fields:

- optional session name
- working directory text input
- `Browse` button

Default working directory:

1. most recently active session's `working_dir`
2. empty string, letting backend fall back to startup directory

Submit:

```ts
CreateSession(name, workingDir)
```

On success:

- upsert returned session
- select it
- close dialog

### Import Interaction

Import button:

1. set `importing=true`
2. call `ImportCodexSessions()`
3. call `ListSessions()`
4. show `Imported X, skipped Y, failed Z`
5. set `importing=false`

Repeated clicks while importing are disabled.

### Rename and Delete

Rename:

- trim name on backend
- reject empty backend-side
- frontend can use a small dialog or inline edit

Delete:

- show confirmation text: `Remove this session from EMT? Codex history files will not be deleted.`
- call `DeleteSession(id)`
- remove from local `sessions`
- if selected session was deleted, select the first remaining session or clear selection

### TerminalPanel

No major changes.

Known acceptable Phase 2 behavior:

- only one `TerminalPanel` is mounted at a time
- `EventsOff('terminal:data')` on unmount remains acceptable because no other terminal listener exists

## Error Handling

- Missing or invalid working directory: show backend error.
- Browse canceled: keep current input value.
- Import root missing: show `Imported 0, skipped 0, failed 0`.
- Malformed JSONL: count as failed, continue.
- Duplicate import: count as skipped.
- Resume without `codex_session_id`: show visible error.
- Delete missing session: show visible error and refresh list if needed.

## Testing Strategy

Backend tests should carry most of Phase 2 risk.

Automated tests:

- Phase 1 session JSON loads with `Source=emt`.
- `ParseCodexSessionMeta` extracts id, cwd, timestamp, path, and mod time.
- Missing timestamp falls back to mod time.
- Import appends valid sessions.
- Import skips duplicate `CodexSessionID`.
- Import counts malformed or missing-meta files as failed.
- Import is idempotent across repeated runs.
- Rename rejects empty names.
- Rename persists valid names.
- Delete removes only EMT metadata and leaves JSONL files untouched.
- `CreateSession` chooses passed `workingDir` and falls back to startup directory when empty.
- Codex discovery matches the session `WorkingDir`.

Manual validation:

1. Run `wails dev`.
2. Create a session with a non-startup working directory.
3. Confirm terminal starts Codex in that directory.
4. Restart and confirm directory grouping persists.
5. Import existing Codex sessions.
6. Repeat import and confirm no duplicate list entries.
7. Search by name, path, and Codex session id.
8. Rename a session.
9. Delete a session and confirm the JSONL file remains.
10. Resume an imported session.

## Risks and Constraints

- Large `~/.codex/sessions` trees may make synchronous import slow. Phase 2 accepts a blocking import with loading state; background import is deferred until needed.
- Codex JSONL format may change. Phase 2 depends only on `session_meta.type`, `payload.id`, `payload.cwd`, and top-level `timestamp`.
- Directory dialog availability may vary. Text input remains the fallback.
- Duplicate names are allowed. No uniqueness constraint is added.
- Nonexistent working directories are rejected. EMT does not create directories.

## Non-Goals Reaffirmed

- No Project entity.
- No Agent entity.
- No Pinia store.
- No route-level navigation.
- No import preview.
- No deletion of Codex JSONL history.
- No semantic search.
- No IDE jump.
