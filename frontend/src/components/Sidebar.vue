<script lang="ts" setup>
import * as models from '../../wailsjs/go/models'

type Session = models.main.Session

defineProps<{
  sessions: Session[]
  selectedId: string
}>()

defineEmits<{
  (event: 'new-session'): void
  (event: 'select-session', id: string): void
  (event: 'close-session', id: string): void
}>()
</script>

<template>
  <aside class="sidebar">
    <header class="sidebar-header">
      <div class="brand">EMT</div>
      <button class="icon-button" title="New session" @click="$emit('new-session')">+</button>
    </header>

    <nav class="session-list">
      <button
        v-for="session in sessions"
        :key="session.id"
        class="session-row"
        :class="{ selected: session.id === selectedId }"
        @click="$emit('select-session', session.id)"
      >
        <span class="session-main">
          <span class="session-name">{{ session.name }}</span>
          <span class="session-status" :class="session.status">{{ session.status }}</span>
        </span>
        <span
          class="close-button"
          title="Close session"
          @click.stop="$emit('close-session', session.id)"
        >
          x
        </span>
      </button>
    </nav>
  </aside>
</template>
