<script lang="ts" setup>
import { ref, watch } from 'vue'

const props = defineProps<{
  open: boolean
  name: string
}>()

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'rename', name: string): void
}>()

const draftName = ref('')

watch(
  () => props.open,
  (open) => {
    if (open) {
      draftName.value = props.name
    }
  },
)

watch(
  () => props.name,
  (name) => {
    if (props.open) {
      draftName.value = name
    }
  },
)

function rename() {
  emit('rename', draftName.value.trim())
}
</script>

<template>
  <div v-if="open" class="dialog-backdrop" @click.self="$emit('close')">
    <form class="session-dialog narrow" @submit.prevent="rename">
      <header class="dialog-header">
        <div class="dialog-title">Rename session</div>
        <button class="dialog-close" type="button" title="Close" @click="$emit('close')">x</button>
      </header>

      <label class="field-label">
        <span>Name</span>
        <input v-model="draftName" class="dialog-input" type="text" />
      </label>

      <footer class="dialog-actions">
        <button class="secondary-button" type="button" @click="$emit('close')">Cancel</button>
        <button class="primary-button" type="submit">Rename</button>
      </footer>
    </form>
  </div>
</template>
