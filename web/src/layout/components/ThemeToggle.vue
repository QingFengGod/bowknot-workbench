<template>
  <APopover trigger="click">
    <template #content>
      <BkFlex
        v-for="item in themeModeList"
        :key="item.value"
        class="popover-item"
        :class="item.value === appStore.themeMode ? ['bk-bg-primary-bg-hover text-primary'] : []"
        align="center"
        col-gap="6px"
        @click="onToggleTheme($event, item.value)"
      >
        <BkIcon :name="item.icon"></BkIcon>
        <span>{{ item.label }}</span>
      </BkFlex>
    </template>
    <div class="cursor-pointer w-24px h-24px" title="切换主题">
      <BkIcon :name="activeItem.icon" />
    </div>
  </APopover>
</template>

<script setup lang="ts">
import type { ThemeMode } from '@/store'
import { useAppStore } from '@/store'
import { computed, nextTick } from 'vue'

defineOptions({ name: 'ThemeToggle' })
const appStore = useAppStore()

const themeModeList = [
  { label: '跟随系统', icon: 'line-md-computer', value: 'system' },
  { label: '明亮模式', icon: 'line-md-sunny-loop', value: 'light' },
  { label: '暗黑模式', icon: 'line-md-moon-loop', value: 'dark' }
] as const

const activeItem = computed(() => {
  return themeModeList.find((item) => item.value === appStore.themeMode)!
})

const onToggleTheme = (e: MouseEvent, mode: ThemeMode) => {
  const x = e.clientX
  const y = e.clientY
  const toDark = mode === 'dark' || (mode === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  // 画圆
  const endRadius = Math.hypot(Math.max(x, innerWidth - x), Math.max(y, innerHeight - y))

  if (document.startViewTransition == undefined) {
    appStore.setThemeMode(mode)
    return
  }

  const transition = document.startViewTransition(async () => {
    appStore.setThemeMode(mode)
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

<style scoped></style>
