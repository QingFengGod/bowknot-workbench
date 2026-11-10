<template>
  <div class="tool-part">
    <div class="tool-head">
      <span class="tool-dot" :class="`is-${part.status}`" />
      <span class="tool-name">{{ part.name }}</span>
      <span class="tool-status">{{ statusText }}</span>
    </div>
    <pre v-if="argsText" class="tool-args">{{ argsText }}</pre>
  </div>
</template>

<script setup lang="ts">
import type { ToolPart } from '@/types/chat'
import { computed } from 'vue'

defineOptions({ name: 'ToolPart' })

const props = defineProps<{
  part: ToolPart
}>()

const STATUS_TEXT: Record<ToolPart['status'], string> = {
  running: '执行中',
  done: '已完成',
  error: '失败'
}

const statusText = computed(() => STATUS_TEXT[props.part.status])

const argsText = computed(() => {
  if (props.part.args === undefined) return ''
  try {
    return JSON.stringify(props.part.args, null, 2)
  } catch {
    return String(props.part.args)
  }
})
</script>

<style scoped lang="css">
.tool-part {
  @apply b-all-1 bk-rd bk-my-sm;
  overflow: hidden;
}

.tool-head {
  @apply bk-bg-fill-secondary bk-px-sm;
  display: flex;
  align-items: center;
  gap: 8px;
  height: 32px;
}

.tool-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--bk-color-text-tertiary);
}
.tool-dot.is-running {
  background: var(--bk-color-primary);
}
.tool-dot.is-done {
  background: var(--bk-color-success);
}
.tool-dot.is-error {
  background: var(--bk-color-error);
}

.tool-name {
  font-size: 13px;
  font-weight: 600;
}

.tool-status {
  @apply bk-color-text-tertiary;
  font-size: 12px;
}

.tool-args {
  margin: 0;
  padding: 10px 14px;
  overflow-x: auto;
  font-family: 'JetBrains Mono', 'Cascadia Code', Consolas, monospace;
  font-size: 12px;
  line-height: 1.6;
}
</style>
