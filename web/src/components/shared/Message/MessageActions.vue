<template>
  <div class="message-actions">
    <button type="button" class="action-btn" @click="copy">{{ copied ? '已复制' : '复制' }}</button>
    <button
      v-if="message.role === 'assistant' && canRegenerate"
      type="button"
      class="action-btn"
      @click="emit('regenerate')"
    >
      重答
    </button>
    <button v-if="message.role === 'assistant'" type="button" class="action-btn" @click="speak">
      {{ speaking ? '停止播报' : '播报' }}
    </button>
    <button type="button" class="action-btn is-danger" @click="emit('delete')">删除</button>
  </div>
</template>

<script setup lang="ts">
import type { ChatMessage } from '@/types/chat'
import { computed, onBeforeUnmount, ref } from 'vue'

defineOptions({ name: 'MessageActions' })

const props = defineProps<{
  message: ChatMessage
  canRegenerate?: boolean
}>()

const emit = defineEmits<{
  regenerate: []
  delete: []
}>()

const plainText = computed(() =>
  props.message.parts
    .map((part) => {
      if (part.type === 'text') return part.text
      if (part.type === 'code') return part.code
      if (part.type === 'tool') return `[工具] ${part.name}`
      return ''
    })
    .join('\n\n')
    .trim()
)

const copied = ref(false)
let copyTimer: ReturnType<typeof setTimeout> | null = null

const copy = async () => {
  if (!plainText.value) return
  try {
    await navigator.clipboard.writeText(plainText.value)
  } catch {
    return
  }
  copied.value = true
  if (copyTimer !== null) clearTimeout(copyTimer)
  copyTimer = setTimeout(() => {
    copied.value = false
    copyTimer = null
  }, 1600)
}

const speaking = ref(false)

const speak = () => {
  if (typeof window.speechSynthesis === 'undefined') return
  if (speaking.value) {
    window.speechSynthesis.cancel()
    speaking.value = false
    return
  }
  if (!plainText.value) return
  const utterance = new SpeechSynthesisUtterance(plainText.value)
  utterance.lang = 'zh-CN'
  utterance.onend = () => {
    speaking.value = false
  }
  utterance.onerror = () => {
    speaking.value = false
  }
  window.speechSynthesis.cancel()
  window.speechSynthesis.speak(utterance)
  speaking.value = true
}

onBeforeUnmount(() => {
  if (copyTimer !== null) clearTimeout(copyTimer)
  if (speaking.value && typeof window.speechSynthesis !== 'undefined') window.speechSynthesis.cancel()
})
</script>

<style scoped lang="css">
.message-actions {
  display: flex;
  align-items: center;
  gap: 2px;
  margin-top: 2px;
  opacity: 0;
  transition: opacity 0.15s ease;
}

.message-actions:focus-within {
  opacity: 1;
}

.action-btn {
  @apply bk-color-text-tertiary bk-rd-xs;
  font-size: 12px;
  line-height: 20px;
  padding: 0 6px;
  border: 0;
  background: transparent;
  cursor: pointer;
}

.action-btn:hover {
  @apply bk-color-primary bk-bg-fill-secondary;
}

.action-btn.is-danger:hover {
  @apply bk-color-error bk-bg-error-bg;
}

@media (hover: none) {
  .message-actions {
    opacity: 1;
  }
}
</style>
