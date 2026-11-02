<template>
  <div class="w-full h-full relative flex">
    <Transition name="transition-offset-top" appear>
      <div class="h-full w-80px bk-bg-container b-r-1 bk-shadow-ter">
        <BkFlex class="text-primary py-sm b-b-1" justify="center" align="center">
          <BkIcon name="logo-bow" size="28px"></BkIcon>
        </BkFlex>
        <div class="bk-py-xxs">
          <div class="flex flex-col items-center bk-py-sm bg-primary text-white">
            <BkIcon name="prime-cog"></BkIcon>
            <div>AppStore</div>
          </div>
          <div class="flex flex-col items-center bk-py-sm hover:bk-bg-fill-content">
            <BkIcon name="prime-cog"></BkIcon>
            <div>AppStore</div>
          </div>
        </div>
      </div>
      <!-- <BkGrid
        height="50px"
        class="absolute top-0 bk-bg-container bk-shadow-ter bk-px-sm"
        tmpl-cols="240px 1fr 220px"
        tmpl-rows="1fr"
      >
        <BkCell area="1/1/2/2" flex align="center" justify="center" class="text-primary">
          <BkIcon name="logo-bow" size="28px"></BkIcon>
          <span class="font-bold text-18px">Bowknot Workbench</span>
        </BkCell>
        <BkCell area="1/2/2/3" flex align="center" justify="center"> sad </BkCell>
        <BkCell area="1/3/2/4" flex align="center" justify="flex-end">
          <BkIcon name="moon-filled-loop"></BkIcon>
          <AAvatar class="bk-bg-primary">未登录</AAvatar>
        </BkCell>
      </BkGrid> -->
    </Transition>
    <!-- <BkFlex class="h-full p-20px scroll"> </BkFlex> -->
  </div>
</template>

<script setup lang="ts">
defineOptions({ name: 'Layout' })

// const headerBarStyle: CSSProperties = {
//   borderBottom: `solid 1px ${$G.cssVar('color-border')}`
// }
</script>

<style scoped></style>
