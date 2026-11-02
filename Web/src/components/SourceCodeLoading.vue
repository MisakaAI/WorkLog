<script setup>
const logoURL = `${import.meta.env.BASE_URL}favicon.svg`
</script>

<template>
  <div class="startup-loader source-code-loading" role="status" aria-live="polite" aria-atomic="true">
    <div>
      <div class="startup-loader__dial" aria-hidden="true">
        <svg class="startup-loader__ring" viewBox="0 0 112 112" aria-hidden="true">
          <circle class="startup-loader__arc" cx="56" cy="56" r="54" pathLength="100" transform="rotate(-90 56 56)" />
        </svg>
        <img class="startup-loader__logo" :src="logoURL" width="80" height="80" alt="" />
      </div>
      <p class="startup-loader__message">正在加载源码编辑器…</p>
    </div>
  </div>
</template>

<style scoped>
.source-code-loading {
  height: min(440px, 55vh);
  min-height: 200px;
  background: transparent;
}
.source-code-loading .startup-loader__dial { margin: 0 auto 24px; }
</style>
