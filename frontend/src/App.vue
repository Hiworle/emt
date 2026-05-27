<script lang="ts" setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { CloseSession, CreateSession, ListSessions, ResumeSession } from '../wailsjs/go/main/App'
import * as models from '../wailsjs/go/models'
import { EventsOff, EventsOn } from '../wailsjs/runtime/runtime'
import Sidebar from './components/Sidebar.vue'
import TerminalPanel from './components/TerminalPanel.vue'

type Session = models.main.Session

const sessions = ref<Session[]>([])
const selectedId = ref('')
const error = ref('')

const selectedSession = computed(() => sessions.value.find((session) => session.id === selectedId.value))

function toSession(source: Session): Session {
  return models.main.Session.createFrom(source)
}

function upsertSession(source: Session) {
  const session = toSession(source)
  const index = sessions.value.findIndex((item) => item.id === session.id)
  if (index === -1) {
    sessions.value = [...sessions.value, session]
    return
  }
  sessions.value.splice(index, 1, session)
}

async function loadSessions() {
  sessions.value = (await ListSessions()).map(toSession)
  if (!selectedId.value && sessions.value.length > 0) {
    selectedId.value = sessions.value[0].id
  }
}

async function createSession() {
  error.value = ''
  try {
    const session = await CreateSession('')
    upsertSession(session)
    selectedId.value = session.id
  } catch (err) {
    error.value = String(err)
  }
}

async function selectSession(id: string) {
  error.value = ''
  selectedId.value = id

  const session = sessions.value.find((item) => item.id === id)
  if (!session || session.status === 'running' || !session.codex_session_id) {
    return
  }

  try {
    await ResumeSession(id)
  } catch (err) {
    error.value = String(err)
  }
}

async function closeSession(id: string) {
  error.value = ''
  try {
    await CloseSession(id)
  } catch (err) {
    error.value = String(err)
  }
}

function handleSessionUpdated(session: Session) {
  upsertSession(session)
}

function handleTerminalExit(event: { sessionId?: string; error?: string }) {
  if (event.sessionId === selectedId.value && event.error) {
    error.value = event.error
  }
}

onMounted(async () => {
  await loadSessions()
  EventsOn('session:updated', handleSessionUpdated)
  EventsOn('terminal:exit', handleTerminalExit)
})

onUnmounted(() => {
  EventsOff('session:updated')
  EventsOff('terminal:exit')
})
</script>

<template>
  <div class="app-shell">
    <Sidebar
      :sessions="sessions"
      :selected-id="selectedId"
      @new-session="createSession"
      @select-session="selectSession"
      @close-session="closeSession"
    />

    <main class="workspace">
      <div class="topbar">
        <div>
          <div class="session-title">{{ selectedSession?.name || 'No session selected' }}</div>
          <div class="session-path">{{ selectedSession?.working_dir || '' }}</div>
        </div>
        <div v-if="error" class="error-message">{{ error }}</div>
      </div>

      <TerminalPanel v-if="selectedId" :key="selectedId" :session-id="selectedId" />
      <div v-else class="empty-terminal">No session selected</div>
    </main>
  </div>
</template>
