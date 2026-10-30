// import.meta.env 类型
interface ImportMetaEnv {
  // 模式
  readonly VITE_MODE: 'development' | 'production'
  // 路由模式
  readonly VITE_ROUTER_TYPE: 'web-history' | 'web-hash-history'
  // 路由和vite打包的前缀
  readonly VITE_ROUTER_BASE: string
  // api基础地址
  readonly VITE_API_BASEURL: string
  // 访问文件基础地址
  readonly VITE_OSS_FILE_BASE_URL: string
}

declare global {
  // 主键类型
  type DbRowId = string | number
  type SafeResult<Data = unknown, Err = Error> = [err: Err | null, data: Data | null]

  // import.meta.env 类型
  interface ImportMeta {
    readonly env: ImportMetaEnv
  }
  // 分页查询参数
  interface ApiPageQuery<T extends Record<string, any> = Record<string, any>> {
    page: number
    size: number
    filter?: Partial<T>
  }
  // 分页查询结果
  interface ApiPageResult<Data = Record<string, any>> {
    list: Data[]
    total: number
  }

  type GlobalApi = {
    env: <Key extends keyof ImportMetaEnv>(key: Key) => ImportMetaEnv[Key]
    cssVar: (name: string) => string
  }
  const $G: GlobalApi

  function $mockData<T = any>(fileName: ''): Promise<T>
  // 扩展全局类型
  interface Window {
    $G: GlobalApi
  }
}
export {}
