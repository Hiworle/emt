<script lang="ts" setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { ResizeTerminal, SendInput } from '../../wailsjs/go/main/App'
import { EventsOff, EventsOn } from '../../wailsjs/runtime/runtime'

const props = defineProps<{
  sessionId: string
}>()

const terminalEl = ref<HTMLDivElement | null>(null)

let terminal: Terminal | null = null
let fitAddon: FitAddon | null = null
let inputDisposable: { dispose: () => void } | null = null

function fitTerminal() {
  if (!terminal || !fitAddon) {
    return
  }

  fitAddon.fit()
  ResizeTerminal(props.sessionId, terminal.rows, terminal.cols).catch(() => undefined)
}

function handleTerminalData(event: { sessionId?: string; data?: string }) {
  if (event.sessionId !== props.sessionId || !event.data) {
    return
  }
  terminal?.write(event.data)
}

onMounted(() => {
  terminal = new Terminal({
    cursorBlink: true,
    fontFamily: '"JetBrains Mono", "SFMono-Regular", Consolas, monospace',
    fontSize: 13,
    lineHeight: 1.2,
    theme: {
      background: '#0b0d10',
      foreground: '#d6dde6',
      cursor: '#f2c14e',
      selectionBackground: '#2f5f74',
    },
  })
  fitAddon = new FitAddon()
  terminal.loadAddon(fitAddon)
  terminal.open(terminalEl.value!)
  terminal.focus()

  inputDisposable = terminal.onData((data) => {
    SendInput(props.sessionId, data).catch(() => undefined)
  })

  EventsOn('terminal:data', handleTerminalData)
  window.addEventListener('resize', fitTerminal)
  requestAnimationFrame(fitTerminal)
})

onUnmounted(() => {
  EventsOff('terminal:data')
  window.removeEventListener('resize', fitTerminal)
  inputDisposable?.dispose()
  terminal?.dispose()
  inputDisposable = null
  terminal = null
  fitAddon = null
})
</script>

<template>
  <section class="terminal-panel">
    <div ref="terminalEl" class="terminal-host"></div>
  </section>
</template>
