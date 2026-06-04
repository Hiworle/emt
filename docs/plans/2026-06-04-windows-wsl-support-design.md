# Windows WSL Support Design

## Goal

Support running EMT as a native Windows Wails application while keeping Codex, working directories, Codex history, and EMT session metadata inside the default WSL distribution.

The primary user-visible outcome is that Windows EMT uses Windows WebView2 and can use the Windows IME, while terminal sessions still execute `codex` in WSL.

## Confirmed Scope

- Use the default WSL distribution only.
- Accept WSL absolute working directory paths only, for example `/home/hope/proj/emt`.
- Keep EMT's session index in WSL at `~/.emt/sessions.json`.
- Keep Codex history discovery in WSL at `~/.codex/sessions`.
- Do not add a distro selector, project/workspace entity, or Windows path conversion in the first version.
- Do not support Windows-native Codex in this version.

## Recommended Approach

Use a Windows-specific terminal backend based on Windows ConPTY. The Windows backend starts `wsl.exe` inside the pseudo terminal and runs Codex in the default WSL distribution.

Alternative approaches were considered:

1. Plain stdin/stdout pipes to `wsl.exe`: simpler, but not a real TTY and risky for interactive CLI behavior.
2. WSL helper/server: can reuse Linux PTY code, but introduces lifecycle, port, and protocol complexity.

ConPTY is the best first Windows backend because it preserves real terminal behavior, resize support, and control-key behavior while allowing the app window to remain native Windows.

## Architecture

Introduce two backend boundaries:

1. `TerminalBackend`
   - Linux/macOS implementation keeps using `github.com/creack/pty`.
   - Windows implementation uses ConPTY and starts `wsl.exe`.

2. `SessionStore`
   - Unix implementation reads and writes local files directly.
   - Windows implementation reads and writes WSL files through `wsl.exe` command execution.

The current `Session` entity remains the only persisted business entity. `working_dir` continues to store WSL paths, not Windows paths.

## Terminal Data Flow

New session on Windows:

```text
CreateSession(name, workingDir)
  -> validate workingDir starts with "/"
  -> start ConPTY
  -> run: wsl.exe --cd <workingDir> codex -C <workingDir>
  -> ConPTY stdout/stderr -> terminal:data event -> xterm.js
  -> xterm.js input -> SendInput -> ConPTY stdin
  -> discover Codex session metadata in WSL
  -> save session to WSL ~/.emt/sessions.json
```

Resume session on Windows:

```text
ResumeSession(id)
  -> load codex_session_id and working_dir from WSL session store
  -> start ConPTY
  -> run: wsl.exe --cd <workingDir> codex resume <codexSessionID> -C <workingDir>
```

Linux and macOS keep the existing commands:

```text
codex -C <workingDir>
codex resume <codexSessionID> -C <workingDir>
```

## Session Store Data Flow

On Windows, EMT should not create an independent `%USERPROFILE%\.emt\sessions.json`. It should read and write the default WSL distribution's `~/.emt/sessions.json`.

The Windows session store should use a command runner abstraction so tests can mock WSL:

```text
wsl.exe sh -lc 'cat ~/.emt/sessions.json'
wsl.exe sh -lc 'mkdir -p ~/.emt && cat > ~/.emt/sessions.json.tmp && mv ~/.emt/sessions.json.tmp ~/.emt/sessions.json'
```

The exact write implementation should avoid shell-escaping file contents by streaming JSON to stdin.

Codex import and preview should also use WSL as the source of truth. The first version can list and read WSL JSONL files through `wsl.exe` and parse them in the Windows Go process. A persistent helper daemon is out of scope.

## Path Rules

First version path handling is intentionally strict:

- Valid working directories must be absolute WSL paths beginning with `/`.
- Windows paths such as `C:\Users\...` are rejected.
- UNC paths such as `\\wsl$\...` are rejected.
- `ChooseWorkingDir` on Windows should not use the native Windows directory picker for the first version, because it returns Windows paths. It can return a clear error and let the existing text input remain the path entry mechanism.

## Error Handling

Errors should be explicit and visible:

- Missing `wsl.exe`: `WSL is not available on this Windows system`.
- Default WSL distribution unavailable: ask the user to run `wsl.exe` in Windows Terminal and configure a default distribution.
- `codex` missing inside WSL: `codex was not found in the default WSL distribution`.
- Invalid working directory: require a WSL absolute path.
- `wsl.exe --cd <workingDir>` failure: show the WSL error summary.
- ConPTY creation failure: show a Windows terminal backend initialization error.
- WSL session store read failure: keep startup controlled and show the error instead of silently switching to a Windows store.
- WSL session store write failure: do not update UI as though the operation succeeded.

## Testing Strategy

Automated Go tests should cover:

- Windows command construction:
  - new session: `wsl.exe --cd /path codex -C /path`
  - resume: `wsl.exe --cd /path codex resume <id> -C /path`
- WSL absolute path validation.
- Session store abstraction:
  - Unix local file store behavior.
  - Windows WSL command runner behavior using a fake runner.
- Terminal backend selection and PTY manager behavior using a fake backend.
- Existing Linux behavior remains unchanged.

Manual Windows verification should cover:

- `wails dev` starts a native Windows window.
- Windows IME can input Chinese into the xterm terminal.
- Creating a Codex session starts Codex inside WSL.
- Restarting EMT reloads sessions from WSL `~/.emt/sessions.json`.
- Resume works for an existing Codex session.
- Import preview reads WSL `~/.codex/sessions`.

## Delivery Plan

Implement in four phases:

1. Add `TerminalBackend` and `SessionStore` abstractions without changing Linux behavior.
2. Add the Windows WSL session store.
3. Add the Windows ConPTY terminal backend.
4. Adjust Windows path validation, `ChooseWorkingDir`, and error messages.

This sequence keeps the first changes testable on Linux and leaves the Windows-specific implementation isolated behind small interfaces.
