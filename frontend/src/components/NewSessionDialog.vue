<script lang="ts" setup>
import { ref, watch } from 'vue'

const props = defineProps<{
  open: boolean
  defaultWorkingDir: string
}>()

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'create', payload: { name: string; workingDir: string }): void
  (event: 'browse', currentDir: string): void
}>()

const name = ref('')
const workingDir = ref('')

watch(
  () => props.open,
  (open) => {
    if (!open) {
      return
    }
    name.value = ''
    workingDir.value = props.defaultWorkingDir
  },
)

watch(
  () => props.defaultWorkingDir,
  (defaultWorkingDir) => {
    if (props.open) {
      workingDir.value = defaultWorkingDir
    }
  },
)

function create() {
  emit('create', {
    name: name.value.trim(),
    workingDir: workingDir.value.trim(),
  })
}
</script>

<template>
  <div v-if="open" class="dialog-backdrop" @click.self="$emit('close')">
    <form class="session-dialog" @submit.prevent="create">
      <header class="dialog-header">
        <div class="dialog-title">New session</div>
        <button class="dialog-close" type="button" title="Close" @click="$emit('close')">x</button>
      </header>

      <label class="field-label">
        <span>Name</span>
        <input v-model="name" class="dialog-input" type="text" placeholder="Session name" />
      </label>

      <label class="field-label">
        <span>Working directory</span>
        <div class="directory-field">
          <input v-model="workingDir" class="dialog-input" type="text" />
          <button class="secondary-button" type="button" @click="$emit('browse', workingDir)">
            Browse
          </button>
        </div>
      </label>

      <footer class="dialog-actions">
        <button class="secondary-button" type="button" @click="$emit('close')">Cancel</button>
        <button class="primary-button" type="submit">Create</button>
      </footer>
    </form>
  </div>
</template>
