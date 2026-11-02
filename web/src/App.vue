<template>
  <ConfigProvider :prefix-cls="PREFIX" :theme="{ cssVar: { prefix: PREFIX }, token: appStore.themeToken }">
    <AApp class="w-full h-full bk-bg-layout">
      <RouterView />
    </AApp>
  </ConfigProvider>
</template>

<script setup lang="ts">
import { App as AApp, ConfigProvider } from 'antdv-next'
import { useAppStore } from './store'
import { PREFIX } from './constants'
defineOptions({ name: 'App' })

const appStore = useAppStore()
</script>

<style scoped></style>
