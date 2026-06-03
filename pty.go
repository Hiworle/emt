package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

type TerminalDataHandler func(sessionID string, data string)
type TerminalExitHandler func(sessionID string, err error)

const terminalBufferLimit = 200 * 1024

type PTYManager struct {
	mu      sync.Mutex
	terms   map[string]*ptySession
	buffers map[string][]byte
	onData  TerminalDataHandler
	onExit  TerminalExitHandler
}

type ptySession struct {
	cmd  *exec.Cmd
	file *os.File
}

func NewPTYManager(onData TerminalDataHandler, onExit TerminalExitHandler) *PTYManager {
	return &PTYManager{
		terms:   make(map[string]*ptySession),
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

func (m *PTYManager) StartNew(ctx context.Context, sessionID string, workingDir string) error {
	return m.start(ctx, sessionID, codexNewArgs(workingDir))
}

func (m *PTYManager) Resume(ctx context.Context, session Session) error {
	if session.CodexSessionID == "" {
		return errors.New("codex session id is empty")
	}
	return m.start(ctx, session.ID, codexResumeArgs(session.CodexSessionID, session.WorkingDir))
}

func (m *PTYManager) start(ctx context.Context, sessionID string, args []string) error {
	m.mu.Lock()
	if _, ok := m.terms[sessionID]; ok {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	cmd := exec.CommandContext(ctx, "codex", args...)
	file, err := pty.Start(cmd)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.terms[sessionID] = &ptySession{cmd: cmd, file: file}
	m.mu.Unlock()

	go m.readLoop(sessionID, file)
	go m.waitLoop(sessionID, cmd)
	return nil
}

func (m *PTYManager) Write(sessionID string, data string) error {
	m.mu.Lock()
	term := m.terms[sessionID]
	m.mu.Unlock()
	if term == nil {
		return errors.New("terminal is not running")
	}
	_, err := io.WriteString(term.file, data)
	return err
}

func (m *PTYManager) Resize(sessionID string, rows int, cols int) error {
	m.mu.Lock()
	term := m.terms[sessionID]
	m.mu.Unlock()
	if term == nil {
		return errors.New("terminal is not running")
	}
	return pty.Setsize(term.file, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
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
	return term.file.Close()
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

func (m *PTYManager) readLoop(sessionID string, file *os.File) {
	buf := make([]byte, 4096)
	for {
		n, err := file.Read(buf)
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

func (m *PTYManager) waitLoop(sessionID string, cmd *exec.Cmd) {
	err := cmd.Wait()
	m.mu.Lock()
	delete(m.terms, sessionID)
	m.mu.Unlock()
	if m.onExit != nil {
		m.onExit(sessionID, err)
	}
}
