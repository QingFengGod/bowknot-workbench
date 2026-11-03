import { defineConfig, presetWind3, transformerDirectives } from 'unocss'
import { presetAntd } from '@antdv-next/unocss'
import { PREFIX } from './src/constants'

export default defineConfig({
  presets: [
    presetWind3({ dark: 'class', preflight: 'on-demand' }),
    presetAntd({
      allowUnprefixed: false,
      prefix: PREFIX,
      antPrefix: PREFIX,
      tokenPrefix: PREFIX
    })
  ],
  transformers: [transformerDirectives()],
  shortcuts: {
    'flex-center': 'flex items-center justify-center',
    'page-container': 'w-full h-full bk-bg-container rd-10px b-all-1 bk-p-sm'
  },
  rules: [
    [
      /^b-(x|y|l|r|t|b|all)(?:-(\d+(?:\.\d+)?))?(?:-(.+))?$/,
      ([, dir, width = '1', rest], { theme }: any) => {
        let colorKey = 'split'
        let style = 'solid'

        if (rest) {
          const parts: string[] = rest.split('-')
          const last = parts[parts.length - 1]
          if (['solid', 'dashed', 'dotted'].includes(last)) {
            style = parts.pop()!
          }
          if (parts.length) {
            colorKey = parts.join('-')
          }
        }

        const token = theme.colors?.[colorKey]
        const color = typeof token === 'string' ? token : colorKey
        const v = `${width}px ${color} ${style}`
        if (dir === 'x') return { 'border-left': v, 'border-right': v }
        if (dir === 'y') return { 'border-top': v, 'border-bottom': v }
        if (dir === 'l') return { 'border-left': v }
        if (dir === 'r') return { 'border-right': v }
        if (dir === 't') return { 'border-top': v }
        if (dir === 'b') return { 'border-bottom': v }
        return { border: v }
      }
    ]
  ]
})
