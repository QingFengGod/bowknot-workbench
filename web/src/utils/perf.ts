const DEV = import.meta.env.DEV

/**
 * 仅在开发环境计时，超过阈值时打印告警。
 * 生产构建会被 `DEV` 短路，不产生额外开销。
 */
export function perfMeasure<T>(label: string, fn: () => T, threshold = 2): T {
  if (!DEV) return fn()
  const start = performance.now()
  const result = fn()
  const cost = performance.now() - start
  if (cost >= threshold) {
    console.warn(`[perf] ${label} ${cost.toFixed(2)}ms（阈值 ${threshold}ms）`)
  }
  return result
}
