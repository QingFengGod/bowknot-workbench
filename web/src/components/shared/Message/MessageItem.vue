<template>
  <article class="message-item" :class="`is-${message.role}`">
    <div v-if="message.role === 'assistant'" class="message-avatar">
      <AAvatar class="bk-bg-primary">助手</AAvatar>
    </div>
    <div class="message-main">
      <div class="message-bubble" :class="`is-${message.role}`">
        <MessageParts :parts="message.parts" :streaming="message.status === 'streaming'" />

        <div v-if="message.status === 'streaming'" class="message-streaming">
          <span class="streaming-dot" />
          <span>正在生成</span>
          <button type="button" class="text-btn" @click="emit('stop')">停止生成</button>
        </div>

        <div v-else-if="message.status === 'failed'" class="message-error">
          <span>{{ message.error || '生成失败，请稍后重试' }}</span>
          <button type="button" class="text-btn" @click="emit('retry')">重试</button>
        </div>

        <div v-else-if="message.status === 'stopped'" class="message-stopped">已停止生成</div>
      </div>

      <div class="message-meta">
        <span>{{ timeText }}</span>
        <span v-if="emotionText">{{ emotionText }}</span>
        <span v-if="message.memoryWritten" class="message-memory">已记住</span>
      </div>

      <MessageActions
        :message="message"
        :can-regenerate="canRegenerate"
        @regenerate="emit('regenerate')"
        @delete="confirmDelete"
      />
    </div>
  </article>
</template>

<script setup lang="ts">
import type { ChatMessage, EmotionPart } from '@/types/chat'
import { computed } from 'vue'
import { Modal } from 'antdv-next'
import MessageActions from './MessageActions.vue'
import MessageParts from './MessageParts.vue'

defineOptions({ name: 'MessageItem' })

const props = defineProps<{
  message: ChatMessage
  canRegenerate?: boolean
}>()

const emit = defineEmits<{
  stop: []
  regenerate: []
  retry: []
  delete: []
}>()

const EMOTION_LABELS: Record<string, string> = {
  happy: '开心',
  shy: '害羞',
  angry: '生气',
  sad: '难过',
  surprised: '惊讶',
  neutral: '平静'
}

const emotionText = computed(() => {
  const part = props.message.parts.find((item): item is EmotionPart => item.type === 'emotion')
  if (!part) return ''
  return `情绪：${EMOTION_LABELS[part.emotion] ?? part.emotion}`
})

const timeText = computed(() => {
  const date = new Date(props.message.createdAt)
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
})

const confirmDelete = () => {
  Modal.confirm({
    title: '删除消息',
    content: '删除后无法恢复，确定删除这条消息吗？',
    okText: '删除',
    cancelText: '取消',
    okButtonProps: { danger: true },
    centered: true,
    onOk: () => emit('delete')
  })
}
</script>

<style scoped lang="css">
.message-item {
  display: flex;
  gap: 10px;
  padding: 8px 0 4px;
}

.message-item.is-user {
  justify-content: flex-end;
}

.message-avatar {
  padding-top: 2px;
}

.message-main {
  min-width: 0;
  max-width: min(760px, 88%);
}

.message-item.is-user .message-main {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
}

.message-bubble {
  @apply bk-rd-lg bk-bg-fill-secondary;
  padding: 10px 14px;
}

.message-bubble.is-user {
  @apply bk-bg-primary-bg;
}

.message-streaming {
  @apply bk-color-text-secondary;
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
  font-size: 12px;
}

.streaming-dot {
  @apply bk-bg-primary;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  animation: bk-streaming-pulse 1.2s ease-in-out infinite;
}

.message-error {
  @apply bk-color-error;
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
  font-size: 12px;
}

.message-stopped {
  @apply bk-color-text-tertiary;
  margin-top: 8px;
  font-size: 12px;
}

.message-meta {
  @apply bk-color-text-tertiary;
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 4px;
  font-size: 12px;
}

.message-memory {
  @apply bk-color-primary;
}

.text-btn {
  @apply bk-color-primary;
  font-size: 12px;
  padding: 0 4px;
  border: 0;
  background: transparent;
  cursor: pointer;
}

.message-item:hover .message-actions {
  opacity: 1;
}

@keyframes bk-streaming-pulse {
  0%,
  100% {
    opacity: 0.35;
    transform: scale(0.9);
  }
  50% {
    opacity: 1;
    transform: scale(1.1);
  }
}

@media (prefers-reduced-motion: reduce) {
  .streaming-dot {
    animation: none;
    opacity: 1;
  }
}

@media (hover: none) {
  .message-actions {
    opacity: 1;
  }
}
</style>
