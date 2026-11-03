import type { RouteRecordInfo } from 'vue-router'

export interface RouteNamedMap {
  Root: RouteRecordInfo<'Root', '/'>
  LoginPage: RouteRecordInfo<'LoginPage', '/login'>
  WorkbenchPage: RouteRecordInfo<'WorkbenchPage', '/workbench'>
  WorkbenchPage: RouteRecordInfo<'WorkbenchPage', '/workbench'>
  NotFoundPage: RouteRecordInfo<'NotFoundPage', string>
}

export type RoutePaths = keyof RouteNamedMap
