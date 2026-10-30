import { defineConfig } from 'unocss'
import { presetAntd } from '@antdv-next/unocss'
import { PREFIX } from './src/constants'

export default defineConfig({
  presets: [
    presetAntd({
      allowUnprefixed: false,
      prefix: PREFIX,
      antPrefix: PREFIX,
      tokenPrefix: PREFIX
    })
  ]
})
