<template>
  <BkGrid tmpl-rows="1fr" tmpl-cols="1fr max-content" row-gap="0" col-gap="0">
    <BkCell class="min-h-0">
      <section class="box-container h-full flex flex-col overflow-hidden">
        <BkFlex class="h-58px bk-bg-fill-secondary bk-p-sm b-b-1 shrink-0" justify="space-between" align="center">
          <BkFlex align="center">
            <AAvatar class="bk-bg-primary">助手</AAvatar>
            <div class="flex items-center gap-2">
              <div class="font-bold">{{ activeTitle }}</div>
              <span v-if="isStreaming" class="status-chip">正在回复…</span>
            </div>
          </BkFlex>
          <BkFlex justify="end">
            <ATooltip :title="state.sessionsCollapsed ? '展开会话列表' : '收起会话列表'">
              <button
                type="button"
                class="conv-toggle"
                :aria-label="state.sessionsCollapsed ? '展开会话列表' : '收起会话列表'"
                @click="state.sessionsCollapsed = !state.sessionsCollapsed"
              >
                <BkIcon name="akar-icons-panel-left" size="18px" :rotate="state.sessionsCollapsed ? 0 : 180" />
              </button>
            </ATooltip>
          </BkFlex>
        </BkFlex>

        <div class="flex-1 min-h-0">
          <MessageList
            :messages="activeMessages"
            @stop="onStop"
            @regenerate="onRegenerate"
            @retry="onRegenerate"
            @delete="onDeleteMessage"
          />
        </div>
      </section>
    </BkCell>

    <BkCell class="transition-width overflow-hidden" :class="state.sessionsCollapsed ? 'w-0' : 'w-300px bk-ml-sm'">
      <Conversations
        v-if="!state.sessionsCollapsed"
        :items="state.sessions"
        :active-id="state.activeId"
        @select="onSelect"
        @create="onCreate"
        @rename="onRename"
        @delete="onDelete"
        @pin="onPin"
      />
    </BkCell>
  </BkGrid>
</template>

<script setup lang="ts">
import MessageList from '@/components/shared/Message/MessageList.vue'
import { useMessageStream } from '@/composables/useMessageStream'
import type { ChatMessage } from '@/types/chat'
import { computed, onBeforeUnmount, onMounted, reactive } from 'vue'
import Conversations from './components/Conversations.vue'
import { mockConversations, mockMessages, mockReplies } from './mock'
import type { ConversationItem } from './type'

defineOptions({ name: 'AssistantPage' })

type PageState = {
  sessionsCollapsed: boolean
  activeId: string
  sessions: ConversationItem[]
}

const state = reactive<PageState>({
  sessionsCollapsed: false,
  activeId: 'daily',
  sessions: mockConversations.map((item) => ({ ...item }))
})

const messagesByConversation = reactive<Record<string, ChatMessage[]>>(structuredClone(mockMessages))

const stream = useMessageStream()

/** 每个会话只自动播放一次，避免删除回复后切换会话又重启 mock 流 */
const startedConversations = new Set<string>()

const activeTitle = computed(() => {
  return state.sessions.find((item) => item.id === state.activeId)?.title ?? '主会话'
})

const activeMessages = computed(() => messagesByConversation[state.activeId] ?? [])

const isStreaming = computed(() => activeMessages.value.some((message) => message.status === 'streaming'))

/** 进入还没有助手回复的 mock 会话时自动播放流式回复 */
const startMock = (conversationId: string) => {
  const reply = mockReplies[conversationId]
  const messages = messagesByConversation[conversationId]
  if (!reply || !messages || startedConversations.has(conversationId)) return
  if (messages.some((message) => message.role === 'assistant')) return
  startedConversations.add(conversationId)
  stream.start(messages, conversationId, reply)
}

const onSelect = (item: ConversationItem) => {
  state.activeId = item.id
  startMock(item.id)
}

const onCreate = () => {
  const id = Date.now().toString(36)
  state.activeId = id
  state.sessions.unshift({ id, title: '新会话', updatedAt: Date.now() })
  messagesByConversation[id] = []
}

const onRename = (item: ConversationItem, title: string) => {
  const target = state.sessions.find((session) => session.id === item.id)
  if (target) target.title = title
}

const onPin = (item: ConversationItem) => {
  const target = state.sessions.find((session) => session.id === item.id)
  if (target) target.pinned = !target.pinned
}

const onDelete = (item: ConversationItem) => {
  if (state.sessions.length <= 1) return
  const index = state.sessions.findIndex((session) => session.id === item.id)
  if (index < 0) return
  state.sessions.splice(index, 1)
  delete messagesByConversation[item.id]
  if (state.activeId === item.id) {
    state.activeId = state.sessions[Math.min(index, state.sessions.length - 1)].id
  }
}

const onStop = (message: ChatMessage) => {
  stream.stop(message.id)
}

const onRegenerate = (message: ChatMessage) => {
  const reply = mockReplies[message.conversationId]
  if (reply) stream.regenerate(message, reply)
}

const onDeleteMessage = (message: ChatMessage) => {
  if (message.status === 'streaming') stream.stop(message.id)
  const messages = messagesByConversation[message.conversationId]
  if (!messages) return
  const index = messages.findIndex((item) => item.id === message.id)
  if (index >= 0) messages.splice(index, 1)
}

onMounted(() => startMock(state.activeId))

onBeforeUnmount(() => stream.dispose())
</script>

<style scoped>
.conv-toggle {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: 0;
  border-radius: 8px;
  color: var(--bk-color-text-secondary);
  background: transparent;
  cursor: pointer;
  transition:
    background-color 0.2s ease,
    color 0.2s ease;
}

.conv-toggle:hover {
  color: var(--bk-color-text);
  background: var(--bk-color-fill);
}

.status-chip {
  @apply bk-color-primary bk-bg-primary-bg bk-px-xs bk-py-xxs;
  border-radius: 999px;
  font-size: 12px;
  line-height: 18px;
}
</style>
