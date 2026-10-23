<script setup lang="ts">
/**
 * 应用标志：东南大学校标（官方矢量图，彩色版）。
 *
 * 尺寸由外部 class 控制（如 size-8 / size-11）；彩色校标在深浅两种主题下都能看清，
 * 所以不再随主题反色。
 */
import logoUrl from "../assets/seu-logo.svg";

withDefaults(defineProps<{ title?: string }>(), { title: "东南大学讲座助手" });
</script>

<template>
  <img
    :src="logoUrl"
    :alt="title"
    draggable="false"
    class="block shrink-0 select-none"
  />
</template>
