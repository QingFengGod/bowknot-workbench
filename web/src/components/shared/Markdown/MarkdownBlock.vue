<template>
  <div class="markdown-block" v-html="html" />
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { renderBlock } from '@/utils/markdown'

defineOptions({ name: 'MarkdownBlock' })

const props = defineProps<{
  content: string
  done?: boolean
}>()

/** 流式块的最小重算间隔，避免每个字符都触发一次 Markdown 解析 */
const MIN_INTERVAL = 60

const html = ref('')
let rafId = 0
let timerId: ReturnType<typeof setTimeout> | null = null
let lastRenderAt = 0

const cancelPending = () => {
  if (rafId) {
    cancelAnimationFrame(rafId)
    rafId = 0
  }
  if (timerId !== null) {
    clearTimeout(timerId)
    timerId = null
  }
}

const render = () => {
  rafId = 0
  timerId = null
  lastRenderAt = performance.now()
  html.value = renderBlock({ id: 'stream', content: props.content, done: props.done === true })
}

const schedule = () => {
  if (props.done) {
    cancelPending()
    render()
    return
  }
  if (rafId || timerId !== null) return
  rafId = requestAnimationFrame(() => {
    rafId = 0
    const wait = MIN_INTERVAL - (performance.now() - lastRenderAt)
    if (wait > 0) timerId = setTimeout(render, wait)
    else render()
  })
}

watch(() => [props.content, props.done], schedule, { immediate: true })

onBeforeUnmount(cancelPending)
</script>

<style scoped lang="css">
.markdown-block {
  @apply bk-color-text;
  font-size: 14px;
  line-height: 1.75;
  word-break: break-word;
}

.markdown-block :deep(> :first-child) {
  margin-top: 0;
}
.markdown-block :deep(> :last-child) {
  margin-bottom: 0;
}

.markdown-block :deep(h1),
.markdown-block :deep(h2),
.markdown-block :deep(h3),
.markdown-block :deep(h4),
.markdown-block :deep(h5),
.markdown-block :deep(h6) {
  margin: 1.1em 0 0.55em;
  font-weight: 600;
  line-height: 1.4;
}
.markdown-block :deep(h1) {
  font-size: 22px;
}
.markdown-block :deep(h2) {
  font-size: 18px;
}
.markdown-block :deep(h3) {
  font-size: 16px;
}
.markdown-block :deep(h4) {
  font-size: 14px;
}

.markdown-block :deep(p) {
  margin: 0.6em 0;
}

.markdown-block :deep(ul),
.markdown-block :deep(ol) {
  margin: 0.6em 0;
  padding-left: 1.4em;
}
.markdown-block :deep(ul) {
  list-style: disc;
}
.markdown-block :deep(ol) {
  list-style: decimal;
}
.markdown-block :deep(li) {
  margin: 0.25em 0;
}
.markdown-block :deep(li > p) {
  margin: 0;
}

.markdown-block :deep(a) {
  @apply bk-color-primary;
  text-decoration: underline;
  text-underline-offset: 2px;
}

.markdown-block :deep(blockquote) {
  @apply bk-mx-0 bk-my-sm bk-bg-fill-secondary;
  padding: 6px 12px;
  border-left: 3px solid var(--bk-color-primary);
  border-radius: 4px;
  color: var(--bk-color-text-secondary);
}

.markdown-block :deep(hr) {
  @apply b-t-1;
  margin: 1em 0;
}

.markdown-block :deep(code) {
  font-family: 'JetBrains Mono', 'Cascadia Code', Consolas, monospace;
  font-size: 12.5px;
  padding: 2px 5px;
  border-radius: 4px;
  background: var(--bk-color-fill-secondary);
}

.markdown-block :deep(pre) {
  @apply bk-bg-fill-secondary bk-rd bk-my-sm;
  padding: 12px 14px;
  overflow-x: auto;
}
.markdown-block :deep(pre code) {
  padding: 0;
  background: transparent;
  font-size: 12.5px;
  line-height: 1.6;
}

.markdown-block :deep(table) {
  width: 100%;
  margin: 0.8em 0;
  border-collapse: collapse;
  font-size: 13px;
}
.markdown-block :deep(th),
.markdown-block :deep(td) {
  @apply b-all-1;
  padding: 6px 10px;
  text-align: left;
}
.markdown-block :deep(th) {
  @apply bk-bg-fill-secondary;
  font-weight: 600;
}

.markdown-block :deep(img) {
  max-width: 100%;
  border-radius: 8px;
}
</style>
