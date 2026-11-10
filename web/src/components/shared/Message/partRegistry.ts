import type { Component } from 'vue'
import ToolPart from './ToolPart.vue'

const registry = new Map<string, Component>()
const CARD_PREFIX = 'card:'

export function registerPart(name: string, component: Component) {
  registry.set(name, component)
}

export function resolvePart(name: string) {
  return registry.get(name)
}

/** 富卡片按组件名注册，新增卡片不需要改动消息分发组件 */
export function registerCardPart(name: string, component: Component) {
  registerPart(`${CARD_PREFIX}${name}`, component)
}

export function resolveCardPart(name: string) {
  return registry.get(`${CARD_PREFIX}${name}`)
}

registerPart('tool', ToolPart)
