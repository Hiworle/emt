<script lang="ts" setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import {
  ChooseWorkingDir,
  CloseSession,
  CreateSession,
  DeleteSession,
  ImportCodexSessions,
  ListSessions,
  RenameSession,
  ResumeSession,
} from '../wailsjs/go/main/App'
import * as models from '../wailsjs/go/models'
import { EventsOff, EventsOn } from '../wailsjs/runtime/runtime'
import NewSessionDialog from './components/NewSessionDialog.vue'
import RenameSessionDialog from './components/RenameSessionDialog.vue'
import Sidebar from './components/Sidebar.vue'
import TerminalPanel from './components/TerminalPanel.vue'

type Session = models.main.Session
type SessionGroup = {
  workingDir: string
  sessions: Session[]
  lastActiveAt: number
}

const sessions = ref<Session[]>([])
const selectedId = ref('')
const error = ref('')
const search = ref('')
const notice = ref('')
const importing = ref(false)
const newDialogOpen = ref(false)
const defaultWorkingDir = ref('')
const renameDialogOpen = ref(false)
const renamingSessionId = ref('')

const selectedSession = computed(() => sessions.value.find((session) => session.id === selectedId.value))
const renamingSession = computed(() =>
  sessions.value.find((session) => session.id === renamingSessionId.value),
)
const groupedSessions = computed<SessionGroup[]>(() => {
  const query = search.value.trim().toLowerCase()
  const groups = new Map<string, SessionGroup>()

  for (const session of sessions.value) {
    const fields = [session.name, session.working_dir, session.codex_session_id]
      .filter(Boolean)
      .join(' ')
      .toLowerCase()
    if (query && !fields.includes(query)) {
      continue
    }

    const workingDir = session.working_dir || '(unknown directory)'
    const lastActiveAt = timestampValue(session.last_active_at)
    const group = groups.get(workingDir) || { workingDir, sessions: [], lastActiveAt: 0 }
    group.sessions.push(session)
    group.lastActiveAt = Math.max(group.lastActiveAt, lastActiveAt)
    groups.set(workingDir, group)
  }

  return Array.from(groups.values())
    .map((group) => ({
      ...group,
      sessions: [...group.sessions].sort(
        (a, b) => timestampValue(b.last_active_at) - timestampValue(a.last_active_at),
      ),
    }))
    .sort((a, b) => b.lastActiveAt - a.lastActiveAt)
})

function timestampValue(value: unknown): number {
  if (value instanceof Date) {
    return value.getTime()
  }
  if (typeof value === 'string') {
    const parsed = Date.parse(value)
    return Number.isNaN(parsed) ? 0 : parsed
  }
  if (typeof value === 'number') {
    return value
  }
  return 0
}

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
  if (selectedId.value && !sessions.value.some((session) => session.id === selectedId.value)) {
    selectedId.value = ''
  }
  if (!selectedId.value && sessions.value.length > 0) {
    selectedId.value = sessions.value[0].id
  }
}

function openNewSessionDialog() {
  defaultWorkingDir.value = selectedSession.value?.working_dir || ''
  newDialogOpen.value = true
}

async function createSession(payload: { name: string; workingDir: string }) {
  error.value = ''
  notice.value = ''
  try {
    const session = await CreateSession(payload.name, payload.workingDir)
    upsertSession(session)
    selectedId.value = session.id
    defaultWorkingDir.value = session.working_dir
    newDialogOpen.value = false
  } catch (err) {
    error.value = String(err)
  }
}

async function browseWorkingDir(currentDir: string) {
  error.value = ''
  notice.value = ''
  try {
    const directory = await ChooseWorkingDir(currentDir || defaultWorkingDir.value)
    if (directory) {
      defaultWorkingDir.value = directory
    }
  } catch (err) {
    error.value = String(err)
  }
}

async function importSessions() {
  importing.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await ImportCodexSessions()
    notice.value = `Imported ${result.imported}, skipped ${result.skipped}, failed ${result.failed}`
    await loadSessions()
  } catch (err) {
    error.value = String(err)
  } finally {
    importing.value = false
  }
}

async function selectSession(id: string) {
  error.value = ''
  notice.value = ''
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
  notice.value = ''
  try {
    await CloseSession(id)
  } catch (err) {
    error.value = String(err)
  }
}

function openRenameDialog(id: string) {
  const session = sessions.value.find((item) => item.id === id)
  if (!session) {
    return
  }
  renamingSessionId.value = id
  renameDialogOpen.value = true
  error.value = ''
  notice.value = ''
}

async function renameSession(name: string) {
  if (!renamingSessionId.value) {
    return
  }
  error.value = ''
  notice.value = ''
  try {
    const session = await RenameSession(renamingSessionId.value, name)
    upsertSession(session)
    renameDialogOpen.value = false
    renamingSessionId.value = ''
  } catch (err) {
    error.value = String(err)
  }
}

async function deleteSession(id: string) {
  if (!window.confirm('Remove this session from EMT? Codex history files will not be deleted.')) {
    return
  }

  error.value = ''
  notice.value = ''
  try {
    await DeleteSession(id)
    sessions.value = sessions.value.filter((session) => session.id !== id)
    if (selectedId.value === id) {
      selectedId.value = sessions.value[0]?.id || ''
    }
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
      v-model:search="search"
      :groups="groupedSessions"
      :importing="importing"
      :selected-id="selectedId"
      @import-sessions="importSessions"
      @new-session="openNewSessionDialog"
      @select-session="selectSession"
      @close-session="closeSession"
      @rename-session="openRenameDialog"
      @delete-session="deleteSession"
    />

    <main class="workspace">
      <div class="topbar">
        <div>
          <div class="session-title">{{ selectedSession?.name || 'No session selected' }}</div>
          <div class="session-path">{{ selectedSession?.working_dir || '' }}</div>
        </div>
        <div class="topbar-message">
          <div v-if="error" class="error-message">{{ error }}</div>
          <div v-else-if="notice" class="notice-message">{{ notice }}</div>
        </div>
      </div>

      <TerminalPanel v-if="selectedId" :key="selectedId" :session-id="selectedId" />
      <div v-else class="empty-terminal">No session selected</div>
    </main>

    <NewSessionDialog
      :open="newDialogOpen"
      :default-working-dir="defaultWorkingDir"
      @close="newDialogOpen = false"
      @create="createSession"
      @browse="browseWorkingDir"
    />
    <RenameSessionDialog
      :open="renameDialogOpen"
      :name="renamingSession?.name || ''"
      @close="renameDialogOpen = false"
      @rename="renameSession"
    />
  </div>
</template>
