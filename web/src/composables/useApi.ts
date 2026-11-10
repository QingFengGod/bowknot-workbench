import { reactive, type UnwrapRef } from 'vue'

type ApiFn<Ar> = () => Promise<Ar>
type UseApiOpt<Ar, Dr = Ar> = {
  transformResult?: (result: Ar) => Dr
  immediate?: boolean
  onError?: (err: unknown) => void
}

type UseApiState<Dr> = {
  loading: boolean
  isError: boolean
  data: Dr | null
}

type UseApiMethods = {
  refresh: () => Promise<void>
}

type UseApiReturn<Dr> = [UseApiState<Dr>, UseApiMethods]
export function useApi<Ar, Dr = Ar>(apiFn: ApiFn<Ar>, opt?: UseApiOpt<Ar, Dr>): UseApiReturn<Dr> {
  const { immediate = true, onError, transformResult } = opt ?? {}
  const state = reactive<UseApiState<Dr>>({
    loading: false,
    isError: false,
    data: null
  })

  async function refresh() {
    state.loading = true
    try {
      const res = await apiFn()
      state.isError = false
      const finalData = typeof transformResult === 'function' ? transformResult(res) : res
      state.data = finalData as UnwrapRef<Dr>
    } catch (err) {
      state.isError = true
      typeof onError == 'function' && onError(err)
    }
    state.loading = false
  }

  const methods: UseApiMethods = {
    refresh
  }

  if (immediate) {
    refresh()
  }

  return [state as UseApiState<Dr>, methods]
}
