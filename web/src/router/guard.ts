import type { Router } from 'vue-router'

export function withRouterGuard(router: Router) {
  router.beforeEach(async () => {
    return true
  })
}
