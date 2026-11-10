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
        <div v-for="g in groups" :key="g.key" class="conv-group bk-mb-sm">
          <div class="conv-group-header">
            <span> {{ g.title }}</span>
            <div class="conv-group-tools">
              <BkIcon class="tool-item" name="prime-ellipsis-v" size="12px" container-size="20px" :rotate="90" />
              <BkIcon class="tool-item" name="prime-chevron-right" size="12px" container-size="20px" :rotate="90" />
            </div>
          </div>
          <div class="conv-group-body">
            <div
              v-for="item in g.items"
              :key="item.id"
              class="conv-item"
              :class="{ active: item.id == props.activeId }"
            >
              <div class="avatar-container">
                <div class="avatar">{{ item.title.charAt(0) }}</div>
              </div>
              <div class="conv-title">{{ item.title }}</div>
              <div class="conv-subtitle">{{ item.summary }}</div>
              <div class="conv-tools">
                <BkIcon class="tool-item" name="prime-ellipsis-v" container-size="20px" size="12px" :rotate="90" />
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import type { ConversationItem, ConversationsProps } from './type'
import { computed } from 'vue'

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

type GroupItem = {
  key: string
  title: string
  items: ConversationItem[]
}

const groups = computed(() => {
  const noGroup: ConversationItem[] = []
  const obj: Record<string, ConversationItem[]> = {}
  props.items.forEach((item) => {
    if (item.group) {
      if (obj[item.group]) obj[item.group].push(item)
      else obj[item.group] = [item]
    } else {
      noGroup.push(item)
    }
  })
  const groupList: GroupItem[] = Object.entries(obj).map(([title, v], idx) => {
    return {
      key: 'group_' + idx,
      title,
      items: v
    }
  })
  if (noGroup.length > 0) {
    groupList.push({
      key: '__no_group__',
      title: '未分类',
      items: noGroup
    })
  }

  return groupList
})
</script>

<style scoped lang="css">
.conversations {
  @apply bk-bg-container bk-rd b-all-1;
  position: relative;
  height: 100%;
  width: 100%;
  overflow: hidden;
}

.conv-group {
  @apply bk-mb-sm;
}
.conv-group-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.conv-group-tools {
  display: flex;
  align-items: center;
  gap: 0 6px;
}
.tool-item {
  @apply bk-rd-xs;
  opacity: 0;
}

.conv-group-body {
  @apply bk-pt-xs;
}
.conv-item {
  @apply bk-p-sm bk-rd bk-mb-xxs;
  height: 68px;
  display: grid;
  grid-template-columns: max-content 1fr max-content;
  grid-template-rows: 1fr 1fr;
  cursor: pointer;
}
.conv-item.active {
  @apply b-all-1-primary;
  border-left-width: 4px;
}
.conv-item:hover {
  @apply bk-bg-fill-secondary;
}
.conv-item .avatar-container {
  @apply flex-center bk-pr-sm;
  grid-area: 1/1/3/2;
}
.avatar-container .avatar {
  @apply bk-bg-primary flex-center;
  width: 38px;
  height: 38px;
  color: #fff;
  border-radius: 999px;
}
.conv-item .conv-title {
  @apply line-clamp-1;
  grid-area: 1/2/2/3;
  font-weight: bold;
}
.conv-item .conv-subtitle {
  @apply bk-color-text-description line-clamp-1;
  grid-area: 2/2/3/4;
  font-size: 12px;
}

.conv-item .conv-tools {
  grid-area: 1/3/2/4;
  min-width: 20px;
  height: 20px;
}

.conv-group-header:hover .tool-item {
  @apply bk-bg-layout;
  opacity: 1;
}

.conv-item:hover .tool-item {
  @apply bk-bg-container;
  opacity: 1;
}
</style>
