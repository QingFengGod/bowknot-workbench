<template>
  <section class="bk-bg-container relative h-full overflow-hidden rd-lg b-all-1" aria-label="会话列表">
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
        <div class="w-80px h-80px flex-center b-all-1-primary-border rd-full">
          <BkIcon name="prime-message" size="46px" color="var(--bk-color-primary)" />
        </div>
        <p class="bk-mt-sm bk-text-h3 font-bold">{{ emptyText }}</p>
        <p class="bk-mt-sm text-12px text-xs bk-color-text-tertiary">{{ emptyHint }}</p>
      </div>
      <div v-else>
        <div v-for="group in groups" :key="group.key" class="bk-mb-sm">
          <div class="bk-color-text-quaternary">
            {{ group.label }}
          </div>
          <BkFlex v-for="entry in group.items" :key="entry.item.id" class="conv-item1">
            <div class="conv-item1-avatar"></div>
            <AAvatar class="bk-bg-primary">{{ initial(entry.item.title) }}</AAvatar>
            <div> </div>
          </BkFlex>
        </div>
      </div>
    </div>

    <div class="scroll flex-1 bk-p-sm" style="height: calc(100% - 58px)">
      <div
        v-if="groups.length == 0"
        class="flex h-full min-h-[260px] flex-col items-center justify-center px-[18px] pt-6 pb-12 text-center"
      >
        <div class="conv-empty__orb">
          <BkIcon name="prime-message" size="24px" />
        </div>
        <p class="text-[13px] font-semibold bk-color-main">{{ emptyText }}</p>
        <p class="mt-1.5 max-w-[190px] text-xs leading-relaxed bk-color-text-tertiary">{{ emptyHint }}</p>
      </div>

      <template v-else>
        <div v-for="group in groups" :key="group.key" class="mt-3 first:mt-0">
          <div
            class="conv-group__label flex items-center gap-2 px-2 pb-1.5 text-[11px] font-semibold tracking-[0.06em] bk-color-text-quaternary"
          >
            {{ group.label }}
          </div>
          <div
            v-for="entry in group.items"
            :key="entry.item.id"
            class="conv-item relative flex cursor-pointer items-start gap-2.5 rounded-xl p-2.5 outline-none"
            :class="{ 'is-active': entry.item.id === activeId, 'is-popover-open': popoverId === entry.item.id }"
            :style="{ '--i': entry.index }"
            :aria-current="entry.item.id === activeId ? 'true' : undefined"
            role="button"
            tabindex="0"
            @click="onItemClick(entry.item)"
            @keydown.enter.prevent="onItemClick(entry.item)"
            @keydown.space.prevent="onItemClick(entry.item)"
            @keydown.esc="cancelRename"
          >
            <span
              class="conv-item__avatar flex h-[34px] w-[34px] flex-none items-center justify-center rounded-[10px] text-[13px] font-semibold transition-colors b-all-1-border-secondary bk-bg-fill-secondary bk-color-text-secondary"
              aria-hidden="true"
            >
              {{ initial(entry.item.title) }}
            </span>

            <div class="min-w-0 flex-1 pt-px">
              <div class="flex items-center gap-2">
                <input
                  v-if="renamingId === entry.item.id"
                  :ref="setRenameInput"
                  v-model="renameDraft"
                  class="conv-item__input"
                  aria-label="会话标题"
                  @click.stop
                  @keydown.enter.prevent="commitRename"
                  @keydown.esc.prevent.stop="cancelRename"
                  @blur="commitRename"
                />
                <span
                  v-else
                  class="conv-item__title min-w-0 flex-1 truncate text-[13.5px] font-semibold tracking-[0.01em] transition-colors bk-color-main"
                >
                  {{ entry.item.title }}
                </span>
                <time
                  v-if="renamingId !== entry.item.id && relativeTime(entry.item.updatedAt)"
                  class="conv-item__time flex-none whitespace-nowrap text-[11px] tabular-nums transition-opacity bk-color-text-quaternary"
                >
                  {{ relativeTime(entry.item.updatedAt) }}
                </time>
              </div>
              <p class="mt-[3px] truncate text-xs leading-[1.45] bk-color-text-tertiary">
                {{ entry.item.summary || '暂无消息' }}
              </p>
            </div>

            <div class="conv-item__ops" @click.stop>
              <APopover
                :open="popoverId === entry.item.id"
                trigger="click"
                placement="bottomRight"
                :arrow="false"
                @open-change="(open: boolean) => setPopoverOpen(entry.item.id, open)"
              >
                <button type="button" class="conv-op" aria-label="会话操作" title="更多操作">
                  <BkIcon name="prime-ellipsis-v" size="16px" :rotate="90" />
                </button>
                <template #content>
                  <div class="flex flex-col gap-0.5">
                    <button
                      type="button"
                      class="popover-item w-full border-0 bg-transparent text-left text-[13px]"
                      @click="handleRename(entry.item)"
                    >
                      重命名
                    </button>
                    <button
                      type="button"
                      class="popover-item w-full border-0 bg-transparent text-left text-[13px]"
                      :class="{ 'pointer-events-none opacity-40': items.length <= 1 }"
                      @click="handleDelete(entry.item)"
                    >
                      删除
                    </button>
                  </div>
                </template>
              </APopover>
            </div>
          </div>
        </div>
      </template>
    </div>

    <div class="conv-fade" aria-hidden="true"></div>
  </section>
</template>

<script setup lang="ts">
import type { ComponentPublicInstance } from 'vue'
import type { ConversationItem, ConversationsProps } from './type'
import { computed, nextTick, ref } from 'vue'
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
}>()

type DisplayItem = { item: ConversationItem; index: number }
type DisplayGroup = { key: string; label: string; items: DisplayItem[] }

const MINUTE = 60 * 1000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

const renamingId = ref('')
const renameDraft = ref('')
const popoverId = ref('')
let renameInput: HTMLInputElement | null = null

const setRenameInput = (el: Element | ComponentPublicInstance | null) => {
  renameInput = el instanceof HTMLInputElement ? el : null
}

const toTime = (value: ConversationItem['updatedAt']) => {
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

const groupLabel = (time: number) => {
  if (!time) return '更早'
  const days = Math.round((startOfDay(Date.now()) - startOfDay(time)) / DAY)
  if (days <= 0) return '今天'
  if (days === 1) return '昨天'
  if (days < 7) return '近 7 天'
  return '更早'
}

const relativeTime = (value: ConversationItem['updatedAt']) => {
  const time = toTime(value)
  if (!time) return ''
  const diff = Date.now() - time
  if (diff < MINUTE) return '刚刚'
  if (diff < HOUR) return `${Math.floor(diff / MINUTE)} 分钟前`
  const days = Math.round((startOfDay(Date.now()) - startOfDay(time)) / DAY)
  if (days <= 0) return `${Math.floor(diff / HOUR)} 小时前`
  if (days === 1) return '昨天'
  const date = new Date(time)
  if (date.getFullYear() === new Date().getFullYear()) return `${date.getMonth() + 1}月${date.getDate()}日`
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`
}

const initial = (title: string) => title.trim().charAt(0).toUpperCase() || '聊'

const groups = computed<DisplayGroup[]>(() => {
  const sorted = props.items
    .map((item, order) => ({ item, order, time: toTime(item.updatedAt) }))
    .sort((a, b) => b.time - a.time || a.order - b.order)

  return sorted.reduce<DisplayGroup[]>((acc, entry, index) => {
    const label = groupLabel(entry.time)
    const display: DisplayItem = { item: entry.item, index }
    const last = acc[acc.length - 1]
    if (last && last.label === label) {
      last.items.push(display)
    } else {
      acc.push({ key: label, label, items: [display] })
    }
    return acc
  }, [])
})

const onItemClick = (item: ConversationItem) => {
  if (renamingId.value === item.id) return
  if (item.id !== props.activeId) emit('select', item)
}

const startRename = async (item: ConversationItem) => {
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
  if (!item || !title) {
    cancelRename()
    return
  }
  renamingId.value = ''
  renameDraft.value = ''
  if (title !== item.title) emit('rename', item, title)
}

const requestDelete = (item: ConversationItem) => {
  if (props.items.length <= 1) return
  cancelRename()
  Modal.confirm({
    title: '删除会话',
    content: `确定删除「${item.title}」吗？删除后消息记录将无法恢复。`,
    okText: '删除',
    cancelText: '取消',
    okButtonProps: { danger: true },
    width: 360,
    centered: true,
    onOk: () => emit('delete', item)
  })
}

const setPopoverOpen = (id: string, open: boolean) => {
  popoverId.value = open ? id : ''
}

const handleRename = (item: ConversationItem) => {
  popoverId.value = ''
  startRename(item)
}

const handleDelete = (item: ConversationItem) => {
  popoverId.value = ''
  requestDelete(item)
}
</script>

<style scoped>
.conv-item1 {
  @apply bk-p-sm;
  height: 68px;
  display: grid;
  grid-template-columns: 60px 1fr max-content;
  grid-template-rows: 1fr 1fr;
}
.conv-item1:hover {
  @apply bk-bg-fill-secondary;
}
.conv-item1 .avatar-container {
  grid-area: 1 / 1 / 3 / 2;
  display: flex;
  justify-content: center;
  align-items: center;
}
.conv-item1 .avatar-container .avatar {
  width: 60px;
  height: 60px;
}

.conv-group__label::after {
  content: '';
  @apply h-px flex-1 bk-bg-split;
}

.conv-item {
  @apply relative flex cursor-pointer items-start gap-2.5 rounded-xl p-2.5;
  border: 1px solid transparent;
  border-left-width: 3px;
  animation: conv-rise 0.42s cubic-bezier(0.22, 0.61, 0.36, 1) both;
  animation-delay: calc(var(--i, 0) * 32ms);
  transition:
    background-color 0.2s ease,
    border-color 0.2s ease,
    box-shadow 0.2s ease;
}

.conv-item + .conv-item {
  @apply bk-mt-xs;
}

.conv-item:hover {
  @apply bk-bg-fill-secondary;
}

.conv-item:focus-visible {
  @apply bk-border-primary-border;
}

.conv-item.is-active {
  @apply bk-border-primary-border;
  border-left-color: var(--bk-color-primary);
  background: linear-gradient(120deg, var(--bk-color-primary-bg) 0%, var(--bk-color-bg-container) 100%);
}

.conv-item.is-active .conv-item__avatar {
  @apply border-transparent bk-color-text-light-solid shadow-[0_4px_10px_-6px_var(--bk-color-primary)];
  background: linear-gradient(135deg, var(--bk-color-primary) 0%, var(--bk-color-primary-active) 100%);
}

.conv-item.is-active .conv-item__title {
  @apply bk-color-primary-text;
}

.conv-item__input {
  @apply h-[22px] min-w-0 flex-1 rounded-md px-1.5 text-[13px] font-semibold outline-none b-all-1-primary-border bk-bg-container bk-color-main shadow-[0_0_0_3px_var(--bk-color-primary-bg)];
}

.conv-item__ops {
  @apply absolute top-2 right-2 z-[2] flex items-center gap-0.5 rounded-lg p-0.5 pointer-events-none opacity-0 -translate-y-0.5 transition duration-200 b-all-1-border-secondary bk-bg-container bk-shadow;
}

.conv-item:hover .conv-item__ops,
.conv-item:focus-within .conv-item__ops,
.conv-item.is-popover-open .conv-item__ops {
  @apply pointer-events-auto translate-y-0 opacity-100;
}

.conv-item:hover .conv-item__time,
.conv-item:focus-within .conv-item__time {
  @apply opacity-0;
}

.conv-op {
  @apply flex h-6 w-6 cursor-pointer items-center justify-center rounded-md border-0 bg-transparent transition-colors bk-color-text-tertiary;
}

.conv-op:hover {
  @apply bk-bg-fill-secondary bk-color-main;
}

.conv-op.is-danger:hover {
  @apply bk-bg-error-bg bk-color-error;
}

.conv-op:disabled {
  @apply cursor-not-allowed opacity-35;
}

.conv-op:disabled:hover {
  @apply bg-transparent bk-color-text-tertiary;
}

.conv-skeleton__avatar {
  @apply h-[34px] w-[34px] rounded-[10px] bk-bg-fill-secondary;
  animation: conv-pulse 1.4s ease-in-out infinite;
}

.conv-skeleton__line {
  @apply h-2 w-[82%] rounded-[5px] opacity-70 bk-bg-fill-secondary;
  animation: conv-pulse 1.4s ease-in-out infinite;
}

.conv-skeleton__line.is-title {
  @apply h-[9px] w-[52%];
}

.conv-empty__orb {
  @apply relative mb-4 flex h-16 w-16 items-center justify-center rounded-[22px] b-all-1-primary-border shadow-[0_12px_28px_-20px_var(--bk-color-primary)] bk-color-primary;
  background: radial-gradient(120% 120% at 20% 10%, var(--bk-color-primary-bg) 0%, var(--bk-color-bg-container) 72%);
}

.conv-empty__orb::after {
  content: '';
  @apply absolute -inset-[9px] rounded-[30px] opacity-[0.55] b-all-1-primary-border-dashed;
  animation: conv-spin 18s linear infinite;
}

.conv-empty__btn {
  @apply mt-3.5 h-[30px] cursor-pointer rounded-lg px-3.5 text-xs font-semibold transition b-all-1-primary-border bk-bg-primary-bg bk-color-primary-text;
}

.conv-empty__btn:hover {
  @apply -translate-y-px b-all-1-primary-border-hover bk-bg-primary-bg-hover;
}

.conv-fade {
  @apply pointer-events-none absolute right-0 bottom-0 left-0 h-[22px];
  background: linear-gradient(to top, var(--bk-color-bg-container) 15%, transparent);
}

@keyframes conv-rise {
  from {
    opacity: 0;
    transform: translateY(8px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@keyframes conv-pulse {
  0%,
  100% {
    opacity: 0.45;
  }
  50% {
    opacity: 0.9;
  }
}

@keyframes conv-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (prefers-reduced-motion: reduce) {
  .conv-item,
  .conv-empty__orb::after,
  .conv-skeleton__avatar,
  .conv-skeleton__line {
    animation: none;
  }
}
</style>
