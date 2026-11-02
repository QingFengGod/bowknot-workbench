// 图标名称与 src/styles/icons.css 中的 .icon-* 类一一对应
export type IconName = 'prime-cog' | 'logo-bow' | 'moon-filled-loop'

export type IconProps = {
  name: IconName
  color?: string
  size?: string
  padding?: string
  rotate?: string
}
