<template>
  <div class="w-full h-full">
    <div
      v-for="item in props.items"
      :key="item.key"
      class="flex flex-col items-center bk-py-sm cursor-pointer"
      :class="[item.key === props.activeKey ? 'bk-bg-primary-bg-hover text-primary' : '']"
      @click="emits('itemClick', item)"
    >
      <BkIcon v-if="item.iconName" :name="item.iconName"></BkIcon>
      <div>{{ item.label }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MenuItem } from '@/store'

defineOptions({ name: 'LayoutNavs' })

const props = withDefaults(
  defineProps<{
    activeKey?: MenuItem['key']
    items?: MenuItem[]
  }>(),
  {
    activeKey: '',
    items: () => []
  }
)

const emits = defineEmits<{
  itemClick: [item: MenuItem]
}>()
</script>

<style scoped></style>
