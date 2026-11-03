import type { ThemeConfig } from 'antdv-next'
import { defineStore } from 'pinia'

export type ThemeMode = 'light' | 'dark' | 'system'

type AppStoreState = {
  lightToken: ThemeConfig['token']
  darkToken: ThemeConfig['token']
  themeMode: ThemeMode
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
        colorPrimary: '#FF4500',
        colorSplit: '#ddd',
        boxShadow: '0 0 6px rgba(0, 0, 0, 0.2)'
      },
      darkToken: {
        colorPrimary: '#FF4500',
        colorSplit: '#424242',
        boxShadow: '0 0 6px rgba(0, 0, 0, 0.45)'
      },
      themeMode: getSavedThemeMode()
    }
  },
  getters: {
    isDark(state): boolean {
      if (state.themeMode === 'system') {
        return window.matchMedia('(prefers-color-scheme: dark)').matches
      }
      return state.themeMode === 'dark'
    }
  },
  actions: {
    setThemeMode(mode: ThemeMode) {
      this.themeMode = mode
      localStorage.setItem(THEME_STORAGE_KEY, mode)
    }
  }
})
