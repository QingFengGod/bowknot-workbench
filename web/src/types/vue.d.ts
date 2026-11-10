declare module 'vue' {
  interface GlobalComponents {
    BkIcon: typeof import('@/components').Icon
    BkGrid: typeof import('@/components').Grid
    BkCell: typeof import('@/components').Cell
    BkFlex: typeof import('@/components').Flex
    BkConversations: typeof import('@/components').Conversations
  }

  interface GlobalDirectives {
    vResize: import('@/common/directives').ResizeDirective
  }

  interface ComponentCustomProperties {
    $G: GlobalApi
    $className(name: string): string
  }
}

export {}
