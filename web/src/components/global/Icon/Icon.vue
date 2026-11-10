<template>
  <div class="bk-icon" :style="styles">
    <i :class="`icon i-${props.name}`" />
  </div>
</template>

<script setup lang="ts">
import type { CSSProperties } from 'vue'
import type { IconProps } from './type'
import { computed } from 'vue'

defineOptions({ name: 'Icon' })

const props = withDefaults(defineProps<IconProps>(), {
  size: '24px'
})

const styles = computed<CSSProperties>(() => {
  const styled: CSSProperties = {}
  if (typeof props.containerSize === 'string') {
    styled['--icon-container-width'] = props.containerSize
    styled['--icon-container-height'] = props.containerSize
  } else if (Array.isArray(props.containerSize) && props.containerSize.length == 2) {
    styled['--icon-container-width'] = props.containerSize[0]
    styled['--icon-container-height'] = props.containerSize[1]
  }
  if (typeof props.size === 'string') {
    styled['--icon-width'] = props.size
    styled['--icon-height'] = props.size
  } else if (Array.isArray(props.size) && props.size.length == 2) {
    styled['--icon-width'] = props.size[0]
    styled['--icon-height'] = props.size[1]
  }
  if (typeof props.color === 'string') {
    styled['--icon-color'] = props.color
  }
  if (typeof props.rotate === 'number') {
    styled['--icon-rotate'] = props.rotate + 'deg'
  }
  if (styled['--icon-container-width']) {
    styled.display = 'flex'
    styled.justifyContent = 'center'
    styled.alignItems = 'center'
  }
  return styled
})
</script>

<style lang="css">
@import './icons.css';
</style>
