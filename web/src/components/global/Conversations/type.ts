export type ConversationItem = {
  id: string
  title: string
  /** 最后一条消息摘要 */
  summary?: string
  /** 更新时间，支持时间戳、日期字符串或 Date */
  updatedAt?: number | string | Date
}

export type ConversationsProps = {
  items?: ConversationItem[]
  activeId?: string
  loading?: boolean
  title?: string
  emptyText?: string
  emptyHint?: string
}
