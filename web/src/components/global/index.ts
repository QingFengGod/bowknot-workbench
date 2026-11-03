import type { Plugin } from 'vue'
import Icon from './Icon/Icon.vue'
export * from './Icon/type'
import Grid from './Grid/Grid.vue'
export * from './Grid/type'
import Cell from './Grid/Cell.vue'
import Flex from './Flex/Flex.vue'
export * from './Flex/type'
import { Avatar, Button } from 'antdv-next'

export function globalComponents(): Plugin {
  return {
    install(app) {
      app.component('BkIcon', Icon)
      app.component(`BkGrid`, Grid)
      app.component(`BkCell`, Cell)
      app.component(`BkFlex`, Flex)
      app.component(Button.name, Button)
      app.component(Avatar.name, Avatar)
    }
  }
}

export { Grid, Cell, Flex, Icon }
