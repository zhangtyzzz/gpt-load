<script setup lang="ts">
// Monochrome brand mark: ink rounded square with an ascending load line.
// Inline SVG so currentColor follows the theme.
withDefaults(defineProps<{ size?: number }>(), { size: 32 });
</script>

<template>
  <svg :width="size" :height="size" viewBox="0 0 32 32" fill="none" aria-hidden="true">
    <rect width="32" height="32" rx="7" fill="currentColor" />
    <path
      d="M8.5 20.5l4.6-5.6 3.8 3.6 6.6-8.2"
      stroke="#ffffff"
      stroke-width="2.4"
      stroke-linecap="round"
      stroke-linejoin="round"
    />
  </svg>
</template>
