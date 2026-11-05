// 图标名称与 src/styles/icons.css 中的 .icon-* 类一一对应

type IconSets = {
  prime: 'home' | 'th-large' | 'chevron-right' | 'cog' | 'gift' | 'share-alt' | 'sparkles' | 'desktop' | 'ellipsis-v'
  'game-icons': 'bow-tie-ribbon'
  'line-md': 'moon-loop' | 'sunny-loop' | 'computer' | 'menu-fold-left'
  'park-outline': 'workbench'
  'griddy-icons': 'ai-assistant'
}
type IconSet = keyof IconSets
export type IconName = {
  [k in IconSet]: `${k}-${IconSets[k]}`
}[IconSet]

export type IconProps = {
  name: IconName
  color?: string
  size?: string
  rotate?: number
}
