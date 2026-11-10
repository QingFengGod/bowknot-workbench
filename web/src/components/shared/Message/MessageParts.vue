<template>
  <div class="message-parts">
    <template v-for="(part, index) in visibleParts" :key="index">
      <div v-if="part.type === 'text'" class="part-text">
        <MarkdownBlock
          v-for="block in splitBlocks(part.text)"
          :key="block.id"
          :content="block.content"
          :done="block.done || !streaming"
        />
      </div>
      <CodeBlock v-else-if="part.type === 'code'" :code="part.code" :lang="part.lang" />
      <template v-else-if="part.type === 'card'">
        <component :is="resolveCardPart(part.component)" v-if="resolveCardPart(part.component)" v-bind="part.props" />
        <div v-else class="part-fallback">未注册的卡片：{{ part.component }}</div>
      </template>
      <component v-else-if="resolvePart(part.type)" :is="resolvePart(part.type)" :part="part" />
      <div v-else class="part-fallback">暂不支持的内容类型：{{ part.type }}</div>
    </template>
  </div>
</template>

<script setup lang="ts">
import type { MessagePart } from '@/types/chat'
import { computed } from 'vue'
import { splitBlocks } from '@/utils/markdown'
import MarkdownBlock from '../Markdown/MarkdownBlock.vue'
import CodeBlock from './CodeBlock.vue'
import { resolveCardPart, resolvePart } from './partRegistry'

defineOptions({ name: 'MessageParts' })

const props = defineProps<{
  parts: MessagePart[]
  /** 流式结束后末块也要按完整语法渲染（例如表格） */
  streaming?: boolean
}>()

/** emotion 只作为消息元数据展示，不占用正文块 */
const visibleParts = computed<MessagePart[]>(() => props.parts.filter((part) => part.type !== 'emotion'))
</script>

<style scoped lang="css">
.message-parts {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.part-fallback {
  @apply bk-color-text-tertiary bk-bg-fill-secondary bk-rd bk-p-sm;
  font-size: 12px;
}
</style>
