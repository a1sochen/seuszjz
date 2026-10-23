# 前端（Vue 3 + TypeScript）

界面代码，构建产物 `dist/` 会被 `main.go` 的 `go:embed` 打进二进制。技术栈：Vue 3 + Vite + TypeScript + Tailwind CSS v4 + shadcn-vue（黑白中性色）。

## 常用命令

```bash
npm install          # 安装依赖
npm run dev          # 开发服务器（端口由根目录 Taskfile 的 VITE_PORT 控制）
npm run build        # 类型检查 + 生产构建
npm run build:dev    # 类型检查 + 不压缩构建
```

## 目录

```
src/main.ts           入口，挂载 App（全局样式与 sonner 提示样式在此引入）
src/App.vue           外壳：Sidebar + 顶栏 + 五个页面 + 登录页
src/store.ts          全局状态（reactive）、localStorage 快照、主题、事件订阅
src/components/AppSidebar.vue   左侧导航与账号区
src/components/views/           讲座 / 抢座 / 我的预约 / 通知公告 / 设置 / 登录
src/components/ui/              shadcn-vue 组件（按需添加，没用到的可以删）
src/lib/utils.ts                cn() 工具（shadcn 组件按需引用）
bindings/                      由 `wails3 generate bindings` 生成的 Go 接口绑定，不要手改
```

新增组件用 `npx shadcn-vue@latest add <组件名>`，样式变量在 `src/style.css`。
