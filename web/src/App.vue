<template>
  <ConfigProvider
    :prefix-cls="PREFIX"
    :theme="{
      algorithm: appStore.isDark ? theme.darkAlgorithm : theme.defaultAlgorithm,
      cssVar: { prefix: PREFIX },
      token: appStore.isDark ? appStore.darkToken : appStore.lightToken
    }"
  >
    <AApp class="w-full h-full bk-bg-layout">
      <RouterView />
    </AApp>
  </ConfigProvider>
</template>

<script setup lang="ts">
import { App as AApp, ConfigProvider, theme } from 'antdv-next'
import { watchEffect } from 'vue'
import { useAppStore } from './store'
import { PREFIX } from './constants'
defineOptions({ name: 'App' })

const appStore = useAppStore()

watchEffect(() => {
  document.documentElement.classList.toggle('dark', appStore.isDark)
})
</script>

<style scoped></style>
