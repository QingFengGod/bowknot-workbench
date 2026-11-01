import { defineConfig, presetWind3 } from 'unocss'
import { presetAntd } from '@antdv-next/unocss'
import { PREFIX } from './src/constants'

export default defineConfig({
  presets: [
    presetWind3({ dark: 'class', preflight: false }),
    presetAntd({
      allowUnprefixed: false,
      prefix: PREFIX,
      antPrefix: PREFIX,
      tokenPrefix: PREFIX
    })
  ]
})
