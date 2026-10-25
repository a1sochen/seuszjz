<script setup lang="ts">
import { computed, ref } from "vue";
import {
  bookingWindow,
  clockOf,
  formatDateTime,
  nextOpening,
  state,
  type BookingWindow,
  type FilterKey,
  type WindowTone,
} from "../../store";
import type { Lecture } from "../../../bindings/seuszjz/internal/seu/models.js";
import StatusBadge from "../StatusBadge.vue";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { CalendarDays, Clock, Search } from "@lucide/vue";

const filters: { key: FilterKey; label: string }[] = [
  { key: "all", label: "全部状态" },
  { key: "available", label: "可预约" },
  { key: "not_open", label: "未开放" },
  { key: "full", label: "已满" },
  { key: "joined", label: "已预约" },
];

const filter = computed({
  get: () => state.filter as string,
  set: (value: string) => {
    state.filter = value as FilterKey;
  },
});

const filtered = computed(() => {
  const keyword = state.keyword.trim().toLowerCase();
  return state.lectures.filter((lecture) => {
    if (state.filter === "joined") {
      if (!lecture.joined) return false;
    } else if (state.filter !== "all" && lecture.status !== state.filter) {
      return false;
    }
    if (!keyword) return true;
    return [lecture.name, lecture.speaker, lecture.venue, lecture.series, lecture.category]
      .filter(Boolean)
      .some((field) => field.toLowerCase().includes(keyword));
  });
});

const joinedCount = computed(() => state.lectures.filter((lecture) => lecture.joined).length);

// 最近一场还没开放预约的讲座, 提示在工具栏右侧, 免得错过开约时间。
const opening = computed(() => nextOpening(state.lectures));

function windowOf(lecture: Lecture): BookingWindow {
  return bookingWindow(lecture);
}

// toneClass 用单色主题里的三档对比度表达紧急程度, 不引入彩色。
function toneClass(tone: WindowTone): string {
  switch (tone) {
    case "hot":
      return "font-medium text-destructive";
    case "idle":
    case "open":
      return "font-medium text-foreground";
    default:
      return "text-muted-foreground";
  }
}

const detailOpen = ref(false);
const detail = ref<Lecture | null>(null);

function openDetail(lecture: Lecture): void {
  detail.value = lecture;
  detailOpen.value = true;
}

function canGrab(lecture: Lecture): boolean {
  return lecture.status === "available" || lecture.status === "not_open";
}



function grab(lecture: Lecture): void {
  detailOpen.value = false;
  state.grabTarget = lecture.wid;
  state.view = "grab";
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="flex shrink-0 flex-wrap items-center gap-2 border-b px-4 py-3">
      <div class="relative w-full max-w-xs">
        <Search class="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          v-model="state.keyword"
          class="h-8 pl-8 text-xs"
          placeholder="搜索讲座、主讲人、地点"
        />
      </div>

      <Select v-model="filter">
        <SelectTrigger class="h-8 w-[124px] text-xs" size="sm">
          <SelectValue placeholder="全部状态" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem v-for="item in filters" :key="item.key" :value="item.key">
            {{ item.label }}
          </SelectItem>
        </SelectContent>
      </Select>

      <p class="ml-auto text-[11px] text-muted-foreground tabular-nums">
        共 {{ state.lectures.length }} 讲 · 已约 {{ joinedCount }}
        <template v-if="opening"> · 最近开放 {{ opening }}</template>
        <template v-if="state.fetchedAt"> · {{ clockOf(state.fetchedAt) }} 更新</template>
      </p>
    </div>

    <ScrollArea class="min-h-0 flex-1">
      <div v-if="state.loadingList && !state.lectures.length" class="flex items-center justify-center gap-2 py-24 text-xs text-muted-foreground">
        <Spinner class="size-3.5" />
        正在加载讲座列表…
      </div>

      <div v-else-if="!filtered.length" class="p-6">
        <Empty class="border border-dashed">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <CalendarDays />
            </EmptyMedia>
            <EmptyTitle>{{ state.lectures.length ? "没有符合条件的讲座" : "暂无讲座" }}</EmptyTitle>
            <EmptyDescription>
              {{ state.lectures.length ? "试试换个筛选条件或清空关键词。" : "教务还没有发布可预约的讲座，稍后刷新看看。" }}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>

      <div v-else class="space-y-2 p-4">
        <article
          v-for="lecture in filtered"
          :key="lecture.wid"
          class="flex flex-col gap-3 rounded-lg border bg-card p-4 transition-colors hover:border-foreground/25 sm:flex-row sm:items-center sm:gap-6"
        >
          <div class="min-w-0 flex-1 space-y-1.5">
            <div class="flex flex-wrap items-center gap-2">
              <StatusBadge :status="lecture.status" />
              <span class="text-[11px] text-muted-foreground">{{ lecture.series || "素质讲座" }}</span>
              <span v-if="lecture.category" class="text-[11px] text-muted-foreground">/ {{ lecture.category }}</span>
            </div>
            <h2 class="truncate text-sm font-medium">{{ lecture.name }}</h2>
            <p class="flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-muted-foreground">
              <span>{{ lecture.speaker || "待定" }}</span>
              <span>·</span>
              <span>{{ lecture.venue || "地点待定" }}</span>
              <span>·</span>
              <span class="tabular-nums">讲座 {{ formatDateTime(lecture.startAt) }}</span>
            </p>
            <p class="flex items-center gap-1.5 text-[11px] tabular-nums">
              <Clock class="size-3 shrink-0" :class="toneClass(windowOf(lecture).tone)" />
              <span :class="toneClass(windowOf(lecture).tone)">{{ windowOf(lecture).text }}</span>
            </p>
          </div>

          <div class="flex shrink-0 items-center gap-3">
            <div class="text-right">
              <p class="text-xs font-medium tabular-nums">{{ lecture.booked }} / {{ lecture.capacity }}</p>
              <p class="text-[11px] text-muted-foreground">已约 / 名额</p>
            </div>
            <Button variant="outline" size="sm" @click="openDetail(lecture)">详情</Button>
            <Button size="sm" :disabled="!canGrab(lecture)" @click="grab(lecture)">抢座</Button>
          </div>
        </article>
      </div>
    </ScrollArea>

    <Dialog v-model:open="detailOpen">
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle class="pr-8 text-base leading-snug">{{ detail?.name }}</DialogTitle>
          <DialogDescription class="flex flex-wrap items-center gap-2 text-xs">
            <StatusBadge v-if="detail" :status="detail.status" />
            <span>{{ detail?.series }}</span>
            <span v-if="detail?.category">/ {{ detail.category }}</span>
          </DialogDescription>
        </DialogHeader>

        <p v-if="detail" class="flex items-center gap-1.5 text-xs tabular-nums">
          <Clock class="size-3.5 shrink-0" :class="toneClass(windowOf(detail).tone)" />
          <span :class="toneClass(windowOf(detail).tone)">{{ windowOf(detail).text }}</span>
        </p>

        <dl v-if="detail" class="grid grid-cols-1 gap-3 text-xs sm:grid-cols-2">
          <div class="space-y-0.5">
            <dt class="text-muted-foreground">主讲人</dt>
            <dd class="font-medium">{{ detail.speaker || "待定" }}</dd>
          </div>
          <div class="space-y-0.5">
            <dt class="text-muted-foreground">地点</dt>
            <dd class="font-medium">{{ detail.venue || "待定" }}</dd>
          </div>
          <div class="space-y-0.5">
            <dt class="text-muted-foreground">讲座时间</dt>
            <dd class="font-medium tabular-nums">
              {{ formatDateTime(detail.startAt) }}<span v-if="clockOf(detail.endAt)"> - {{ clockOf(detail.endAt) }}</span>
            </dd>
          </div>
          <div class="space-y-0.5">
            <dt class="text-muted-foreground">预约开放</dt>
            <dd class="font-medium tabular-nums">{{ formatDateTime(detail.bookStartAt) || "—" }}</dd>
          </div>
          <div class="space-y-0.5">
            <dt class="text-muted-foreground">预约截止</dt>
            <dd class="font-medium tabular-nums">{{ formatDateTime(detail.bookEndAt) || "—" }}</dd>
          </div>
          <div class="space-y-0.5">
            <dt class="text-muted-foreground">名额</dt>
            <dd class="font-medium tabular-nums">{{ detail.booked }} / {{ detail.capacity }}</dd>
          </div>
          <div class="space-y-0.5">
            <dt class="text-muted-foreground">主办</dt>
            <dd class="font-medium">{{ detail.organizer || "—" }}</dd>
          </div>
        </dl>

        <div v-if="detail?.intro" class="max-h-48 overflow-auto rounded-md border bg-muted/40 p-3">
          <p class="text-xs leading-relaxed whitespace-pre-line text-muted-foreground">{{ detail.intro }}</p>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="detailOpen = false">关闭</Button>
          <Button :disabled="!detail || !canGrab(detail)" @click="detail && grab(detail)">去抢座</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
