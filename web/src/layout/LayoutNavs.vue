<template>
  <div class="w-full h-full">
    <div
      v-for="item in props.items"
      :key="item.key"
      class="flex flex-col items-center bk-py-sm cursor-pointer"
      :class="[item.key === props.activeKey ? 'bg-primary text-white' : '']"
      @click="emits('itemClick', item)"
    >
      <BkIcon :name="item.iconName"></BkIcon>
      <div>{{ item.title }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { NavItem } from './type'

defineOptions({ name: 'LayoutNavs' })

const props = withDefaults(
  defineProps<{
    activeKey?: NavItem['key']
    items?: NavItem[]
  }>(),
  {
    activeKey: '',
    items: () => []
  }
)

const emits = defineEmits<{
  itemClick: [item: NavItem]
}>()
</script>

<style scoped></style>
