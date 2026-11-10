export type MessageStatus = 'sending' | 'streaming' | 'done' | 'failed' | 'stopped'

export type TextPart = { type: 'text'; text: string }

export type CodePart = { type: 'code'; lang?: string; code: string; done?: boolean }

export type ToolPart = {
  type: 'tool'
  id: string
  name: string
  status: 'running' | 'done' | 'error'
  args?: unknown
  result?: unknown
}

export type EmotionPart = { type: 'emotion'; emotion: string; action?: string }

export type CardPart = { type: 'card'; component: string; props: Record<string, unknown> }

export type MessagePart = TextPart | CodePart | ToolPart | EmotionPart | CardPart

export type ChatMessage = {
  id: string
  conversationId: string
  role: 'user' | 'assistant'
  parts: MessagePart[]
  status: MessageStatus
  createdAt: number
  error?: string
  memoryWritten?: boolean
}

/** 流式事件按 part 级全量替换，前端不做字符串拼接 */
export type StreamEvent =
  | { type: 'start'; message: ChatMessage }
  | { type: 'update'; index: number; part: MessagePart }
  | { type: 'end'; status: 'done' | 'stopped' }
  | { type: 'error'; message: string }

export type MarkdownBlockData = {
  id: string
  content: string
  done: boolean
}
