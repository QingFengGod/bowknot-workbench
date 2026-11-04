<template>
  <div class="w-full h-full relative overflow-x-clip" style="--layout-header-height: 52px">
    <div class="h-52px bk-bg-container flex bk-shadow b-b-1">
      <div class="text-primary h-52px w-80px flex-center">
        <BkIcon name="game-icons-bow-tie-ribbon" size="28px" />
      </div>
      <BkFlex class="flex-1 bk-px-sm" justify="space-between" align="center">
        <span>s</span>
        <BkFlex justify="flex-end" align="center">
          <BkIcon name="line-md-moon-filled-loop" class="cursor-pointer" title="切换主题" @click="onToggleTheme" />
          <AAvatar>未</AAvatar>
        </BkFlex>
      </BkFlex>
    </div>
    <div class="bk-p-md" style="height: calc(100% - var(--layout-header-height))">
      <RouterView v-slot="{ Component }">
        <Transition name="transition-fade" mode="out-in">
          <component :is="Component" :key="route.path"></component>
        </Transition>
      </RouterView>
    </div>

    <div :class="['navs-panel', state.navCollapsed ? 'collapsed' : '']">
      <div class="navs-container">
        <LayoutNavs :active-key="state.activeNavKey" :items="navs" @item-click="onNavItemClick" />
      </div>
      <div class="navs-collapsed-btn" @click="state.navCollapsed = !state.navCollapsed">
        <BkIcon name="prime-chevron-right" size="16px" :rotate="state.navCollapsed ? '' : '180deg'" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, reactive } from 'vue'
import LayoutNavs from './LayoutNavs.vue'
import type { LayoutState, NavItem } from './type'
import { useRoute, useRouter } from 'vue-router'
import { useAppStore } from '@/store'

defineOptions({ name: 'Layout' })
const router = useRouter()
const route = useRoute()
const appStore = useAppStore()

const state = reactive<LayoutState>({
  activeNavKey: route.matched[1].path,
  navCollapsed: false
})

const navs: NavItem[] = [
  { title: '工作台', iconName: 'park-outline-workbench', key: '/workbench' },
  // { title: 'AppStore', iconName: 'prime-gift', key: '/app-store' },
  { title: 'Apps', iconName: 'prime-th-large', key: '/apps' },
  { title: '助手', iconName: 'griddy-icons-ai-assistant', key: '/assistant' },
  { title: 'agents', iconName: 'prime-share-alt', key: '/agents' },
  { title: 'AIGC', iconName: 'prime-sparkles', key: '/aigc' }
]

const onNavItemClick = (item: NavItem) => {
  if (item.key === state.activeNavKey) return
  state.activeNavKey = item.key
  router.push(item.key)
}

const onToggleTheme = (e: MouseEvent) => {
  const x = e.clientX
  const y = e.clientY
  // 画圆
  const endRadius = Math.hypot(Math.max(x, innerWidth - x), Math.max(y, innerHeight - y))
  const toDark = !appStore.isDark

  if (document.startViewTransition == undefined) {
    appStore.setThemeMode(toDark ? 'dark' : 'light')
    return
  }

  const transition = document.startViewTransition(async () => {
    appStore.setThemeMode(toDark ? 'dark' : 'light')
    await nextTick()
  })

  transition.ready.then(() => {
    const clipPath = [`circle(0px at ${x}px ${y}px)`, `circle(${endRadius}px at ${x}px ${y}px)`]
    document.documentElement.animate(
      {
        clipPath: toDark ? clipPath : clipPath.reverse()
      },
      {
        duration: 300,
        easing: 'ease-in',
        fill: 'both',
        pseudoElement: toDark ? '::view-transition-new(root)' : '::view-transition-old(root)'
      }
    )
  })
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
