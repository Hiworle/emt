<script lang="ts" setup>
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { ResizeTerminal, SendInput, TerminalBuffer } from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'

const props = defineProps<{
  sessionId: string
  active: boolean
}>()

const terminalEl = ref<HTMLDivElement | null>(null)

let terminal: Terminal | null = null
let fitAddon: FitAddon | null = null
let inputDisposable: { dispose: () => void } | null = null
let terminalDataDisposable: (() => void) | null = null

function fitTerminal() {
  if (!terminal || !fitAddon || !props.active) {
    return
  }

  fitAddon.fit()
  ResizeTerminal(props.sessionId, terminal.rows, terminal.cols).catch(() => undefined)
}

async function fitActiveTerminal() {
  if (!props.active) {
    return
  }
  await nextTick()
  requestAnimationFrame(() => {
    fitTerminal()
    terminal?.focus()
  })
}

function handleTerminalData(event: { sessionId?: string; data?: string }) {
  if (event.sessionId !== props.sessionId || !event.data) {
    return
  }
  terminal?.write(event.data)
}

onMounted(async () => {
  const sessionId = props.sessionId
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
  if (props.active) {
    terminal.focus()
  }

  try {
    const buffer = await TerminalBuffer(sessionId)
    if (terminal && props.sessionId === sessionId && buffer) {
      terminal.write(buffer)
    }
  } catch {
    // Live terminal output still works if history replay fails.
  }
  if (!terminal || props.sessionId !== sessionId) {
    return
  }

  inputDisposable = terminal.onData((data) => {
    SendInput(props.sessionId, data).catch(() => undefined)
  })

  terminalDataDisposable = EventsOn('terminal:data', handleTerminalData)
  window.addEventListener('resize', fitTerminal)
  fitActiveTerminal()
})

watch(() => props.active, fitActiveTerminal)

onUnmounted(() => {
  terminalDataDisposable?.()
  window.removeEventListener('resize', fitTerminal)
  inputDisposable?.dispose()
  terminalDataDisposable = null
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
