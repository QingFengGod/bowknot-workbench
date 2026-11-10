<template>
  <BkGrid tmpl-rows="1fr" tmpl-cols="1fr max-content" row-gap="0" col-gap="0">
    <BkCell class="box-container overflow-hidden">
      <BkFlex class="h-58px bk-bg-fill-secondary bk-p-sm b-b-1" justify="space-between" align="center">
        <BkFlex align="center">
          <AAvatar class="bk-bg-primary">助手</AAvatar>
          <div class="font-bold">{{ activeTitle }}</div>
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
      />
    </BkCell>
  </BkGrid>
</template>

<script setup lang="ts">
import { computed, reactive } from 'vue'
import Conversations from '@/components/global/Conversations/Conversations.vue'
import type { ConversationItem } from '@/components'

defineOptions({ name: 'AssistantPage' })

type PageState = {
  sessionsCollapsed: boolean
  activeId: string
  sessions: ConversationItem[]
}

const now = Date.now()
const MINUTE = 60 * 1000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

const state = reactive<PageState>({
  sessionsCollapsed: false,
  activeId: 'daily',
  sessions: [
    {
      id: 'daily',
      group: '日常',
      title: '日常',
      summary: '今天的安排已经整理好了，晚上记得早点休息。',
      updatedAt: now - 2 * MINUTE
    },
    {
      id: 'work',
      group: '工作',
      title: '工作讨论',
      summary: '周报要点分三块：本周进展、风险与下周计划。',
      updatedAt: now - 36 * MINUTE
    },
    {
      id: 'travel',
      group: '日常',
      title: '旅行计划',
      summary: '杭州三天两晚，按西湖、灵隐、龙井的顺序排好了。',
      updatedAt: now - 5 * HOUR
    },
    {
      id: 'reading',
      group: '工作',
      title: '读书笔记',
      summary: '把《禅与摩托车维修艺术》的摘录按章节归档了。',
      updatedAt: now - (DAY + 3 * HOUR)
    },
    {
      id: 'fitness',
      group: '身体健康',
      title: '健身打卡',
      summary: '本周完成 3 次力量训练，下一次建议练背。',
      updatedAt: now - 3 * DAY
    },
    {
      id: 'movie1',
      title: '片单',
      summary: '把想看的十部电影按情绪分成了三类。',
      updatedAt: now - 12 * DAY
    },
    {
      id: 'movie2',
      title: '片单',
      summary: '把想看的十部电影按情绪分成了三类。',
      updatedAt: now - 12 * DAY
    },
    {
      id: 'movie3',
      title: '片单',
      summary: '把想看的十部电影按情绪分成了三类。',
      updatedAt: now - 12 * DAY
    },
    {
      id: 'movie4',
      title: '片单',
      summary: '把想看的十部电影按情绪分成了三类。',
      updatedAt: now - 12 * DAY
    },
    {
      id: 'movie5',
      title: '片单',
      summary: '把想看的十部电影按情绪分成了三类。',
      updatedAt: now - 12 * DAY
    },
    {
      id: 'movie6',
      title: '片单',
      summary: '把想看的十部电影按情绪分成了三类。',
      updatedAt: now - 12 * DAY
    },
    {
      id: 'movie7',
      title: '片单',
      summary: '把想看的十部电影按情绪分成了三类。',
      updatedAt: now - 12 * DAY
    },
    {
      id: 'movie8',
      title: '片单',
      summary: '把想看的十部电影按情绪分成了三类。',
      updatedAt: now - 12 * DAY
    },
    {
      id: 'movie9',
      title: '片单',
      summary: '把想看的十部电影按情绪分成了三类。',
      updatedAt: now - 12 * DAY
    },
    {
      id: 'movie10',
      title: '片单',
      summary: '把想看的十部电影按情绪分成了三类。',
      updatedAt: now - 12 * DAY
    }
  ]
})

const activeTitle = computed(() => {
  return state.sessions.find((item) => item.id === state.activeId)?.title ?? '主会话'
})

const onSelect = (item: ConversationItem) => {
  state.activeId = item.id
}

const onCreate = () => {
  const date = new Date()
  const id = Date.now().toString(36)
  state.activeId = id
}

const onRename = (item: ConversationItem, title: string) => {
  const target = state.sessions.find((session) => session.id === item.id)
  if (target) target.title = title
}

const onDelete = (item: ConversationItem) => {
  if (state.sessions.length <= 1) return
  const index = state.sessions.findIndex((session) => session.id === item.id)
  if (index < 0) return
  state.sessions.splice(index, 1)
  if (state.activeId === item.id) {
    state.activeId = state.sessions[Math.min(index, state.sessions.length - 1)].id
  }
}
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
</style>
