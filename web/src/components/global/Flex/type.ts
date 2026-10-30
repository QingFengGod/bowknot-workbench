import type { CSSProperties } from 'vue'
export type FlexProps = {
  inline?: boolean
  align?: CSSProperties['alignItems']
  justify?: CSSProperties['justifyContent']
  rowGap?: CSSProperties['rowGap']
  colGap?: CSSProperties['columnGap']
  wrap?: boolean
  vertical?: boolean
  reverse?: boolean
}
