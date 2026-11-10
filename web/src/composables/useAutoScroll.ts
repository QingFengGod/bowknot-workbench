import type { Ref } from 'vue'
import { ref } from 'vue'

export type AutoScrollOptions = {
  /** 距底部多少像素内仍视为“在底部” */
  threshold?: number
}

export function useAutoScroll(target?: Ref<HTMLElement | null>, options: AutoScrollOptions = {}) {
  const threshold = options.threshold ?? 80
  const scrollRef = target ?? ref<HTMLElement | null>(null)
  const stickToBottom = ref(true)
  const showBackToBottom = ref(false)

  const syncPosition = () => {
    const el = scrollRef.value
    if (!el) return
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight
    const atBottom = distance <= threshold
    stickToBottom.value = atBottom
    showBackToBottom.value = !atBottom
  }

  const scrollToBottom = (behavior: ScrollBehavior = 'smooth') => {
    const el = scrollRef.value
    if (!el) return
    el.scrollTo({ top: el.scrollHeight, behavior })
    stickToBottom.value = true
    showBackToBottom.value = false
  }

  /** 内容增长时调用：用户主动上滚后不再抢滚动位置 */
  const keepBottom = () => {
    if (stickToBottom.value) scrollToBottom('auto')
  }

  return { scrollRef, stickToBottom, showBackToBottom, syncPosition, scrollToBottom, keepBottom }
}
