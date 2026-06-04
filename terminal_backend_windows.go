//go:build windows

package main

func defaultTerminalBackend() terminalBackend {
	return windowsConPTYBackend{}
}

func defaultCodexRuntime() codexRuntime {
	return codexRuntimeWSL
}
