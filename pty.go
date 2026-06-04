package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/creack/pty"
)

type TerminalDataHandler func(sessionID string, data string)
type TerminalExitHandler func(sessionID string, err error)

const terminalBufferLimit = 200 * 1024

type PTYManager struct {
	mu      sync.Mutex
	backend terminalBackend
	runtime codexRuntime
	terms   map[string]terminalProcess
	buffers map[string][]byte
	onData  TerminalDataHandler
	onExit  TerminalExitHandler
}

type terminalBackend interface {
	Start(ctx context.Context, command terminalCommand) (terminalProcess, error)
}

type terminalProcess interface {
	io.Reader
	io.Writer
	Resize(rows int, cols int) error
	Close() error
	Wait() error
}

func NewPTYManager(onData TerminalDataHandler, onExit TerminalExitHandler) *PTYManager {
	return NewPTYManagerWithBackend(localPTYBackend{}, codexRuntimeLocal, onData, onExit)
}

func NewPTYManagerWithBackend(backend terminalBackend, runtime codexRuntime, onData TerminalDataHandler, onExit TerminalExitHandler) *PTYManager {
	return &PTYManager{
		backend: backend,
		runtime: runtime,
		terms:   make(map[string]terminalProcess),
		buffers: make(map[string][]byte),
		onData:  onData,
		onExit:  onExit,
	}
}

func codexNewArgs(workingDir string) []string {
	return []string{"-C", workingDir}
}

func codexResumeArgs(codexSessionID string, workingDir string) []string {
	return []string{"resume", codexSessionID, "-C", workingDir}
}

type codexRuntime string

const (
	codexRuntimeLocal codexRuntime = "local"
	codexRuntimeWSL   codexRuntime = "wsl"
)

type terminalCommand struct {
	Name string
	Args []string
}

func codexNewCommand(runtime codexRuntime, workingDir string) terminalCommand {
	args := codexNewArgs(workingDir)
	if runtime == codexRuntimeWSL {
		return terminalCommand{
			Name: "wsl.exe",
			Args: append([]string{"--cd", workingDir, "codex"}, args...),
		}
	}
	return terminalCommand{Name: "codex", Args: args}
}

func codexResumeCommand(runtime codexRuntime, codexSessionID string, workingDir string) terminalCommand {
	args := codexResumeArgs(codexSessionID, workingDir)
	if runtime == codexRuntimeWSL {
		return terminalCommand{
			Name: "wsl.exe",
			Args: append([]string{"--cd", workingDir, "codex"}, args...),
		}
	}
	return terminalCommand{Name: "codex", Args: args}
}

func validateWSLWorkingDir(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return errors.New("working directory must be a WSL absolute path")
	}
	return nil
}

func (m *PTYManager) StartNew(ctx context.Context, sessionID string, workingDir string) error {
	return m.start(ctx, sessionID, codexNewCommand(m.runtime, workingDir))
}

func (m *PTYManager) Resume(ctx context.Context, session Session) error {
	if session.CodexSessionID == "" {
		return errors.New("codex session id is empty")
	}
	return m.start(ctx, session.ID, codexResumeCommand(m.runtime, session.CodexSessionID, session.WorkingDir))
}

func (m *PTYManager) start(ctx context.Context, sessionID string, command terminalCommand) error {
	m.mu.Lock()
	if _, ok := m.terms[sessionID]; ok {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	process, err := m.backend.Start(ctx, command)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.terms[sessionID] = process
	m.mu.Unlock()

	go m.readLoop(sessionID, process)
	go m.waitLoop(sessionID, process)
	return nil
}

func (m *PTYManager) Write(sessionID string, data string) error {
	m.mu.Lock()
	term := m.terms[sessionID]
	m.mu.Unlock()
	if term == nil {
		return errors.New("terminal is not running")
	}
	_, err := term.Write([]byte(data))
	return err
}

func (m *PTYManager) Resize(sessionID string, rows int, cols int) error {
	m.mu.Lock()
	term := m.terms[sessionID]
	m.mu.Unlock()
	if term == nil {
		return errors.New("terminal is not running")
	}
	return term.Resize(rows, cols)
}

func (m *PTYManager) Buffer(sessionID string) string {
	m.mu.Lock()
	buffer := append([]byte(nil), m.buffers[sessionID]...)
	m.mu.Unlock()
	return string(buffer)
}

func (m *PTYManager) Close(sessionID string) error {
	m.mu.Lock()
	term := m.terms[sessionID]
	delete(m.terms, sessionID)
	m.mu.Unlock()
	if term == nil {
		return nil
	}
	return term.Close()
}

func (m *PTYManager) CloseAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.terms))
	for id := range m.terms {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Close(id)
	}
}

func (m *PTYManager) readLoop(sessionID string, reader io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			data := string(buf[:n])
			m.appendBuffer(sessionID, data)
			if m.onData != nil {
				m.onData(sessionID, data)
			}
		}
		if err != nil {
			return
		}
	}
}

func (m *PTYManager) appendBuffer(sessionID string, data string) {
	if data == "" {
		return
	}

	m.mu.Lock()
	buffer := append(m.buffers[sessionID], []byte(data)...)
	if len(buffer) > terminalBufferLimit {
		buffer = buffer[len(buffer)-terminalBufferLimit:]
	}
	m.buffers[sessionID] = append([]byte(nil), buffer...)
	m.mu.Unlock()
}

func (m *PTYManager) waitLoop(sessionID string, process terminalProcess) {
	err := process.Wait()
	m.mu.Lock()
	delete(m.terms, sessionID)
	m.mu.Unlock()
	if m.onExit != nil {
		m.onExit(sessionID, err)
	}
}

type localPTYBackend struct{}

func (localPTYBackend) Start(ctx context.Context, command terminalCommand) (terminalProcess, error) {
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	file, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	return &localPTYProcess{cmd: cmd, file: file}, nil
}

type localPTYProcess struct {
	cmd  *exec.Cmd
	file *os.File
}

func (p *localPTYProcess) Read(buf []byte) (int, error) { return p.file.Read(buf) }
func (p *localPTYProcess) Write(data []byte) (int, error) {
	return p.file.Write(data)
}
func (p *localPTYProcess) Resize(rows int, cols int) error {
	return pty.Setsize(p.file, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}
func (p *localPTYProcess) Close() error { return p.file.Close() }
func (p *localPTYProcess) Wait() error  { return p.cmd.Wait() }
