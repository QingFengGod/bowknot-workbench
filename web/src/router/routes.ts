import type { RouteRecordRaw } from 'vue-router'

export const rootRoute: RouteRecordRaw = {
  path: '/',
  name: 'Root',
  component: () => import('@/layout/index.vue'),
  redirect: '/workbench',
  children: [
    {
      path: '/workbench',
      name: 'WorkbenchPage',
      component: () => import('@/views/workbench/index.vue')
    },
    {
      path: '/assistant',
      name: 'AssistantPage',
      component: () => import('@/views/assistant/index.vue')
    }
  ]
}

export const loginRoute: RouteRecordRaw = {
  path: '/login',
  name: 'LoginPage',
  component: () => import('@/views/login/index.vue')
}

export const notFoundRoute: RouteRecordRaw = {
  path: '/:pathMatch(.*)*',
  name: 'NotFound',
  component: () => import('@/views/error/notFound/index.vue')
}
