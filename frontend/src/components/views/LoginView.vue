<script setup lang="ts">
import { computed, ref } from "vue";
import { login, saveSettings, state, toast } from "../../store";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Moon, Sun } from "@lucide/vue";
import BrandMark from "../BrandMark.vue";

const username = ref(state.settings?.username ?? "");
const password = ref("");
const remember = ref(state.settings?.remember ?? true);

const light = computed(() => state.settings?.theme === "light");

async function submit(): Promise<void> {
  if (!username.value.trim() || !password.value) {
    toast("err", "请填写一卡通号与密码");
    return;
  }
  const ok = await login(username.value, password.value, remember.value);
  if (ok) password.value = "";
}

function toggleTheme(): void {
  void saveSettings({ theme: light.value ? "dark" : "light" });
}
</script>

<template>
  <div class="relative grid h-full place-items-center overflow-auto bg-background p-6">
    <div
      class="pointer-events-none absolute inset-0 opacity-[0.05] [background-image:radial-gradient(circle_at_1px_1px,var(--foreground)_1px,transparent_0)] [background-size:22px_22px]"
    />

    <Button variant="ghost" size="icon" class="absolute top-4 right-4" title="切换外观" @click="toggleTheme">
      <Sun v-if="light" />
      <Moon v-else />
    </Button>

    <div class="relative w-full max-w-sm">
      <div class="mb-6 flex flex-col items-center gap-3 text-center">
        <BrandMark class="size-11" />
        <div class="space-y-1">
          <h1 class="text-lg font-semibold tracking-tight">东南大学讲座助手</h1>
          <p class="text-xs text-muted-foreground">登录统一身份认证，开始预约素质讲座</p>
        </div>
      </div>

      <Card class="gap-0 py-0">
        <CardHeader class="gap-1 px-5 pt-5 pb-0">
          <CardTitle class="text-sm">账号登录</CardTitle>
          <CardDescription class="text-xs">使用一卡通号与统一身份认证密码</CardDescription>
        </CardHeader>

        <CardContent class="px-5 py-4">
          <form class="space-y-4" @submit.prevent="submit">
            <FieldGroup class="gap-4">
              <Field>
                <FieldLabel for="username">一卡通号</FieldLabel>
                <Input
                  id="username"
                  v-model="username"
                  autocomplete="username"
                  placeholder="例如 09012345"
                  :disabled="state.loggingIn"
                />
              </Field>

              <Field>
                <FieldLabel for="password">密码</FieldLabel>
                <Input
                  id="password"
                  v-model="password"
                  type="password"
                  autocomplete="current-password"
                  placeholder="统一身份认证密码"
                  :disabled="state.loggingIn"
                />
              </Field>

              <Field orientation="horizontal" class="items-center justify-between rounded-md border px-3 py-2">
                <div class="space-y-0.5">
                  <FieldLabel for="remember" class="text-xs font-normal">记住登录信息</FieldLabel>
                  <FieldDescription class="text-[11px]">存于浏览器 localStorage，可随时清除</FieldDescription>
                </div>
                <Switch id="remember" v-model="remember" :disabled="state.loggingIn" />
              </Field>
            </FieldGroup>

            <p v-if="state.loginError" class="text-xs text-destructive">{{ state.loginError }}</p>
            <p v-else-if="state.loggingIn" class="flex items-center gap-2 text-xs text-muted-foreground">
              <Spinner class="size-3.5" />
              {{ state.loginProgress || "正在登录…" }}
            </p>

            <Button type="submit" class="w-full" :disabled="state.loggingIn">
              <Spinner v-if="state.loggingIn" />
              {{ state.loggingIn ? "登录中" : "登录" }}
            </Button>
          </form>
        </CardContent>

        <CardFooter class="border-t bg-muted/40 px-5 py-3">
          <p class="text-[11px] leading-relaxed text-muted-foreground">
            账号密码只保存在浏览器本地存储（localStorage），密码做 AES-GCM 混淆，程序目录里不落文件。
          </p>
        </CardFooter>
      </Card>
    </div>
  </div>
</template>
