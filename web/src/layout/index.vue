<template>
  <BkGrid tmpl-rows="40px 1fr" tmpl-cols="60px 1fr">
    <Transition name="transition-offset-top" appear>
      <BkCell area="1/1/2/3" class="bk-bg-primary"></BkCell>
    </Transition>
    <Transition name="transition-offset-top" appear>
      <BkCell area="2/1/3/2" class="bk-bg-warning"></BkCell>
    </Transition>
  </BkGrid>
</template>

<script setup lang="ts">
defineOptions({ name: 'Layout' })
</script>

<style scoped></style>
