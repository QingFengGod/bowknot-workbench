<template>
  <BkGrid tmpl-cols="max-content max-content 1fr" tmpl-rows="52px 1fr" row-gap="0" col-gap="0">
    <BkCell area="1/1/3/2" class="w-78px bk-bg-container b-r-1">
      <div class="text-primary h-58px w-full flex-center">
        <BkIcon name="game-icons-bow-tie-ribbon" size="32px" />
      </div>
      <div class="w-full" style="height: calc(100% - 58px)">
        <LayoutNavs :active-key="state.activeNavKey" :items="appStore.menus" @item-click="onNavItemClick" />
      </div>
    </BkCell>
    <BkCell area="1/2/3/3" class="w-220px bk-bg-container b-r-1" v-if="menus.length > 0">
      <AMenu :items="menus">
        <template #iconRender="item"> <BkIcon :name="item.iconName" /> </template>
      </AMenu>
    </BkCell>

    <BkCell area="1/3/2/4" class="bk-bg-container b-b-1 bk-px-sm" flex align="center" justify="space-between">
      <BkFlex>
        <BkIcon name="line-md-menu-fold-left"></BkIcon>
      </BkFlex>
      <BkFlex justify="flex-end" align="center">
        <ThemeToggle />
        <AAvatar>未</AAvatar>
      </BkFlex>
    </BkCell>

    <BkCell area="2/3/3/4" class="bk-p-sm">
      <RouterView v-slot="{ Component }">
        <Transition name="transition-fade" mode="out-in">
          <component :is="Component" :key="route.path"></component>
        </Transition>
      </RouterView>
    </BkCell>
  </BkGrid>
</template>

<script setup lang="ts">
import { computed, reactive } from 'vue'
import LayoutNavs from './components/LayoutNavs.vue'
import { useRoute, useRouter } from 'vue-router'
import type { MenuItem } from '@/store'
import { useAppStore } from '@/store'
import ThemeToggle from './components/ThemeToggle.vue'

defineOptions({ name: 'Layout' })
const router = useRouter()
const route = useRoute()
const appStore = useAppStore()
type LayoutState = {
  activeNavKey: string
  navCollapsed: boolean
}
const state = reactive<LayoutState>({
  activeNavKey: route.matched[1].path,
  navCollapsed: false
})

const menus = computed(() => {
  let find = appStore.menus.find((item) => item.key === state.activeNavKey)
  if (find) return find.children ?? []
  return []
})

const onNavItemClick = (item: MenuItem) => {
  if (item.key === state.activeNavKey) return
  state.activeNavKey = item.key
  router.push(item.key)
}
</script>

<style scoped lang="css">
.navs-panel {
  position: absolute;
  top: 50%;
  left: 0;
  transform: translate(0, -50%);
  transition: transform 0.3s ease-in-out;
  width: 80px;
}
.navs-container {
  @apply bk-bg-container bk-shadow;
  width: 80px;
  border-radius: 0 20px 20px 0;
  overflow: hidden;
}
.navs-panel.collapsed {
  transform: translate(-100%, -50%);
}
.navs-collapsed-btn {
  @apply bk-bg-container bk-shadow b-l-1;
  width: 20px;
  height: 80px;
  display: flex;
  justify-content: center;
  align-items: center;
  border-radius: 0 20px 20px 0;
  box-shadow: 0 0 5px rgba(0, 0, 0, 0.3);
  cursor: pointer;
  position: absolute;
  top: 50%;
  right: -20px;
  transform: translateY(-50%);
}
</style>
