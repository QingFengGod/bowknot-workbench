import type { IconName } from '@/components/global'

export type NavItem = {
  title: string
  iconName: IconName
  key: string
}

export type LayoutState = {
  activeNavKey: string
  navCollapsed: boolean
}
