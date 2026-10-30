import 'uno.css'
import './global'
import { createApp } from 'vue'
import App from './App.vue'
import { globalComponents } from '@/components'
import { getStore } from '@/store'
import { getRouter } from '@/router'
import '@/styles/index.css'
import { globalDirectives } from './directives'

async function bootstrap() {
  const app = createApp(App).use(globalComponents()).use(globalDirectives()).use(getRouter()).use(getStore())

  app.config.errorHandler = (err) => {
    console.error(err)
  }
  app.mount('#app')
  app.config.globalProperties.$G = window.$G
}

bootstrap()
