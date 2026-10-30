<template>
  <div :style="styles">
    <slot></slot>
  </div>
</template>

<script setup lang="ts">
import type { CSSProperties } from 'vue'
import type { GridProps } from './type'
import { computed } from 'vue'
defineOptions({ name: 'Grid' })
const props = withDefaults(defineProps<GridProps>(), {
  inlineGrid: false,
  width: '100%',
  height: '100%',
  rowGap: '10px',
  colGap: '10px'
})

const styles = computed<CSSProperties>(() => {
  return {
    display: props.inlineGrid ? 'inline-grid' : 'grid',
    width: props.width,
    height: props.height,
    'min-width': props.minWidth,
    'min-height': props.minHeight,
    'max-width': props.maxWidth,
    'max-height': props.maxHeight,
    'grid-template-rows': props.tmplRows,
    'grid-template-columns': props.tmplCols,
    'grid-template-areas': props.tmplAreas,
    'row-gap': props.rowGap,
    'column-gap': props.colGap
  }
})
</script>
