<template>
  <div class="min-w-0 min-h-0" :style="styles">
    <slot></slot>
  </div>
</template>

<script setup lang="ts">
import type { CSSProperties } from 'vue'
import { computed } from 'vue'
import type { CellProps } from './type'
defineOptions({ name: 'Cell' })
const props = withDefaults(defineProps<CellProps>(), {
  rowGap: '10px',
  colGap: '10px'
})

const styles = computed<CSSProperties>(() => {
  let styled: CSSProperties = {}
  if (props.area) {
    styled['grid-area'] = props.area
  } else if (props.row || props.col) {
    styled['grid-row'] = props.row ?? 'auto'
    styled['grid-column'] = props.col ?? 'auto'
  } else {
    styled['grid-area'] = 'auto'
  }
  if (props.flex || props.inlineFlex) {
    styled.display = props.inlineFlex ? 'flex' : 'inline-flex'
    if (props.vertical && !props.reverse) {
      styled['flex-direction'] = 'column'
    } else if (props.vertical && props.reverse) {
      styled['flex-direction'] = 'column-reverse'
    } else if (!props.vertical && props.reverse) {
      styled['flex-direction'] = 'row-reverse'
    } else {
      styled['flex-direction'] = 'row'
    }
    styled['align-items'] = props.align
    styled['justify-content'] = props.justify
    styled['flex-wrap'] = props.wrap !== false ? 'wrap' : 'nowrap'
    styled['row-gap'] = props.rowGap
    styled['column-gap'] = props.colGap
  }
  return styled
})
</script>

<style scoped></style>
