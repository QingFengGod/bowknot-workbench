import type { Plugin } from 'vue'
import { createResizeDirective } from './resize'
export type { ResizeDirective } from './resize'

export function globalDirectives(): Plugin {
  return {
    install(app) {
      app.directive('resize', createResizeDirective())
    }
  }
}
