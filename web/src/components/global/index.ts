import type { Plugin } from 'vue'
import Icon from './Icon/Icon.vue'
export * from './Icon/type'
import Grid from './Grid/Grid.vue'
import Cell from './Grid/Cell.vue'
export * from './Grid/type'
import Flex from './Flex/Flex.vue'
export * from './Flex/type'

import { Avatar, Button, Menu, Popover, Tooltip } from 'antdv-next'

export function globalComponents(): Plugin {
  return {
    install(app) {
      app.component('BkIcon', Icon)
      app.component(`BkGrid`, Grid)
      app.component(`BkCell`, Cell)
      app.component(`BkFlex`, Flex)
      app.component(Button.name, Button)
      app.component(Avatar.name, Avatar)
      app.component(Menu.name, Menu)
      app.component(Popover.name, Popover)
      app.component(Tooltip.name, Tooltip)
    }
  }
}

export { Grid, Cell, Flex, Icon }
