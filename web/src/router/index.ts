import type { RouteRecordRaw } from 'vue-router'
import { createRouter, createWebHashHistory, createWebHistory } from 'vue-router'
import { withRouterGuard } from './guard'
import { loginRoute, notFoundRoute, rootRoute } from './routes'
import type { Component } from 'vue'

const router = createRouter({
  history:
    $G.env('VITE_ROUTER_TYPE') === 'web-history'
      ? createWebHistory($G.env('VITE_ROUTER_BASE'))
      : createWebHashHistory($G.env('VITE_ROUTER_BASE')),
  routes: []
})

withRouterGuard(router)

export function getRouter() {
  return router
}

const componentsMap: Record<string, () => Component> = {}

export function getComponentKeys() {
  return Object.keys(componentsMap)
}

export function initDynamicRoutes(_routes: RouteRecordRaw[]) {
  router.clearRoutes()
  router.addRoute(rootRoute)
  router.addRoute(loginRoute)
  router.addRoute(notFoundRoute)
}

initDynamicRoutes([])
