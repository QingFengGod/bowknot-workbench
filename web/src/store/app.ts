import type { ThemeConfig } from 'antdv-next'
import { defineStore } from 'pinia'
type AppStoreState = {
  themeToken: ThemeConfig['token']
}

export const useAppStore = defineStore('app', {
  state(): AppStoreState {
    return {
      themeToken: {
        colorPrimary: '#1AA069'
      }
    }
  }
})
