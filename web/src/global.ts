import { PREFIX } from '@/constants'
;(function () {
  window.$G = {
    env(key) {
      let v: any = import.meta.env[key]
      if (v === 'true' || v === 'false') return v === 'true'
      return v
    },
    cssVar(name) {
      return `var(--${PREFIX}-${name})`
    }
  }
  window.$mockData = async (fileName) => {
    let url = `/${$G.env('VITE_ROUTER_BASE')}/mock/${fileName}.json`
    let res = await fetch(url.replace(/\/+/g, '/'))
    return res.json()
  }
})()
