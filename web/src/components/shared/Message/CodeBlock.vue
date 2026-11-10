<template>
  <div class="code-block">
    <div class="code-head">
      <span class="code-lang">{{ lang || 'text' }}</span>
      <button type="button" class="code-copy" @click="copy">{{ copied ? '已复制' : '复制' }}</button>
    </div>
    <pre class="code-body"><code>{{ code }}</code></pre>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'

defineOptions({ name: 'CodeBlock' })

const props = defineProps<{
  code: string
  lang?: string
}>()

const copied = ref(false)
let timer: ReturnType<typeof setTimeout> | null = null

const copy = async () => {
  try {
    await navigator.clipboard.writeText(props.code)
  } catch {
    return
  }
  copied.value = true
  if (timer !== null) clearTimeout(timer)
  timer = setTimeout(() => {
    copied.value = false
    timer = null
  }, 1600)
}

onBeforeUnmount(() => {
  if (timer !== null) clearTimeout(timer)
})
</script>

<style scoped lang="css">
.code-block {
  @apply b-all-1 bk-rd bk-my-sm;
  overflow: hidden;
}

.code-head {
  @apply bk-bg-fill-secondary bk-px-sm;
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 32px;
  border-bottom: 1px solid var(--bk-color-split);
}

.code-lang {
  @apply bk-color-text-tertiary;
  font-family: 'JetBrains Mono', 'Cascadia Code', Consolas, monospace;
  font-size: 12px;
}

.code-copy {
  @apply bk-color-text-secondary bk-rd-xs;
  font-size: 12px;
  padding: 2px 8px;
  border: 0;
  background: transparent;
  cursor: pointer;
}

.code-copy:hover {
  @apply bk-color-primary bk-bg-container;
}

.code-body {
  margin: 0;
  padding: 12px 14px;
  overflow-x: auto;
  font-family: 'JetBrains Mono', 'Cascadia Code', Consolas, monospace;
  font-size: 12.5px;
  line-height: 1.6;
}
</style>
