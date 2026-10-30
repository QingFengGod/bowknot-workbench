import { createPinia } from 'pinia'
export * from './app'

export function getStore() {
  const pinia = createPinia()
  return pinia
}
