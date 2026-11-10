import type { ChatMessage, MessagePart } from '@/types/chat'

export type StreamReply = {
  text: string
  /** 生成到该字符数时失败，用于复现错误分支 */
  errorAt?: number
  errorMessage?: string
}

export type MessageStreamOptions = {
  /** 每次吐字的间隔 */
  interval?: number
  /** 每次最少推进的字符数 */
  charsPerTick?: number
}

type ActiveStream = {
  message: ChatMessage
  reply: StreamReply
  cursor: number
}

let seed = 0
const createId = () => `msg-${Date.now().toString(36)}-${(seed++).toString(36)}`

/** 尽量在空行或换行处停下，让每次 update 都落在块/行边界 */
const nextCursor = (text: string, cursor: number, step: number) => {
  const minimum = Math.min(text.length, cursor + step)
  const limit = Math.min(text.length, minimum + 90)
  const paragraph = text.indexOf('\n\n', minimum)
  if (paragraph !== -1 && paragraph < limit) return paragraph + 2
  const line = text.indexOf('\n', minimum)
  if (line !== -1 && line < limit) return line + 1
  return minimum
}

export function useMessageStream(options: MessageStreamOptions = {}) {
  const interval = options.interval ?? 32
  const charsPerTick = options.charsPerTick ?? 6
  let timer: ReturnType<typeof setTimeout> | null = null
  let active: ActiveStream | null = null

  const clearTimer = () => {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
  }

  const finish = (status: 'done' | 'stopped') => {
    if (!active) return
    active.message.status = status
    clearTimer()
    active = null
  }

  const fail = (reason: string) => {
    if (!active) return
    active.message.status = 'failed'
    active.message.error = reason
    clearTimer()
    active = null
  }

  const tick = () => {
    timer = setTimeout(() => {
      timer = null
      const current = active
      if (!current) return
      const { reply, message } = current
      current.cursor = nextCursor(reply.text, current.cursor, charsPerTick)
      const parts: MessagePart[] = [{ type: 'text', text: reply.text.slice(0, current.cursor) }]
      message.parts = parts
      if (reply.errorAt !== undefined && current.cursor >= reply.errorAt) {
        fail(reply.errorMessage ?? '生成失败，请稍后重试')
        return
      }
      if (current.cursor >= reply.text.length) {
        finish('done')
        return
      }
      tick()
    }, interval)
  }

  const run = (message: ChatMessage, reply: StreamReply) => {
    clearTimer()
    message.parts = [{ type: 'text', text: '' }]
    message.status = 'streaming'
    message.error = undefined
    active = { message, reply, cursor: 0 }
    tick()
  }

  const start = (messages: ChatMessage[], conversationId: string, reply: StreamReply) => {
    const message: ChatMessage = {
      id: createId(),
      conversationId,
      role: 'assistant',
      parts: [{ type: 'text', text: '' }],
      status: 'streaming',
      createdAt: Date.now()
    }
    messages.push(message)
    // 从数组里取回响应式代理，直接改原始对象不会触发视图更新
    const stored = messages[messages.length - 1] ?? message
    run(stored, reply)
    return stored
  }

  /** 只替换目标回复本身，不改动用户消息 */
  const regenerate = (message: ChatMessage, reply: StreamReply) => {
    run(message, reply)
  }

  const stop = (messageId?: string) => {
    if (active && (!messageId || active.message.id === messageId)) finish('stopped')
  }

  const dispose = () => {
    clearTimer()
    active = null
  }

  return { start, stop, regenerate, dispose }
}
