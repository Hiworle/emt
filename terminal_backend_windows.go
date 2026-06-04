//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

func defaultTerminalBackend() terminalBackend {
	return windowsConPTYBackend{}
}

func defaultCodexRuntime() codexRuntime {
	return codexRuntimeWSL
}

type windowsConPTYBackend struct{}

func (windowsConPTYBackend) Start(ctx context.Context, command terminalCommand) (terminalProcess, error) {
	inputReader, inputWriter, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outputReader, outputWriter, err := os.Pipe()
	if err != nil {
		_ = inputReader.Close()
		_ = inputWriter.Close()
		return nil, err
	}

	var pc windows.Handle
	size := windows.Coord{X: 80, Y: 24}
	if err := windows.CreatePseudoConsole(size, windows.Handle(inputReader.Fd()), windows.Handle(outputWriter.Fd()), 0, &pc); err != nil {
		_ = inputReader.Close()
		_ = inputWriter.Close()
		_ = outputReader.Close()
		_ = outputWriter.Close()
		return nil, err
	}

	attrList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		windows.ClosePseudoConsole(pc)
		_ = inputReader.Close()
		_ = inputWriter.Close()
		_ = outputReader.Close()
		_ = outputWriter.Close()
		return nil, err
	}
	if err := attrList.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(pc), unsafe.Sizeof(pc)); err != nil {
		attrList.Delete()
		windows.ClosePseudoConsole(pc)
		_ = inputReader.Close()
		_ = inputWriter.Close()
		_ = outputReader.Close()
		_ = outputWriter.Close()
		return nil, err
	}

	args := append([]string{command.Name}, command.Args...)
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		attrList.Delete()
		windows.ClosePseudoConsole(pc)
		_ = inputReader.Close()
		_ = inputWriter.Close()
		_ = outputReader.Close()
		_ = outputWriter.Close()
		return nil, err
	}

	startupInfo := &windows.StartupInfoEx{}
	startupInfo.StartupInfo.Cb = uint32(unsafe.Sizeof(*startupInfo))
	startupInfo.ProcThreadAttributeList = attrList.List()
	var processInfo windows.ProcessInformation
	if err := windows.CreateProcess(
		nil,
		commandLine,
		nil,
		nil,
		false,
		windows.EXTENDED_STARTUPINFO_PRESENT,
		nil,
		nil,
		&startupInfo.StartupInfo,
		&processInfo,
	); err != nil {
		attrList.Delete()
		windows.ClosePseudoConsole(pc)
		_ = inputReader.Close()
		_ = inputWriter.Close()
		_ = outputReader.Close()
		_ = outputWriter.Close()
		return nil, err
	}

	_ = inputReader.Close()
	_ = outputWriter.Close()

	process := &windowsConPTYProcess{
		process:      processInfo.Process,
		thread:       processInfo.Thread,
		pconsole:     pc,
		attrs:        attrList,
		inputWriter:  inputWriter,
		outputReader: outputReader,
	}
	if done := ctx.Done(); done != nil {
		go func() {
			<-done
			_ = process.Close()
		}()
	}
	return process, nil
}

type windowsConPTYProcess struct {
	process      windows.Handle
	thread       windows.Handle
	pconsole     windows.Handle
	attrs        *windows.ProcThreadAttributeListContainer
	inputWriter  *os.File
	outputReader *os.File
	closeOnce    sync.Once
}

func (p *windowsConPTYProcess) Read(buf []byte) (int, error) {
	return p.outputReader.Read(buf)
}

func (p *windowsConPTYProcess) Write(data []byte) (int, error) {
	return p.inputWriter.Write(data)
}

func (p *windowsConPTYProcess) Resize(rows int, cols int) error {
	return windows.ResizePseudoConsole(p.pconsole, windows.Coord{X: int16(cols), Y: int16(rows)})
}

func (p *windowsConPTYProcess) Close() error {
	_ = p.inputWriter.Close()
	_ = p.outputReader.Close()
	_ = windows.TerminateProcess(p.process, 1)
	return nil
}

func (p *windowsConPTYProcess) Wait() error {
	_, err := windows.WaitForSingleObject(p.process, windows.INFINITE)
	if err != nil {
		p.cleanup()
		return err
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(p.process, &exitCode); err != nil {
		p.cleanup()
		return err
	}
	p.cleanup()
	if exitCode != 0 {
		return fmt.Errorf("process exited with code %d", exitCode)
	}
	return nil
}

func (p *windowsConPTYProcess) cleanup() {
	p.closeOnce.Do(func() {
		_ = p.inputWriter.Close()
		_ = p.outputReader.Close()
		windows.ClosePseudoConsole(p.pconsole)
		p.attrs.Delete()
		_ = windows.CloseHandle(p.thread)
		_ = windows.CloseHandle(p.process)
	})
}
