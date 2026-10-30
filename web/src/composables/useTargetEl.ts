import type { ShallowRef } from 'vue'
import { isRef } from 'vue'
export type TargetEl<Inst = Element> = Element | (() => Element) | string | Readonly<ShallowRef<Inst | null>>

export type TargetElWrapper<Inst = Element> = {
  value: Inst | null
}
export function useTargetEl<Inst = Element>(targetEl: TargetEl<Inst>) {
  if (typeof targetEl === 'function') {
    return { value: targetEl() }
  } else if (typeof targetEl === 'string') {
    return { value: document.querySelector(targetEl) }
  } else if (targetEl instanceof Element) {
    return { value: targetEl }
  } else if (isRef(targetEl)) {
    return targetEl
  }
  return { value: null }
}
