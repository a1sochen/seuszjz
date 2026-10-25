<script setup lang="ts">
import { computed } from "vue";
import { lectureStatusText, statusTone, type StatusTone } from "../store";

const props = defineProps<{ status: string }>();

// 六种状态的配色: 底/字/边同色系, token 定义在 style.css 的 :root 与 .dark。
// 类名必须写成字面量, Tailwind 才能在构建时扫到。
const toneClasses: Record<StatusTone, string> = {
  open: "border-status-open-border bg-status-open text-status-open-fg",
  full: "border-status-full-border bg-status-full text-status-full-fg",
  locked: "border-status-locked-border bg-status-locked text-status-locked-fg",
  joined: "border-status-joined-border bg-status-joined text-status-joined-fg",
  muted: "border-status-muted-border bg-status-muted text-status-muted-fg",
  bad: "border-status-bad-border bg-status-bad text-status-bad-fg",
};

const tone = computed(() => statusTone(props.status));
const label = computed(() => lectureStatusText(props.status));
</script>

<template>
  <span
    class="inline-flex w-fit shrink-0 items-center rounded-md border px-2 py-0.5 text-xs font-medium whitespace-nowrap"
    :class="toneClasses[tone]"
  >
    {{ label }}
  </span>
</template>
