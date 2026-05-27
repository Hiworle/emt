<script lang="ts" setup>
import * as models from '../../wailsjs/go/models'

type Session = models.main.Session
type SessionGroup = {
  workingDir: string
  sessions: Session[]
}

defineProps<{
  groups: SessionGroup[]
  selectedId: string
  search: string
  importing: boolean
}>()

const emit = defineEmits<{
  (event: 'new-session'): void
  (event: 'import-sessions'): void
  (event: 'select-session', id: string): void
  (event: 'close-session', id: string): void
  (event: 'update:search', value: string): void
}>()

function sourceLabel(session: Session) {
  return session.source === 'imported' ? 'Imported' : 'EMT'
}

function handleSearchInput(event: Event) {
  emit('update:search', (event.target as HTMLInputElement).value)
}
</script>

<template>
  <aside class="sidebar">
    <header class="sidebar-header">
      <div class="brand">EMT</div>
      <button class="primary-button" title="New session" @click="$emit('new-session')">+ New</button>
    </header>

    <div class="sidebar-tools">
      <input
        class="search-input"
        type="search"
        placeholder="Search"
        :value="search"
        @input="handleSearchInput"
      />
      <button class="secondary-button" :disabled="importing" @click="$emit('import-sessions')">
        {{ importing ? 'Importing' : 'Import' }}
      </button>
    </div>

    <nav class="session-list">
      <section v-for="group in groups" :key="group.workingDir" class="session-group">
        <div class="group-heading" :title="group.workingDir">{{ group.workingDir }}</div>
        <button
          v-for="session in group.sessions"
          :key="session.id"
          class="session-row"
          :class="{ selected: session.id === selectedId }"
          @click="$emit('select-session', session.id)"
        >
          <span class="session-main">
            <span class="session-name">{{ session.name }}</span>
            <span class="session-meta">
              <span class="session-status" :class="session.status">{{ session.status }}</span>
              <span class="session-source" :class="session.source">{{ sourceLabel(session) }}</span>
            </span>
          </span>
          <span class="session-actions">
            <span
              class="close-button"
              title="Close session"
              @click.stop="$emit('close-session', session.id)"
            >
              x
            </span>
          </span>
        </button>
      </section>
      <div v-if="groups.length === 0" class="sidebar-empty">No sessions</div>
    </nav>
  </aside>
</template>
