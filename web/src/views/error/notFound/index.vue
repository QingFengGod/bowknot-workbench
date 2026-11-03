<template>
  <div class="w-full h-full flex-center flex-col">
    <img :src="notFoundUrl" width="360"></img>
    <div class="text-primary text-66px font-bold">404</div>
    <div class="text-sec mb-xl">您访问的页面不存在或已被移除</div>
    <BkFlex>
      <AButton type="primary" @click="$router.replace('/')">
        <template #icon><BkIcon name="prime-home" /></template>
        返回首页
      </AButton>
      <AButton @click="$router.back()">
        <template #icon><BkIcon name="prime-chevron-right" rotate="180deg" /></template>
        返回上一页
      </AButton>
    </BkFlex>
  </div>
</template>

<script setup lang="ts">
import notFoundUrl from '@/assets/imgs/404.png'
defineOptions({ name: 'NotFoundPage' })

</script>

<style scoped></style>
