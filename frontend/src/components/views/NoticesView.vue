<script setup lang="ts">
import { computed, ref } from "vue";
import { clockOf, formatDateTime, state } from "../../store";
import { Badge } from "@/components/ui/badge";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Spinner } from "@/components/ui/spinner";
import { Bell, ChevronDown } from "@lucide/vue";

const notices = computed(() => state.notices);
const open = ref<string | null>(null);

function toggle(wid: string, value: boolean): void {
  open.value = value ? wid : null;
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="flex shrink-0 items-center gap-2 border-b px-4 py-3">
      <p class="text-[11px] text-muted-foreground tabular-nums">
        共 {{ notices.length }} 条通知
        <template v-if="state.fetchedAt"> · {{ clockOf(state.fetchedAt) }} 更新</template>
      </p>
    </div>

    <ScrollArea class="min-h-0 flex-1">
      <div v-if="state.loadingList && !notices.length" class="flex items-center justify-center gap-2 py-24 text-xs text-muted-foreground">
        <Spinner class="size-3.5" />
        正在加载通知…
      </div>

      <div v-else-if="!notices.length" class="p-6">
        <Empty class="border border-dashed">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Bell />
            </EmptyMedia>
            <EmptyTitle>暂无通知</EmptyTitle>
            <EmptyDescription>教务发布通知后会在这里出现。</EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>

      <div v-else class="space-y-2 p-4">
        <Collapsible
          v-for="notice in notices"
          :key="notice.wid"
          :open="open === notice.wid"
          class="rounded-lg border bg-card"
          @update:open="(value) => toggle(notice.wid, value)"
        >
          <CollapsibleTrigger class="flex w-full items-start gap-3 px-4 py-3 text-left">
            <div class="min-w-0 flex-1 space-y-1">
              <div class="flex flex-wrap items-center gap-2">
                <Badge v-if="notice.top" variant="secondary">置顶</Badge>
                <span class="truncate text-sm font-medium">{{ notice.title }}</span>
              </div>
              <p class="text-[11px] text-muted-foreground tabular-nums">{{ formatDateTime(notice.date) }}</p>
            </div>
            <ChevronDown
              class="mt-0.5 size-4 shrink-0 text-muted-foreground transition-transform duration-200"
              :class="open === notice.wid ? 'rotate-180' : ''"
            />
          </CollapsibleTrigger>

          <CollapsibleContent>
            <div class="border-t px-4 py-3">
              <p v-if="notice.body" class="text-xs leading-relaxed whitespace-pre-line text-muted-foreground">
                {{ notice.body }}
              </p>
              <p v-else class="text-xs text-muted-foreground">这条通知没有正文。</p>
            </div>
          </CollapsibleContent>
        </Collapsible>
      </div>
    </ScrollArea>
  </div>
</template>
