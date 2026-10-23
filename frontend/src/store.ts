import { reactive } from "vue";
import { toast as showToast } from "vue-sonner";
import { Events } from "@wailsio/runtime";
import * as SessionService from "../bindings/seuszjz/sessionservice.js";
import * as LectureService from "../bindings/seuszjz/lectureservice.js";
import * as GrabService from "../bindings/seuszjz/grabservice.js";
import type { Settings, Status } from "../bindings/seuszjz/models.js";
import type { Lecture, MyActivity, Notice, Result } from "../bindings/seuszjz/internal/seu/models.js";
import type { State as GrabState } from "../bindings/seuszjz/internal/grab/models.js";

export type ViewKey = "lectures" | "mine" | "notices" | "grab" | "settings";
export type ToastKind = "ok" | "err" | "info";
export type FilterKey = "all" | "available" | "not_open" | "full" | "joined";

export const state = reactive({
  booting: true,
  view: "lectures" as ViewKey,
  status: null as Status | null,
  settings: null as Settings | null,
  lectures: [] as Lecture[],
  mine: [] as MyActivity[],
  notices: [] as Notice[],
  serverTime: "",
  fetchedAt: "",
  loadingList: false,
  loggingIn: false,
  loginError: "",
  loginProgress: "",
  grab: null as GrabState | null,
  reserveTarget: null as Lecture | null,
  grabTarget: "" as string,
  filter: "all" as FilterKey,
  keyword: "",
  busy: {} as Record<string, boolean>,
});

// 提示统一走 vue-sonner（<Toaster /> 挂在 App.vue 里），不再自己维护队列。
export function toast(kind: ToastKind, text: string): void {
  const message = text?.trim() || "发生未知错误";
  if (kind === "ok") showToast.success(message);
  else if (kind === "err") showToast.error(message);
  else showToast(message);
}

export function errorText(e: unknown): string {
  if (typeof e === "string") return clean(e);
  if (e instanceof Error) return clean(e.message);
  if (e && typeof e === "object" && "message" in e) return clean(String((e as { message: unknown }).message));
  return clean(String(e));
}

function clean(text: string): string {
  return text.replace(/^Error:\s*/i, "").replace(/^.*?error:\s*/i, "").trim() || "发生未知错误";
}

export function busy(key: string, value: boolean): void {
  if (value) state.busy[key] = true;
  else delete state.busy[key];
}

export function isBusy(key: string): boolean {
  return !!state.busy[key];
}

export function loggedIn(): boolean {
  return !!state.status && state.status.loggedIn;
}

// shadcn-vue 的暗色模式是 html.dark（不是 data-theme），同步改 color-scheme 让原生控件跟着变。
export function applyTheme(theme: string): void {
  const dark = theme !== "light";
  const root = document.documentElement;
  root.classList.toggle("dark", dark);
  root.style.colorScheme = dark ? "dark" : "light";
}

export function lectureStatusText(status: string): string {
  switch (status) {
    case "available":
      return "可预约";
    case "full":
      return "已满";
    case "not_open":
      return "未开放";
    case "ended":
      return "已结束";
    case "booked":
      return "已预约";
    case "checked_in":
      return "已打卡";
    case "defaulted":
      return "已违约";
    case "cancelled":
      return "已取消";
    default:
      return "未知";
  }
}

export function formatDateTime(value: string): string {
  if (!value) return "—";
  return value.replace(":00", "").replace(/-/g, "/").slice(0, 16);
}

export function clockOf(value: string): string {
  const m = value.match(/(\d{2}:\d{2})/);
  return m ? m[1] : "";
}

// --- 启动 -------------------------------------------------------------------

const viewKeys: ViewKey[] = ["lectures", "mine", "notices", "grab", "settings"];

// 支持 /?view=mine 之类的深链接，方便直接从指定页面打开。
function initialView(): ViewKey | null {
  const raw = new URLSearchParams(window.location.search).get("view");
  if (!raw) return null;
  return (viewKeys as string[]).includes(raw) ? (raw as ViewKey) : null;
}

// --- 本地存储 ---------------------------------------------------------------

// 学号、混淆后的密码、会话 Cookie 都存这里，程序目录里不再落任何文件：
// 换浏览器 / 清站点数据 = 退出登录，换台机器也能把登录状态带走。
const STORE_KEY = "seuszjz.state.v1";

function readLocal(): string {
  try {
    return window.localStorage.getItem(STORE_KEY) ?? "";
  } catch {
    // 隐私模式或禁用了本地存储：退化成「不记住」，登录本身不受影响。
    return "";
  }
}

function writeLocal(snapshot: string): void {
  try {
    if (snapshot) window.localStorage.setItem(STORE_KEY, snapshot);
    else window.localStorage.removeItem(STORE_KEY);
  } catch {
    /* 同上，存不了就只在内存里用 */
  }
}

export async function boot(): Promise<void> {
  registerEvents();
  try {
    const data = await SessionService.Bootstrap(readLocal());
    // 后端会把导入/校正后的快照原样返回，直接覆盖写回。
    writeLocal(data.state ?? "");
    state.status = data.status;
    state.settings = data.settings;
    state.grab = data.grab;
    applyTheme(data.settings.theme);
    const requested = initialView();
    if (requested) state.view = requested;

    if (data.status.loggedIn) {
      await refresh();
    } else if (data.settings.autoLogin && (data.status.hasSession || data.settings.hasPassword)) {
      await autoLogin();
    }
  } catch (e) {
    toast("err", errorText(e));
  } finally {
    state.booting = false;
  }
}

const listeners: Array<() => void> = [];

function registerEvents(): void {
  listeners.push(
    Events.On("login:progress", (ev) => {
      state.loginProgress = String(ev.data ?? "");
    }),
  );
  listeners.push(
    Events.On("grab:state", (ev) => {
      state.grab = ev.data as unknown as GrabState;
    }),
  );
  listeners.push(
    Events.On("grab:notice", (ev) => {
      toast("info", String(ev.data ?? ""));
    }),
  );
  listeners.push(
    Events.On("ocr:ready", (ev) => {
      const data = ev.data as { available?: boolean; info?: string } | null;
      if (state.status && data) {
        state.status.ocrAvailable = !!data.available;
        state.status.ocrInfo = data.info ?? "";
      }
      if (data?.available) toast("ok", "本地验证码识别已就绪");
    }),
  );
  listeners.push(
    Events.On("state:changed", (ev) => {
      // 登录成功、退出、改设置、Cookie 失效时后端会推一份新快照过来。
      writeLocal(String(ev.data ?? ""));
    }),
  );
  listeners.push(
    Events.On("grab:done", () => {
      void refresh();
    }),
  );
  window.addEventListener("beforeunload", () => {
    for (const off of listeners) off();
  });
}

// --- 会话 -------------------------------------------------------------------

async function afterLogin(): Promise<void> {
  state.status = await SessionService.Status();
  state.loginError = "";
  if (state.status.user.displayName) {
    toast("ok", `欢迎回来，${state.status.user.displayName}`);
  } else {
    toast("ok", "登录成功");
  }
  await refresh();
}

export async function login(username: string, password: string, remember: boolean): Promise<boolean> {
  if (state.loggingIn) return false;
  state.loggingIn = true;
  state.loginError = "";
  state.loginProgress = "正在连接统一身份认证…";
  try {
    await SessionService.Login(username.trim(), password, remember);
    await afterLogin();
    return true;
  } catch (e) {
    state.loginError = errorText(e);
    return false;
  } finally {
    state.loggingIn = false;
    state.loginProgress = "";
  }
}

export async function autoLogin(): Promise<void> {
  if (state.loggingIn) return;
  state.loggingIn = true;
  state.loginError = "";
  state.loginProgress = "正在尝试免登录…";
  try {
    const res = await SessionService.AutoLogin();
    await afterLogin();
    if (res.source === "cache") toast("info", "已复用本机会话");
  } catch (e) {
    const text = errorText(e);
    if (!/尚未登录/.test(text)) state.loginError = text;
  } finally {
    state.loggingIn = false;
    state.loginProgress = "";
  }
}

export async function logout(): Promise<void> {
  try {
    state.status = await SessionService.Logout();
    state.lectures = [];
    state.mine = [];
    state.notices = [];
    state.grab = null;
    toast("info", "已退出登录");
  } catch (e) {
    toast("err", errorText(e));
  }
}

// --- 列表 -------------------------------------------------------------------

export async function refresh(): Promise<void> {
  if (!loggedIn() || state.loadingList) return;
  state.loadingList = true;
  try {
    const data = await LectureService.Overview();
    state.lectures = data.lectures ?? [];
    state.mine = data.mine ?? [];
    state.notices = data.notices ?? [];
    state.serverTime = data.serverTime;
    state.fetchedAt = data.fetchedAt;
  } catch (e) {
    const text = errorText(e);
    toast("err", text);
    if (/登录|会话/.test(text)) {
      state.status = await SessionService.Status();
    }
  } finally {
    state.loadingList = false;
  }
}

export function openReserve(target: Lecture): void {
  state.reserveTarget = target;
}

export function closeReserve(): void {
  state.reserveTarget = null;
}

export async function reserve(wid: string, code: string): Promise<Result | null> {
  busy("reserve", true);
  try {
    const res = await LectureService.Reserve(wid, code);
    if (res.success) {
      toast("ok", res.message || "预约成功");
      await refresh();
    }
    return res;
  } catch (e) {
    toast("err", errorText(e));
    return null;
  } finally {
    busy("reserve", false);
  }
}

export async function cancel(wid: string): Promise<void> {
  busy("cancel-" + wid, true);
  try {
    const res = await LectureService.Cancel(wid);
    if (res.success) toast("ok", res.message || "已取消预约");
    else toast("err", res.message || "取消失败");
    await refresh();
  } catch (e) {
    toast("err", errorText(e));
  } finally {
    busy("cancel-" + wid, false);
  }
}

export async function check(wid: string): Promise<void> {
  busy("check-" + wid, true);
  try {
    const res = await LectureService.Check(wid);
    toast(res.success ? "ok" : "err", res.message || (res.success ? "校验通过" : "校验失败"));
  } catch (e) {
    toast("err", errorText(e));
  } finally {
    busy("check-" + wid, false);
  }
}

export async function fetchCaptcha(): Promise<string> {
  const cap = await LectureService.Captcha();
  return cap.image;
}

export async function solveCaptcha(image: string): Promise<string> {
  try {
    return await LectureService.Solve(image);
  } catch (e) {
    toast("err", errorText(e));
    return "";
  }
}

// --- 抢座 -------------------------------------------------------------------

export function startGrab(wid: string, interval: number, cooldown: number, autoCaptcha: boolean): void {
  void (async () => {
    try {
      state.grab = await GrabService.Start(wid, interval, cooldown, autoCaptcha);
      state.grabTarget = wid;
      toast("info", "抢座任务已启动");
    } catch (e) {
      toast("err", errorText(e));
    }
  })();
}

export async function stopGrab(): Promise<void> {
  try {
    state.grab = await GrabService.Stop();
    toast("info", "抢座任务已停止");
  } catch (e) {
    toast("err", errorText(e));
  }
}

export async function submitGrabCode(code: string): Promise<boolean> {
  try {
    state.grab = await GrabService.SubmitCode(code);
    return true;
  } catch (e) {
    toast("err", errorText(e));
    return false;
  }
}

export async function refreshGrabCaptcha(): Promise<void> {
  try {
    state.grab = await GrabService.RefreshCaptcha();
  } catch (e) {
    toast("err", errorText(e));
  }
}

export async function saveSettings(patch: Partial<Settings>): Promise<void> {
  if (!state.settings) return;
  const next: Settings = { ...state.settings, ...patch };
  try {
    state.settings = await SessionService.SaveSettings(next);
    applyTheme(state.settings.theme);
    toast("ok", "设置已保存");
  } catch (e) {
    toast("err", errorText(e));
  }
}

// 清掉 localStorage 里保存的密码与会话（后端顺手推一份空快照回来）。
export async function forgetLogin(): Promise<void> {
  try {
    state.settings = await SessionService.ForgetLogin();
    state.status = await SessionService.Status();
    state.lectures = [];
    state.mine = [];
    state.notices = [];
    toast("ok", "已清除本机保存的登录信息");
  } catch (e) {
    toast("err", errorText(e));
  }
}
