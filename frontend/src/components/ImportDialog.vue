<script lang="ts" setup>
import { computed, ref, watch } from 'vue'
import {
  ImportSelectedCodexSessions,
  PreviewCodexSessions,
} from '../../wailsjs/go/main/App'
import * as models from '../../wailsjs/go/models'

type ImportPreviewSession = models.main.ImportPreviewSession

const props = defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'imported', result: models.main.ImportResult): void
  (event: 'error', message: string): void
}>()

const loading = ref(false)
const importing = ref(false)
const sessions = ref<ImportPreviewSession[]>([])
const failed = ref(0)
const selectedIds = ref<Set<string>>(new Set())
const search = ref('')
const timeFilter = ref<'7d' | '30d' | 'all'>('30d')
const workingDirFilter = ref('')
const showExisting = ref(false)

const directoryOptions = computed(() => {
  const values = new Set<string>()
  for (const session of sessions.value) {
    if (session.working_dir) {
      values.add(session.working_dir)
    }
  }
  return Array.from(values).sort((a, b) => a.localeCompare(b))
})

const visibleSessions = computed(() => {
  const query = search.value.trim().toLowerCase()
  const cutoff = timeCutoff(timeFilter.value)
  return sessions.value.filter((session) => {
    if (!showExisting.value && session.status === 'existing') {
      return false
    }
    if (workingDirFilter.value && session.working_dir !== workingDirFilter.value) {
      return false
    }
    if (cutoff > 0 && timestampValue(session.last_active_at) < cutoff) {
      return false
    }
    if (!query) {
      return true
    }
    return [session.name, session.working_dir, session.codex_session_id]
      .filter(Boolean)
      .join(' ')
      .toLowerCase()
      .includes(query)
  })
})

const selectedCount = computed(() => selectedIds.value.size)

watch(
  () => props.open,
  async (open) => {
    if (!open) {
      return
    }
    resetFilters()
    await loadPreview()
  },
)

function resetFilters() {
  search.value = ''
  timeFilter.value = '30d'
  workingDirFilter.value = ''
  showExisting.value = false
  selectedIds.value = new Set()
}

async function loadPreview() {
  loading.value = true
  try {
    const result = await PreviewCodexSessions()
    sessions.value = result.sessions.map((session) =>
      models.main.ImportPreviewSession.createFrom(session),
    )
    failed.value = result.failed || 0
  } catch (err) {
    emit('error', String(err))
  } finally {
    loading.value = false
  }
}

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

function timeCutoff(filter: '7d' | '30d' | 'all'): number {
  if (filter === 'all') {
    return 0
  }
  const days = filter === '7d' ? 7 : 30
  return Date.now() - days * 24 * 60 * 60 * 1000
}

function formatDate(value: unknown): string {
  const timestamp = timestampValue(value)
  if (!timestamp) {
    return ''
  }
  return new Date(timestamp).toLocaleString()
}

function isSelected(id: string): boolean {
  return selectedIds.value.has(id)
}

function toggleSelection(session: ImportPreviewSession) {
  if (session.status !== 'new') {
    return
  }
  const next = new Set(selectedIds.value)
  if (next.has(session.codex_session_id)) {
    next.delete(session.codex_session_id)
  } else {
    next.add(session.codex_session_id)
  }
  selectedIds.value = next
}

function selectVisible() {
  const next = new Set(selectedIds.value)
  for (const session of visibleSessions.value) {
    if (session.status === 'new') {
      next.add(session.codex_session_id)
    }
  }
  selectedIds.value = next
}

function clearSelection() {
  selectedIds.value = new Set()
}

async function importSelected() {
  if (selectedIds.value.size === 0) {
    return
  }
  importing.value = true
  try {
    const result = await ImportSelectedCodexSessions(Array.from(selectedIds.value))
    emit('imported', result)
  } catch (err) {
    emit('error', String(err))
  } finally {
    importing.value = false
  }
}
</script>

<template>
  <div v-if="open" class="dialog-backdrop" @click.self="$emit('close')">
    <section class="session-dialog import-dialog">
      <header class="dialog-header">
        <div class="dialog-title">Import Codex sessions</div>
        <button class="dialog-close" type="button" title="Close" @click="$emit('close')">x</button>
      </header>

      <div class="import-filters">
        <input
          v-model="search"
          class="dialog-input"
          type="search"
          placeholder="Search name, directory, or Codex id"
        />
        <select v-model="workingDirFilter" class="dialog-select">
          <option value="">All directories</option>
          <option v-for="directory in directoryOptions" :key="directory" :value="directory">
            {{ directory }}
          </option>
        </select>
        <select v-model="timeFilter" class="dialog-select">
          <option value="7d">Recent 7 days</option>
          <option value="30d">Recent 30 days</option>
          <option value="all">All</option>
        </select>
        <label class="dialog-checkbox">
          <input v-model="showExisting" type="checkbox" />
          <span>Show existing</span>
        </label>
      </div>

      <div class="import-summary">
        <span>{{ visibleSessions.length }} visible</span>
        <span>{{ selectedCount }} selected</span>
        <span v-if="failed > 0">Failed to parse: {{ failed }}</span>
      </div>

      <div class="import-table">
        <div class="import-row import-row-header">
          <span></span>
          <span>Name</span>
          <span>Directory</span>
          <span>Last active</span>
          <span>Status</span>
        </div>
        <button
          v-for="session in visibleSessions"
          :key="session.codex_session_id + session.codex_session_path"
          class="import-row"
          type="button"
          :class="{ existing: session.status === 'existing' }"
          @click="toggleSelection(session)"
        >
          <input
            type="checkbox"
            :checked="isSelected(session.codex_session_id)"
            :disabled="session.status !== 'new'"
            @click.stop="toggleSelection(session)"
          />
          <span class="import-name" :title="session.name">{{ session.name }}</span>
          <span class="import-path" :title="session.working_dir">{{ session.working_dir }}</span>
          <span>{{ formatDate(session.last_active_at) }}</span>
          <span class="import-status" :class="session.status">{{ session.status }}</span>
        </button>
        <div v-if="!loading && visibleSessions.length === 0" class="import-empty">No sessions</div>
        <div v-if="loading" class="import-empty">Loading sessions</div>
      </div>

      <footer class="dialog-actions import-actions">
        <button class="secondary-button" type="button" @click="selectVisible">Select visible</button>
        <button class="secondary-button" type="button" @click="clearSelection">Clear selection</button>
        <button
          class="primary-button"
          type="button"
          :disabled="selectedCount === 0 || importing"
          @click="importSelected"
        >
          {{ importing ? 'Importing' : 'Import selected' }}
        </button>
      </footer>
    </section>
  </div>
</template>
