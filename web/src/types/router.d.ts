import type { RoutePathKey } from '@/common/enums'
import type { RouteNamedMap } from './route'
import type { VNode } from 'vue'

declare module 'vue-router' {
  interface RouteMeta {}

  interface TypesConfig {
    RouteNamedMap: RouteNamedMap
  }
}

export {}
