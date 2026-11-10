import type { MarkdownBlockData } from '@/types/chat'
import DOMPurify from 'dompurify'
import MarkdownIt from 'markdown-it'
import { perfMeasure } from './perf'

const createRenderer = () => {
  const instance = new MarkdownIt({
    html: false,
    linkify: true,
    breaks: true
  })
  instance.renderer.rules.link_open = (tokens, idx, options, _env, self) => {
    tokens[idx].attrSet('target', '_blank')
    tokens[idx].attrSet('rel', 'noopener noreferrer')
    return self.renderToken(tokens, idx, options)
  }
  return instance
}

const renderer = createRenderer()
/** 流式中的块不渲染表格，避免分隔行未到时的结构抖动 */
const streamingRenderer = createRenderer()
streamingRenderer.disable('table')

const cache = new Map<string, string>()
const CACHE_LIMIT = 500

const FENCE_RE = /^\s*(`{3,}|~{3,})/

/** 按空行与围栏代码切块；表格行之间没有空行，天然落在同一块内 */
export function splitBlocks(text: string): MarkdownBlockData[] {
  const lines = text.split('\n')
  const blocks: string[] = []
  let buffer: string[] = []
  let fence: string | null = null

  const flush = () => {
    const content = buffer.join('\n')
    if (content.trim()) blocks.push(content)
    buffer = []
  }

  for (const line of lines) {
    const trimmed = line.trimStart()
    if (fence) {
      buffer.push(line)
      if (trimmed.startsWith(fence)) fence = null
      continue
    }
    const fenceMatch = trimmed.match(FENCE_RE)
    if (fenceMatch) {
      fence = fenceMatch[1]
      buffer.push(line)
      continue
    }
    if (!line.trim()) {
      flush()
      continue
    }
    buffer.push(line)
  }
  flush()

  // 以空行结尾说明最后一块已经完整，不必每次流式都重算
  const endsWithBlank = /\n\s*$/.test(text)
  return blocks.map((content, index) => ({
    id: `block-${index}`,
    content,
    done: index < blocks.length - 1 || endsWithBlank
  }))
}

const closeUnclosedFence = (content: string) => {
  const fences = content.match(/^\s*(`{3,}|~{3,})/gm) ?? []
  if (fences.length % 2 === 0) return content
  const marker = fences[fences.length - 1].trim().slice(0, 3)
  return `${content}\n${marker}`
}

const renderSource = (content: string, done: boolean) => {
  const source = done ? content : closeUnclosedFence(content)
  const html = (done ? renderer : streamingRenderer).render(source)
  return DOMPurify.sanitize(html, { USE_PROFILES: { html: true } })
}

/** 已完成块只渲染一次，流式块每次更新都重新计算 */
export function renderBlock(block: MarkdownBlockData): string {
  if (block.done) {
    const cached = cache.get(block.content)
    if (cached !== undefined) return cached
  }
  const html = perfMeasure(`markdown ${block.id}`, () => renderSource(block.content, block.done))
  if (block.done) {
    if (cache.size >= CACHE_LIMIT) cache.clear()
    cache.set(block.content, html)
  }
  return html
}
