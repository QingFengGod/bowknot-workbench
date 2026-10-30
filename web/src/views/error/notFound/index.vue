<template>
  <div class="w-full h-full flex-center flex-col">
    <img :src="notFoundUrl" width="360"></img>
    <div class="text-primary text-66px font-bold">404</div>
    <div class="text-desc mb-xl">您访问的页面不存在或已被移除</div>
    <BkFlex>
      <ElButton type="primary" @click="$router.replace('/')">
        <IconifyIcon icon="prime:home"></IconifyIcon>
        返回首页
      </ElButton>
      <ElButton @click="$router.back()">
        <IconifyIcon icon="prime:angle-left"></IconifyIcon>
        返回上一页
      </ElButton>
    </BkFlex>
  </div>
</template>

<script setup lang="ts">
import notFoundUrl from '@/assets/images/404.png'
defineOptions({ name: 'notfound' })

</script>

<style scoped></style>
