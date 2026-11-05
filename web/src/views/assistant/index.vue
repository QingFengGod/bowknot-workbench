<template>
  <BkGrid tmpl-rows="1fr" tmpl-cols="1fr max-content" row-gap="0" col-gap="0">
    <BkCell class="box-container overflow-hidden">
      <BkFlex class="h-58px bk-bg-fill-secondary bk-p-sm b-b-1" justify="space-between" align="center">
        <BkFlex align="center">
          <AAvatar class="bk-bg-primary">助手</AAvatar>
          <div class="font-bold">主会话</div>
        </BkFlex>
        <BkFlex justify="end">
          <ATooltip :title="state.sessionsCollapsed ? '展开会话列表' : '收起会话列表'">
            <BkIcon
              name="line-md-menu-fold-left"
              :rotate="state.sessionsCollapsed ? 0 : 180"
              @click="state.sessionsCollapsed = !state.sessionsCollapsed"
            />
          </ATooltip>
        </BkFlex>
      </BkFlex>
    </BkCell>
    <BkCell class="transition-width" :class="state.sessionsCollapsed ? 'w-0' : 'w-300px bk-ml-sm'">
      <div v-if="!state.sessionsCollapsed" class="box-container h-full overflow-hidden">
        <div class="bk-bg-fill-secondary h-58px bk-p-sm b-b-1 font-bold flex items-center"> 会话列表 </div>
        <BkFlex class="scroll bk-p-xs" row-gap="6px" vertical style="height: calc(100% - 58px)">
          <BkFlex
            v-for="item in state.sessions"
            :key="item.id"
            class="bk-p-xs rd-lg cursor-pointer bk-bg-primary-bg-hover text-primary"
            justify="space-between"
            align="center"
          >
            <span>{{ item.title }}</span>
            <BkIcon class="cursor-pointer" name="prime-ellipsis-v" size="16px" :rotate="90" />
          </BkFlex>
        </BkFlex>
      </div>
    </BkCell>
  </BkGrid>
</template>

<script setup lang="ts">
import { reactive } from 'vue'

defineOptions({ name: 'AssistantPage' })

type PageState = {
  sessionsCollapsed: boolean
  sessions: Array<{ id: string; title: string }>
}
const state = reactive<PageState>({
  sessionsCollapsed: false,
  sessions: [
    { id: '1', title: '主会话' },
    { id: '2', title: '会话1' },
    { id: '3', title: '会话3' },
    { id: '4', title: '会话4' },
    { id: '5', title: '会话5' },
    { id: '6', title: '会话6' },
    { id: '7', title: '会话7' },
    { id: '8', title: '会话8' },
    { id: '9', title: '会话9' },
    { id: '10', title: '会话10' }
  ]
})
</script>

<style scoped></style>
