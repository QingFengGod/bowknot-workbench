import type { Plugin } from 'vue'
import Icon from './Icon/Icon.vue'
import Grid from './Grid/Grid.vue'
import Cell from './Grid/Cell.vue'
import Flex from './Flex/Flex.vue'
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
