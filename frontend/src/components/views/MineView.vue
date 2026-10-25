<script setup lang="ts">
import { computed } from "vue";
import { cancel, check, clockOf, formatDateTime, isBusy, state } from "../../store";
import StatusBadge from "../StatusBadge.vue";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Spinner } from "@/components/ui/spinner";
import { Bookmark, CircleCheck, X } from "@lucide/vue";

const records = computed(() => state.mine);



function canCancel(status: string): boolean {
  return status !== "checked_in" && status !== "cancelled";
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="flex shrink-0 items-center gap-2 border-b px-4 py-3">
      <p class="text-[11px] text-muted-foreground tabular-nums">
        共 {{ records.length }} 条预约记录
        <template v-if="state.fetchedAt"> · {{ clockOf(state.fetchedAt) }} 更新</template>
      </p>
    </div>

    <ScrollArea class="min-h-0 flex-1">
      <div v-if="state.loadingList && !records.length" class="flex items-center justify-center gap-2 py-24 text-xs text-muted-foreground">
        <Spinner class="size-3.5" />
        正在加载预约记录…
      </div>

      <div v-else-if="!records.length" class="p-6">
        <Empty class="border border-dashed">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Bookmark />
            </EmptyMedia>
            <EmptyTitle>还没有预约记录</EmptyTitle>
            <EmptyDescription>去「讲座」页挑一讲，预约成功后会出现在这里。</EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>

      <div v-else class="space-y-2 p-4">
        <article
          v-for="record in records"
          :key="record.recordWid || record.wid"
          class="flex flex-col gap-3 rounded-lg border bg-card p-4 sm:flex-row sm:items-center sm:gap-6"
        >
          <div class="min-w-0 flex-1 space-y-1.5">
            <div class="flex flex-wrap items-center gap-2">
              <StatusBadge :status="record.status" />
              <span class="text-[11px] text-muted-foreground">{{ record.series || "素质讲座" }}</span>
            </div>
            <h2 class="truncate text-sm font-medium">{{ record.name }}</h2>
            <p class="flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-muted-foreground">
              <span>{{ record.speaker || "待定" }}</span>
              <span>·</span>
              <span>{{ record.venue || "地点待定" }}</span>
              <span>·</span>
              <span class="tabular-nums">{{ formatDateTime(record.startAt) }}</span>
            </p>
            <p class="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
              <span v-if="record.bookedAt" class="tabular-nums">预约于 {{ formatDateTime(record.bookedAt) }}</span>
              <span v-if="record.code" class="tabular-nums">
                现场码 <span class="font-mono text-foreground">{{ record.code }}</span>
              </span>
              <span v-if="record.checkedIn" class="inline-flex items-center gap-1">
                <CircleCheck class="size-3" />
                已打卡{{ record.checkInAt ? ` ${formatDateTime(record.checkInAt)}` : "" }}
              </span>
            </p>
          </div>

          <div class="flex shrink-0 items-center gap-2">
            <Button
              v-if="!record.checkedIn && canCancel(record.status)"
              variant="outline"
              size="sm"
              :disabled="isBusy('check-' + record.wid)"
              @click="check(record.wid)"
            >
              <Spinner v-if="isBusy('check-' + record.wid)" />
              校验
            </Button>
            <Button
              v-if="canCancel(record.status)"
              variant="ghost"
              size="sm"
              :disabled="isBusy('cancel-' + record.wid)"
              @click="cancel(record.wid)"
            >
              <Spinner v-if="isBusy('cancel-' + record.wid)" />
              <X v-else />
              取消
            </Button>
          </div>
        </article>
      </div>
    </ScrollArea>
  </div>
</template>
