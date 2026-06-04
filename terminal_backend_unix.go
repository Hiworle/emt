//go:build !windows

package main

import (
	"context"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func defaultTerminalBackend() terminalBackend {
	return localPTYBackend{}
}

func defaultCodexRuntime() codexRuntime {
	return codexRuntimeLocal
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
