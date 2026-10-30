<template>
  <i :class="iconClass" :style="styles"></i>
</template>

<script setup lang="ts">
import type { CSSProperties } from 'vue'
import type { IconProps } from './type'
import { computed } from 'vue'

defineOptions({ name: 'Icon' })

const props = defineProps<IconProps>()

const iconClass = computed(() => `icon-${props.name}`)

const styles = computed<CSSProperties>(() => {
  return {
    color: props.color,
    width: props.size,
    height: props.size,
    padding: props.padding,
    transform: props.rotate ? `rotate(${props.rotate})` : undefined
  }
})
</script>

<style scoped></style>
