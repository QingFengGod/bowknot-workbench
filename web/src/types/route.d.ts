import type { RouteRecordInfo } from 'vue-router'

export interface RouteNamedMap {
  Root: RouteRecordInfo<'Root', '/'>
  Login: RouteRecordInfo<'Login', '/login'>
  Workbench: RouteRecordInfo<'Workbench', '/workbench'>
  NotFound: RouteRecordInfo<'NotFound', string>
}

export type RoutePaths = keyof RouteNamedMap
