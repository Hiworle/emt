# Terminal Session Buffer Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Preserve terminal display content when users switch between sessions.

**Architecture:** `PTYManager` owns an in-memory bounded output buffer per session. `TerminalPanel` replays that buffer on mount, then subscribes to live data for the selected session.

**Tech Stack:** Go, Wails v2, Vue 3, TypeScript, xterm.js.

---

### Task 1: Backend Terminal Buffer

**Files:**
- Modify: `pty.go`
- Modify: `app.go`
- Modify: `pty_test.go`
- Modify: `app_test.go`

**Steps:**
1. Write failing tests for buffer append, truncation, read-loop buffering, and `App.TerminalBuffer`.
2. Run targeted tests and confirm they fail for missing methods/state.
3. Add a bounded per-session buffer in `PTYManager`.
4. Append PTY output to the buffer before emitting live data.
5. Add `Buffer(sessionID string) string` and `App.TerminalBuffer(sessionID string) string`.
6. Run targeted tests, then `gofmt`.
7. Commit.

### Task 2: Frontend Replay

**Files:**
- Modify: `frontend/wailsjs/go/main/App.d.ts`
- Modify: `frontend/wailsjs/go/main/App.js`
- Modify: `frontend/src/components/TerminalPanel.vue`

**Steps:**
1. Regenerate or update the Wails binding for `TerminalBuffer`.
2. Update `TerminalPanel` to call `TerminalBuffer(sessionId)` after opening xterm and write the returned data.
3. Store the disposer returned by `EventsOn('terminal:data', ...)` and call that disposer on unmount instead of global `EventsOff('terminal:data')`.
4. Keep live event filtering by `sessionId`.
5. Run `npm run build`.
6. Commit.

### Task 3: Final Verification

**Steps:**
1. Run `GOCACHE=/tmp/go-build-emt-terminal-buffer go test ./...`.
2. Run `cd frontend && npm run build`.
3. Run `wails dev` and manually verify switching sessions preserves terminal output.
4. Stop `wails dev` and clean generated side effects if any.
