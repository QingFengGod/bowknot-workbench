<template>
  <div class="message-list">
    <div ref="scrollEl" class="message-scroll scroll" @scroll="syncPosition">
      <div v-if="messages.length === 0" class="message-empty">
        <div class="empty-icon">
          <BkIcon class="bk-color-primary" name="prime-message" size="42px" />
        </div>
        <p class="empty-title">开始和助理聊聊吧</p>
        <p class="empty-hint">回复会以流式方式逐步出现，可以随时停止或重答。</p>
      </div>

      <template v-else>
        <MessageItem
          v-for="message in messages"
          :key="message.id"
          v-memo="[message.parts, message.status, message.id === lastAssistantId]"
          :message="message"
          :can-regenerate="message.id === lastAssistantId && message.status !== 'streaming'"
          @stop="emit('stop', message)"
          @regenerate="emit('regenerate', message)"
          @retry="emit('retry', message)"
          @delete="emit('delete', message)"
        />
      </template>

      <div class="scroll-anchor" />
    </div>

    <Transition name="transition-fade">
      <button v-show="showBackToBottom" type="button" class="back-bottom" @click="scrollToBottom()"> 回到底部 </button>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import type { ChatMessage } from '@/types/chat'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useAutoScroll } from '@/composables/useAutoScroll'
import MessageItem from './MessageItem.vue'

defineOptions({ name: 'MessageList' })

const props = defineProps<{
  messages: ChatMessage[]
}>()

const emit = defineEmits<{
  stop: [message: ChatMessage]
  regenerate: [message: ChatMessage]
  retry: [message: ChatMessage]
  delete: [message: ChatMessage]
}>()

const scrollEl = ref<HTMLElement | null>(null)
const { showBackToBottom, syncPosition, scrollToBottom, keepBottom } = useAutoScroll(scrollEl)

const lastAssistantId = computed(() => {
  for (let index = props.messages.length - 1; index >= 0; index -= 1) {
    const message = props.messages[index]
    if (message.role === 'assistant') return message.id
  }
  return ''
})

watch(() => props.messages, keepBottom, { deep: true, flush: 'post' })

/** 切换会话时回到该会话的底部 */
watch(
  () => props.messages,
  () => {
    nextTick(() => scrollToBottom('auto'))
  }
)

onMounted(() => {
  nextTick(() => scrollToBottom('auto'))
})
</script>

<style scoped lang="css">
.message-list {
  @apply relative h-full;
  overflow: hidden;
}

.message-scroll {
  height: 100%;
  padding: 12px 20px 28px;
}

.message-empty {
  @apply h-full flex-center flex-col bk-color-text-tertiary;
  gap: 6px;
}

.empty-icon {
  @apply flex-center b-all-1-primary rd-full;
  width: 72px;
  height: 72px;
  border-style: dashed;
}

.empty-title {
  @apply bk-color-text-heading;
  margin-top: 8px;
  font-size: 15px;
  font-weight: 600;
}

.empty-hint {
  font-size: 12px;
}

.scroll-anchor {
  height: 1px;
}

.back-bottom {
  @apply bk-bg-container bk-color-text-secondary bk-shadow bk-px-sm bk-py-xs;
  position: absolute;
  right: 20px;
  bottom: 16px;
  border: 1px solid var(--bk-color-split);
  border-radius: 999px;
  font-size: 12px;
  cursor: pointer;
}

.back-bottom:hover {
  @apply bk-color-primary;
}
</style>
