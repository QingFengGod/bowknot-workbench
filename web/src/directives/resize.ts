import type { Directive } from 'vue'

export type DResizeSize = {
  contentBoxSize: { width: number; height: number }
  borderBoxSize: { width: number; height: number }
}
export type DResizeFn = (size: DResizeSize) => void

export type ResizeDirective = Directive<Element, DResizeFn>

export interface ResizeDirectiveOpt {
  throttleInterval?: number
}

export function createResizeDirective(opt: ResizeDirectiveOpt = {}): ResizeDirective {
  const weakMap = new WeakMap<Element, DResizeFn>()
  const { throttleInterval = 380 } = opt
  let lastTime = 0
  const resizeFn = (entries: ResizeObserverEntry[]) => {
    const now = Date.now()
    if (now - lastTime < throttleInterval) {
      return
    }
    entries.forEach((entry) => {
      const callback = weakMap.get(entry.target as Element)
      if (callback) {
        callback({
          contentBoxSize: {
            width: entry.borderBoxSize[0].inlineSize,
            height: entry.borderBoxSize[0].blockSize
          },
          borderBoxSize: {
            width: entry.borderBoxSize[0].inlineSize,
            height: entry.borderBoxSize[0].blockSize
          }
        })
      }
    })
    lastTime = now
  }

  const resizeObserver = new ResizeObserver(resizeFn)
  return {
    mounted(el, binding) {
      el.getBoundingClientRect()
      resizeObserver.observe(el)
      weakMap.set(el, binding.value)
    },
    beforeUnmount(el) {
      resizeObserver.unobserve(el)
      weakMap.delete(el)
    }
  }
}
