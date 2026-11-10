<template>
  <section class="conversations" aria-label="会话列表">
    <BkFlex class="linear-primary-bg h-58px bk-p-sm b-b-1" justify="space-between" align="center">
      <div class="flex items-baseline gap-2">
        <h2 class="bk-text-h4 font-bold bk-color-text-heading">{{ title }}</h2>
        <div class="rd-full w-18px h-18px flex-center text-10px bk-color-text-tertiary bk-bg-fill-secondary">
          {{ items.length }}
        </div>
      </div>
      <span class="bk-color-primary cursor-pointer" @click="emit('create')">新建会话</span>
    </BkFlex>

    <div class="scroll flex-1 bk-p-sm radial-primary-bg" style="height: calc(100% - 58px)">
      <div v-if="groups.length == 0" class="w-full h-80% flex-center flex-col">
        <div class="w-80px h-80px flex-center b-all-1-primary rd-full">
          <BkIcon class="bk-color-primary" name="prime-message" size="46px" />
        </div>
        <p class="bk-mt-sm bk-text-h3 font-bold">{{ emptyText }}</p>
        <p class="bk-mt-sm text-12px text-xs bk-color-text-tertiary">{{ emptyHint }}</p>
      </div>
      <template v-else>
        <div v-for="g in groups" :key="g.key" class="conv-group">
          <div class="conv-group-header" @click="toggleGroup(g.key)">
            <span class="conv-group-title">{{ g.title }}</span>
            <ATooltip :title="isCollapsed(g.key) ? '展开分组' : '收起分组'">
              <BkIcon
                name="prime-chevron-right"
                size="16px"
                :rotate="isCollapsed(g.key) ? 0 : 90"
                :aria-label="isCollapsed(g.key) ? '展开分组' : '收起分组'"
              />
            </ATooltip>
          </div>
          <div class="conv-group-body" :class="{ 'is-collapsed': isCollapsed(g.key) }">
            <div class="conv-group-inner">
              <div
                v-for="item in g.items"
                :key="item.id"
                class="conv-item"
                :class="{ active: item.id == props.activeId, 'is-menu-open': menuId === item.id }"
              >
                <input
                  v-if="renamingId === item.id"
                  :ref="setRenameInput"
                  v-model="renameDraft"
                  class="conv-title-input"
                  aria-label="会话标题"
                  @click.stop
                  @keydown.enter.prevent="commitRename"
                  @keydown.esc.prevent.stop="cancelRename"
                  @blur="commitRename"
                />
                <div v-else class="conv-title">{{ item.title }}</div>
                <div class="conv-side" @click.stop>
                  <span class="conv-time">{{ formatTime(item.updatedAt) }}</span>
                  <APopover
                    :open="menuId === item.id"
                    trigger="click"
                    placement="bottomRight"
                    :arrow="false"
                    @open-change="(open: boolean) => setMenuOpen(item.id, open)"
                  >
                    <BkIcon class="conv-more" name="prime-ellipsis-v" container-size="20px" size="12px" :rotate="90" />
                    <template #content>
                      <div class="min-w-120px">
                        <div class="popover-item" @click="handleRename(item)"> 重命名 </div>
                        <div type="button" class="popover-item" @click="handlePin(item)">
                          {{ item.pinned ? '取消置顶' : '置顶' }}
                        </div>
                        <div class="popover-item is-danger" :disabled="items.length <= 1" @click="handleDelete(item)">
                          删除
                        </div>
                      </div>
                    </template>
                  </APopover>
                </div>
                <div class="conv-subtitle">{{ item.summary }}</div>
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import type { ComponentPublicInstance } from 'vue'
import type { ConversationItem, ConversationsProps } from '../type'
import { computed, nextTick, reactive, ref } from 'vue'
import { Modal } from 'antdv-next'

defineOptions({ name: 'Conversations' })

const props = withDefaults(defineProps<ConversationsProps>(), {
  items: () => [],
  activeId: '',
  loading: false,
  title: '会话列表',
  emptyText: '还没有会话',
  emptyHint: '开始一段新的对话，助理会记得你说过的每句话。'
})

const emit = defineEmits<{
  select: [item: ConversationItem]
  create: []
  rename: [item: ConversationItem, title: string]
  delete: [item: ConversationItem]
  pin: [item: ConversationItem]
}>()

type GroupItem = {
  key: string
  title: string
  items: ConversationItem[]
}

const DAY = 24 * 60 * 60 * 1000

/** 默认只展开置顶与今天，其余分组首次进入时收起；用户手动切换仅保留在内存中 */
const DEFAULT_OPEN_KEYS = ['pinned', 'today']

const collapsed = reactive<Record<string, boolean>>({})

const isCollapsed = (key: string) => collapsed[key] ?? !DEFAULT_OPEN_KEYS.includes(key)

const toggleGroup = (key: string) => {
  collapsed[key] = !isCollapsed(key)
}

/** 当前展开操作菜单的会话 id */
const menuId = ref('')
const renamingId = ref('')
const renameDraft = ref('')
let renameInput: HTMLInputElement | null = null

const setRenameInput = (el: Element | ComponentPublicInstance | null) => {
  renameInput = el instanceof HTMLInputElement ? el : null
}

const setMenuOpen = (id: string, open: boolean) => {
  menuId.value = open ? id : ''
}

const closeMenu = () => {
  menuId.value = ''
}

const startRename = async (item: ConversationItem) => {
  closeMenu()
  renamingId.value = item.id
  renameDraft.value = item.title
  await nextTick()
  renameInput?.focus()
  renameInput?.select()
}

const cancelRename = () => {
  renamingId.value = ''
  renameDraft.value = ''
}

const commitRename = () => {
  if (!renamingId.value) return
  const item = props.items.find((target) => target.id === renamingId.value)
  const title = renameDraft.value.trim()
  renamingId.value = ''
  renameDraft.value = ''
  if (!item || !title || title === item.title) return
  emit('rename', item, title)
}

const handleRename = (item: ConversationItem) => {
  void startRename(item)
}

const handlePin = (item: ConversationItem) => {
  closeMenu()
  emit('pin', item)
}

const handleDelete = (item: ConversationItem) => {
  closeMenu()
  if (props.items.length <= 1) return
  Modal.confirm({
    title: '删除会话',
    content: `确定删除「${item.title}」吗？删除后消息记录将无法恢复。`,
    okText: '删除',
    cancelText: '取消',
    okButtonProps: { danger: true },
    centered: true,
    onOk: () => emit('delete', item)
  })
}

const toTime = (value?: ConversationItem['updatedAt']) => {
  if (!value) return 0
  if (value instanceof Date) return value.getTime()
  if (typeof value === 'number') return value
  const time = new Date(value).getTime()
  return Number.isNaN(time) ? 0 : time
}

const startOfDay = (time: number) => {
  const date = new Date(time)
  return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime()
}

type TimeGroupKey = 'today' | 'yesterday' | 'week' | 'earlier'

const TIME_GROUP_TITLE: Record<TimeGroupKey, string> = {
  today: '今天',
  yesterday: '昨天',
  week: '近 7 天',
  earlier: '更早'
}

/** 未指定 group 的会话按更新时间归入时间分组 */
const timeGroupKey = (time: number): TimeGroupKey => {
  if (!time) return 'earlier'
  const days = Math.round((startOfDay(Date.now()) - startOfDay(time)) / DAY)
  if (days <= 0) return 'today'
  if (days === 1) return 'yesterday'
  if (days < 7) return 'week'
  return 'earlier'
}

const pad = (value: number) => String(value).padStart(2, '0')

/** 列表右侧的紧凑相对时间，控制在 44px 内，避免挤压标题 */
const formatTime = (value?: ConversationItem['updatedAt']) => {
  const time = toTime(value)
  if (!time) return ''
  const diff = Date.now() - time
  if (diff < 60 * 1000) return '刚刚'
  if (diff < 60 * 60 * 1000) return `${Math.floor(diff / (60 * 1000))} 分钟`
  const date = new Date(time)
  const days = Math.round((startOfDay(Date.now()) - startOfDay(time)) / DAY)
  if (days <= 0) return `${pad(date.getHours())}:${pad(date.getMinutes())}`
  if (days === 1) return '昨天'
  if (days < 7) return `${days} 天前`
  return `${date.getMonth() + 1}/${date.getDate()}`
}

const groups = computed<GroupItem[]>(() => {
  const sorted = [...props.items].sort((a, b) => toTime(b.updatedAt) - toTime(a.updatedAt))
  const pinned: ConversationItem[] = []
  const bucket = new Map<string, GroupItem>()
  sorted.forEach((item) => {
    if (item.pinned) {
      pinned.push(item)
      return
    }
    const timeKey = timeGroupKey(toTime(item.updatedAt))
    // 分组 key 用稳定标识，避免跨天后「今天」变「昨天」导致折叠状态错位
    const key = item.group || timeKey
    const title = item.group || TIME_GROUP_TITLE[timeKey]
    const group = bucket.get(key)
    if (group) group.items.push(item)
    else bucket.set(key, { key, title, items: [item] })
  })
  const groupList = [...bucket.values()]
  if (pinned.length > 0) {
    groupList.unshift({ key: 'pinned', title: '置顶', items: pinned })
  }
  return groupList
})
</script>

<style scoped lang="css">
.conversations {
  @apply w-full h-full bk-bg-container bk-rd b-all-1;
  position: relative;
  overflow: hidden;
}

.conv-group {
  @apply bk-mb-xxs;
}
.conv-group-header {
  @apply bk-p-xs cursor-pointer;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.conv-group-header:hover {
  @apply bk-color-primary;
}

.conv-group-body {
  display: grid;
  grid-template-rows: 1fr;
  transition: grid-template-rows 0.26s ease;
}
.conv-group-body.is-collapsed {
  grid-template-rows: 0fr;
}
.conv-group-inner {
  min-height: 0;
  overflow: hidden;
}
.conv-item {
  @apply bk-px-sm bk-py-xxs bk-rd bk-mt-xxs;
  display: grid;
  grid-template-columns: minmax(0, 1fr) max-content;
  grid-template-rows: 24px 20px;
  column-gap: 6px;
  cursor: pointer;
}
.conv-item.active {
  @apply b-all-1-primary bk-bg-primary-bg;
  border-left-width: 3px;
}
.conv-item.active:hover {
  @apply bk-bg-container;
}
.conv-item:hover {
  @apply bk-bg-fill-secondary;
}
.conv-item .conv-title {
  @apply line-clamp-1;
  grid-area: 1/1/2/2;
  font-weight: bold;
}
.conv-title-input {
  @apply w-full bg-transparent font-bold outline-none;
  grid-area: 1/1/2/2;
  height: 24px;
  font-size: 14px;
  border: 0;
  border-bottom: 1px solid var(--bk-color-primary);
}
.conv-item .conv-subtitle {
  @apply bk-color-text-description line-clamp-1;
  grid-area: 2/1/3/3;
  font-size: 12px;
}

.conv-side {
  @apply flex items-center justify-end;
  position: relative;
  grid-area: 1/2/2/3;
  min-width: 44px;
}
.conv-time {
  @apply bk-color-text-tertiary;
  font-size: 11px;
  white-space: nowrap;
  transition: opacity 0.15s ease;
}
.conv-more {
  position: absolute;
  top: 50%;
  right: 0;
  opacity: 0;
  transform: translateY(-50%);
  transition: opacity 0.15s ease;
}

.conv-item:hover .conv-time,
.conv-item.is-menu-open .conv-time {
  opacity: 0;
}

.conv-item:hover .conv-more,
.conv-item.is-menu-open .conv-more {
  @apply bk-bg-container bk-rd-xs;
  opacity: 1;
}

.popover-item.is-danger {
  @apply bk-color-error;
}
.popover-item.is-danger:hover {
  @apply bk-bg-error-bg bk-color-error;
}
.popover-item:disabled {
  @apply cursor-not-allowed opacity-40 bg-transparent bk-color-text-tertiary;
}
</style>
