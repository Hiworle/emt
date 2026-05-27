# MVP Phase 1 Design

## Goal

Build the first usable EMT loop: create a Codex session, interact with it in an embedded terminal, keep a local session list, and resume a previous EMT-created session after restarting the app.

## Assumptions

- MVP Phase 1 supports macOS and Linux only.
- The working directory is fixed to the EMT process startup directory.
- EMT only manages Codex sessions created through EMT. It does not import or scan all historical Codex sessions.
- Codex CLI resume uses `codex resume <session-id>`, not a session JSONL path.
- The UI should stay minimal: one sidebar, one terminal panel, no project/workspace/agent entities.

## Success Criteria

1. `wails dev` starts the app and shows an empty or persisted session list.
2. Clicking new session starts `codex` in the fixed working directory and attaches it to xterm.js.
3. Terminal input, output, and resize flow through the PTY.
4. Restarting EMT reloads `~/.emt/sessions.json`.
5. Selecting a previous EMT-created session starts `codex resume <session-id>` and attaches it to the terminal.

## Data Model

Keep one business entity: `Session`.

```go
type Session struct {
    ID             string    `json:"id"`
    Name           string    `json:"name"`
    CodexSessionID string    `json:"codex_session_id"`
    WorkingDir     string    `json:"working_dir"`
    CreatedAt      time.Time `json:"created_at"`
    LastActiveAt   time.Time `json:"last_active_at"`
    Status         string    `json:"status"`
}
```

Persist it as:

```json
{
  "sessions": []
}
```

`Status` is only `running`, `idle`, or `error` for the first version. Avoid adding `Workspace`, `Agent`, `Project`, tags, or history entities until a real feature requires them.

## Backend Design

### SessionManager

`SessionManager` owns `~/.emt/sessions.json`.

Responsibilities:

- Load sessions at startup.
- Save sessions after create, resume status change, close, and process exit.
- Create a local EMT session id and default name.
- Update `CodexSessionID` after Codex writes a `session_meta` record.

The first implementation can find `CodexSessionID` by watching new files under `~/.codex/sessions` after spawning `codex`, then parsing the first JSONL line with `type=session_meta`. If this is unreliable, the session remains resumable only after the id is found and the UI should show an error status.

### PTYManager

`PTYManager` owns live PTY processes.

Responsibilities:

- Start new Codex sessions with `codex -C <startup-cwd>`.
- Resume sessions with `codex resume <codex-session-id> -C <startup-cwd>`.
- Relay PTY output to frontend events.
- Accept frontend input and terminal resize.
- Stop a live PTY on close.

Only one PTY per `Session.ID` is allowed. If the user selects an idle session, EMT starts resume. If it is already running, EMT only switches the frontend to that PTY.

### Wails Bindings

Expose only the API needed by the PRD:

```go
CreateSession(name string) (Session, error)
ResumeSession(id string) error
CloseSession(id string) error
ListSessions() ([]Session, error)
SendInput(id string, data string) error
ResizeTerminal(id string, rows int, cols int) error
```

Output events:

- `terminal:data` with `{ sessionId, data }`
- `session:updated` with the updated `Session`
- `terminal:exit` with `{ sessionId, exitCode }`

## Frontend Design

Keep state in `App.vue` for MVP. Do not add Pinia yet.

Components:

- `Sidebar.vue`: session list, selected state, new button, close button.
- `TerminalPanel.vue`: xterm.js instance, fit addon, input/output wiring.
- `App.vue`: loads sessions, holds selected session id, calls Wails bindings.

Interaction:

1. App startup calls `ListSessions()`.
2. New button calls `CreateSession("")`; backend assigns a default name if empty.
3. Clicking a session calls `ResumeSession(id)` when needed, then selects it.
4. xterm input calls `SendInput(selectedId, data)`.
5. resize calls `ResizeTerminal(selectedId, rows, cols)`.

## Error Handling

Keep errors visible and simple.

- If `codex` is not found, mark the session `error` and show a short message in the terminal area.
- If `codex_session_id` is missing, keep the session visible but disable resume with a clear error.
- If the PTY exits, mark the session `idle` unless the spawn failed.
- If `sessions.json` is corrupt, rename it to a `.bak` file and start with an empty list.

## Testing

Backend tests should cover the stable logic:

- Session store load/save round trip.
- Corrupt JSON backup behavior.
- Parsing `session_meta` from a Codex JSONL file.
- Session status transitions when PTY start/exit hooks are called.

Manual verification covers the PTY and Wails integration:

1. Run `wails dev`.
2. Create a session and confirm Codex appears in xterm.
3. Type into the terminal and confirm output returns.
4. Restart the app and confirm the session list persists.
5. Select the previous session and confirm `codex resume <session-id>` starts.

## Non-Goals

- No historical session import.
- No directory picker.
- No multi-agent abstraction.
- No workspace/project model.
- No Windows PTY support in Phase 1.
- No semantic history search or session summarization.
