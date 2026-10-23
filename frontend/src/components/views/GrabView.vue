<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import {
  clockOf,
  formatDateTime,
  refreshGrabCaptcha,
  saveSettings,
  startGrab,
  state,
  stopGrab,
  submitGrabCode,
} from "../../store";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Play, RefreshCw, Square, Zap } from "@lucide/vue";

const settings = state.settings;

const selected = computed({
  get: () => state.grab?.wid || state.grabTarget || "",
  set: (value: string) => {
    state.grabTarget = value;
  },
});

const interval = ref(settings?.interval ?? 3);
const cooldown = ref(settings?.cooldown ?? 300);
const autoCaptcha = ref(settings?.autoCaptcha ?? true);

const grabbing = computed(() => !!state.grab?.running);
const captcha = computed(() => state.grab?.captcha ?? "");
const code = ref("");
const submitting = ref(false);

const logs = computed(() => state.grab?.logs ?? []);
const logBox = ref<HTMLElement | null>(null);

watch(
  () => logs.value.length,
  async () => {
    await nextTick();
    const el = logBox.value;
    if (el) el.scrollTop = el.scrollHeight;
  },
);

function begin(): void {
  if (!selected.value) return;
  void saveSettings({ interval: interval.value, cooldown: cooldown.value, autoCaptcha: autoCaptcha.value });
  startGrab(selected.value, interval.value, cooldown.value, autoCaptcha.value);
}

async function submit(): Promise<void> {
  if (!code.value.trim()) return;
  submitting.value = true;
  try {
    const ok = await submitGrabCode(code.value.trim());
    if (ok) code.value = "";
  } finally {
    submitting.value = false;
  }
}

function levelClass(level: string): string {
  switch (level) {
    case "error":
      return "text-destructive";
    case "warn":
      return "text-muted-foreground";
    case "ok":
      return "text-foreground";
    default:
      return "text-muted-foreground/80";
  }
}

const phaseText = computed(() => {
  const phase = state.grab?.phase ?? "";
  switch (phase) {
    case "idle":
      return "空闲";
    case "waiting":
      return "等待开放";
    case "running":
      return "抢座中";
    case "captcha":
      return "等待验证码";
    case "done":
      return "已完成";
    default:
      return phase || "空闲";
  }
});
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="shrink-0 space-y-3 border-b px-4 py-3">
      <div class="flex flex-wrap items-end gap-2">
        <div class="min-w-0 flex-1 space-y-1.5">
          <Label class="text-[11px] text-muted-foreground">抢座目标</Label>
          <Select v-model="selected" :disabled="grabbing">
            <SelectTrigger class="h-8 w-full text-xs" size="sm">
              <SelectValue placeholder="选择一讲讲座" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="lecture in state.lectures" :key="lecture.wid" :value="lecture.wid">
                {{ lecture.name }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <Button v-if="!grabbing" size="sm" :disabled="!selected" @click="begin">
          <Play />
          开始抢座
        </Button>
        <Button v-else variant="outline" size="sm" @click="stopGrab()">
          <Square />
          停止
        </Button>
      </div>

      <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div class="flex items-center gap-2">
          <Label for="interval" class="text-[11px] font-normal text-muted-foreground">间隔 (秒)</Label>
          <Input
            id="interval"
            v-model.number="interval"
            type="number"
            min="1"
            class="h-7 w-16 text-xs tabular-nums"
            :disabled="grabbing"
          />
        </div>

        <div class="flex items-center gap-2">
          <Label for="cooldown" class="text-[11px] font-normal text-muted-foreground">冷却 (秒)</Label>
          <Input
            id="cooldown"
            v-model.number="cooldown"
            type="number"
            min="10"
            class="h-7 w-20 text-xs tabular-nums"
            :disabled="grabbing"
          />
        </div>

        <Separator orientation="vertical" class="h-4!" />

        <label class="flex items-center gap-2 text-[11px] text-muted-foreground">
          <Switch v-model="autoCaptcha" :disabled="grabbing" />
          自动识别验证码
        </label>

        <Separator orientation="vertical" class="h-4!" />

        <p class="text-[11px] text-muted-foreground tabular-nums">
          尝试 {{ state.grab?.attempts ?? 0 }} 次 · 验证码 {{ state.grab?.captchaCount ?? 0 }} 张
        </p>
        <p v-if="state.grab?.startedAt" class="text-[11px] text-muted-foreground tabular-nums">
          始于 {{ formatDateTime(state.grab.startedAt) }}
        </p>
      </div>
    </div>

    <ScrollArea class="min-h-0 flex-1">
      <div class="space-y-4 p-4">
        <div class="flex items-start gap-3 rounded-lg border bg-card p-4">
          <div class="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md border bg-muted">
            <Spinner v-if="grabbing" class="size-3.5" />
            <Zap v-else class="size-3.5 text-muted-foreground" />
          </div>
          <div class="min-w-0 flex-1 space-y-1">
            <div class="flex flex-wrap items-center gap-2">
              <Badge :variant="state.grab?.success ? 'default' : 'outline'">{{ phaseText }}</Badge>
              <span class="truncate text-xs font-medium">{{ state.grab?.name || "尚未选择讲座" }}</span>
            </div>
            <p class="text-xs leading-relaxed text-muted-foreground">
              {{ state.grab?.message || "选择一讲讲座后点击开始，程序会按间隔轮询余量并自动提交预约。" }}
            </p>
          </div>
        </div>

        <div v-if="captcha" class="rounded-lg border bg-card p-4">
          <div class="mb-3 flex items-center justify-between">
            <p class="text-xs font-medium">需要手动输入验证码</p>
            <Button variant="ghost" size="sm" @click="refreshGrabCaptcha()">
              <RefreshCw />
              换一张
            </Button>
          </div>
          <div class="flex flex-wrap items-center gap-3">
            <img
              :src="captcha"
              alt="验证码"
              class="h-11 w-auto rounded-md border bg-white object-contain px-2"
            />
            <Input
              v-model="code"
              class="h-9 w-32 text-sm tracking-[0.3em] tabular-nums"
              placeholder="验证码"
              maxlength="8"
              @keyup.enter="submit"
            />
            <Button size="sm" :disabled="submitting || !code.trim()" @click="submit">
              <Spinner v-if="submitting" />
              提交
            </Button>
          </div>
        </div>

        <div class="rounded-lg border bg-card">
          <div class="flex items-center justify-between border-b px-4 py-2.5">
            <p class="text-xs font-medium">运行日志</p>
            <p class="text-[11px] text-muted-foreground tabular-nums">{{ logs.length }} 条</p>
          </div>
          <div ref="logBox" class="max-h-[420px] min-h-32 overflow-auto px-4 py-3">
            <p v-if="!logs.length" class="py-8 text-center text-[11px] text-muted-foreground">
              还没有日志，启动抢座后这里会实时输出进度。
            </p>
            <ul v-else class="space-y-1 font-mono text-[11px] leading-relaxed">
              <li v-for="(entry, index) in logs" :key="index" class="flex gap-2">
                <span class="shrink-0 text-muted-foreground/60 tabular-nums">{{ clockOf(entry.at) }}</span>
                <span :class="levelClass(entry.level)" class="break-all">{{ entry.text }}</span>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </ScrollArea>
  </div>
</template>
