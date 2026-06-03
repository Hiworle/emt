# Terminal Session Buffer Design

## Problem

Switching sessions destroys the active `TerminalPanel` and its xterm.js buffer. The PTY process keeps running, but EMT only forwards live `terminal:data` events; it does not keep output produced while a session is not mounted.

## Approach

Store a bounded terminal output buffer per session in `PTYManager`. Every PTY read appends to the buffer before emitting the live event. `App` exposes a small Wails method to read the current buffer for a session, and `TerminalPanel` writes that buffer when it mounts before it subscribes to live output.

The buffer is in memory only. It preserves tab-switch display state during the app process lifetime without changing persisted session data or Codex JSONL files.

## Components

- `pty.go`: add per-session ring buffer and a `Buffer(sessionID)` accessor.
- `app.go`: expose `TerminalBuffer(sessionID)`.
- `frontend/wailsjs/go/main/*`: generated binding for the new method.
- `frontend/src/components/TerminalPanel.vue`: replay the buffer on mount and dispose only its own event listener on unmount.

## Constraints

- Keep memory bounded. Use a small byte cap per session, currently 200 KiB.
- Do not persist terminal output to disk.
- Do not alter PTY start/resume semantics.
- Do not remove other listeners when a terminal panel unmounts.

## Verification

- Unit tests cover append, truncation, PTY read buffering, and app API access.
- Frontend build verifies the Wails binding and TypeScript usage.
- Manual smoke verifies that output remains visible after switching away from a session and back.
