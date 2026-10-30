<template>
  <div :style="styles">
    <slot></slot>
  </div>
</template>

<script setup lang="ts">
import type { CSSProperties } from 'vue'
import type { FlexProps } from './type'
import { computed } from 'vue'
defineOptions({ name: 'Flex' })
const props = withDefaults(defineProps<FlexProps>(), {
  wrap: true,
  rowGap: '10px',
  colGap: '10px'
})

const styles = computed<CSSProperties>(() => {
  let flexDirection: CSSProperties['flexDirection']
  if (props.vertical && !props.reverse) {
    flexDirection = 'column'
  } else if (props.vertical && props.reverse) {
    flexDirection = 'column-reverse'
  } else if (!props.vertical && props.reverse) {
    flexDirection = 'row-reverse'
  } else {
    flexDirection = 'row'
  }
  return {
    display: props.inline ? 'inline-flex' : 'flex',
    'flex-direction': flexDirection,
    'align-items': props.align,
    'justify-content': props.justify,
    'flex-wrap': props.wrap !== false ? 'wrap' : 'nowrap',
    'row-gap': props.rowGap,
    'column-gap': props.colGap
  }
})
</script>

<style scoped></style>
