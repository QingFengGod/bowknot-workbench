import type { IconName } from '@/components'
import type { ThemeConfig } from 'antdv-next'
import { defineStore, type _DeepPartial } from 'pinia'

export type ThemeMode = 'light' | 'dark' | 'system'
export type MenuItem = {
  label: string
  key: string
  iconName?: IconName
  children?: MenuItem[]
}

type AppStoreState = {
  lightToken: ThemeConfig['token']
  darkToken: Partial<ThemeConfig['token']>
  themeMode: ThemeMode
  menus: MenuItem[]
}

const THEME_STORAGE_KEY = 'bk-theme-mode'

function getSavedThemeMode(): ThemeMode {
  const saved = localStorage.getItem(THEME_STORAGE_KEY)
  return saved === 'light' || saved === 'dark' ? saved : 'system'
}

export const useAppStore = defineStore('app', {
  state(): AppStoreState {
    return {
      lightToken: {
        colorPrimary: '#fb5215',
        colorSplit: '#e3e6ec',
        boxShadow: '0 0 6px rgba(0, 0, 0, 0.2)'
      },
      darkToken: {
        colorSplit: '#414243',
        boxShadow: '0 0 6px rgba(0, 0, 0, 0.45)'
      },
      themeMode: getSavedThemeMode(),
      menus: [
        { label: '工作台', iconName: 'park-outline-workbench', key: '/workbench' },
        // { label: 'AppStore', iconName: 'prime-gift', key: '/app-store' },
        { label: 'Apps', iconName: 'prime-th-large', key: '/apps' },
        { label: '助手', iconName: 'griddy-icons-ai-assistant', key: '/assistant' },
        { label: 'agents', iconName: 'prime-share-alt', key: '/agents' },
        { label: 'AIGC', iconName: 'prime-sparkles', key: '/aigc' }
      ]
    }
  },
  getters: {
    isDark(state) {
      if (state.themeMode === 'system') {
        return window.matchMedia('(prefers-color-scheme: dark)').matches
      }
      return state.themeMode === 'dark'
    },
    themeToken(state) {
      if (this.isDark) return { ...state.lightToken, ...state.darkToken }
      return state.lightToken
    }
  },
  actions: {
    setThemeMode(mode: ThemeMode) {
      this.themeMode = mode
      localStorage.setItem(THEME_STORAGE_KEY, mode)
    }
  }
})
