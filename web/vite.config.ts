import { defineConfig, loadEnv, normalizePath } from 'vite'
import Vue from '@vitejs/plugin-vue'
import { join } from 'node:path'
import Unocss from 'unocss/vite'

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, join(process.cwd(), 'env'))
  return {
    plugins: [Vue(), Unocss()],
    envDir: join(import.meta.dirname, 'env'),
    resolve: {
      alias: {
        '@': join(import.meta.dirname, './src')
      }
    },
    server: {
      host: '0.0.0.0',
      proxy: {
        [env.VITE_API_BASEURL]: {
          target: 'http://127.0.0.1:8086',
          changeOrigin: true,
          rewrite: (path) => path.replace(new RegExp(`^${env.VITE_API_BASEURL}`), '')
        },
        [env.VITE_OSS_FILE_BASE_URL]: {
          target: 'http://127.0.0.1:9000',
          changeOrigin: true,
          rewrite: (path) => {
            return path.replace(new RegExp(`^${env.VITE_OSS_FILE_BASE_URL}`), '')
          }
        }
      }
    },
    base: env.VITE_ROUTER_BASE,
    build: {
      sourcemap: false,
      modulePreload: false,
      rolldownOptions: {
        output: {
          chunkFileNames: 'js/[name]-[hash].js',
          entryFileNames: 'js/[name]-[hash].js',
          assetFileNames: '[ext]/[name]-[hash].[ext]',
          codeSplitting: {
            groups: [
              {
                name(moduleId: string) {
                  moduleId = normalizePath(moduleId)
                  if (!moduleId.includes('node_modules')) return null
                  let idx = moduleId.lastIndexOf('node_modules')
                  const [_, libName, libName2] = moduleId.slice(idx).split(/[/]/)
                  if (libName.startsWith('@')) {
                    return libName.slice(1) + libName2
                  }
                  return libName
                }
              }
            ]
          }
        }
      }
    }
  }
})
