import type { FlexProps } from './../Flex/type'
import type { CSSProperties } from 'vue'

export type GridProps = {
  inlineGrid?: boolean
  width?: CSSProperties['width']
  height?: CSSProperties['height']
  minWidth?: CSSProperties['minWidth']
  minHeight?: CSSProperties['minHeight']
  maxWidth?: CSSProperties['maxWidth']
  maxHeight?: CSSProperties['maxHeight']
  tmplRows?: CSSProperties['gridTemplateRows']
  tmplCols?: CSSProperties['gridTemplateColumns']
  tmplAreas?: CSSProperties['gridTemplateAreas']
  rowGap?: CSSProperties['rowGap']
  colGap?: CSSProperties['columnGap']
  autoFlow?: CSSProperties['gridAutoFlow']
  autoRows?: CSSProperties['gridAutoRows']
  autoColumns?: CSSProperties['gridAutoColumns']
  justifyItems?: CSSProperties['justifyItems']
  alignItems?: CSSProperties['alignItems']
  justifyContent?: CSSProperties['justifyContent']
  alignContent?: CSSProperties['alignContent']
}

export type CellProps = FlexProps & {
  area?: CSSProperties['gridArea']
  row?: CSSProperties['gridRow']
  col?: CSSProperties['gridColumn']
  flex?: boolean
  inlineFlex?: boolean
}
