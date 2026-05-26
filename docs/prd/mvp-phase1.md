# PRD: EMT (Easy Multi-agent Terminal)

## Context

个人开发者需要一个轻量级桌面终端，用于管理多个 AI coding agent（MVP 阶段仅适配 Codex CLI）。核心痛点：每次重启都丢失会话上下文，需要手动恢复。目标是开箱即用、重启自动恢复、跨平台支持。

项目已有 Wails v2 (Go + Vue 3 + TypeScript) 脚手架，可直接在此基础上开发。

---

## MVP Phase 1 目标

1. 管理多个 Codex session（创建/切换/关闭）
2. 重启后自动恢复 session 列表，用户可继续对话
3. 内嵌终端模拟器，直接在应用内与 Codex 交互

---

## 技术架构

### 整体结构

```
┌─────────────────────────────────────────────┐
│  Frontend (Vue 3 + xterm.js)                │
│  ┌─────────┐ ┌────────────────────────────┐ │
│  │ Session │ │  Terminal View (xterm.js)   │ │
│  │  List   │ │                            │ │
│  │         │ │  Codex CLI running here    │ │
│  │ [+New]  │ │                            │ │
│  │ sess-1  │ │                            │ │
│  │ sess-2  │ │                            │ │
│  └─────────┘ └────────────────────────────┘ │
└─────────────────────────────────────────────┘
        │ Wails Bindings + Events (IPC)
┌─────────────────────────────────────────────┐
│  Backend (Go)                               │
│  ┌──────────────┐  ┌────────────────────┐  │
│  │ SessionMgr   │  │  PTY Manager       │  │
│  │ - CRUD       │  │  - spawn codex     │  │
│  │ - persist    │  │  - resize          │  │
│  │ - restore    │  │  - I/O relay       │  │
│  └──────────────┘  └────────────────────┘  │
│  ┌──────────────────────────────────────┐   │
│  │ Session Store (JSON file)            │   │
│  │ ~/.emt/sessions.json                 │   │
│  └──────────────────────────────────────┘   │
└─────────────────────────────────────────────┘
```

### Backend (Go)

**核心模块：**

1. **SessionManager** (`session.go`)
   - 维护活跃 session 列表
   - 每个 session 记录：ID、名称、Codex session 文件路径、创建时间、最后活跃时间、状态
   - 持久化到 `~/.emt/sessions.json`

2. **PTYManager** (`pty.go`)
   - 使用 `creack/pty` 库 spawn Codex CLI 进程
   - 管理 PTY 的 I/O 流
   - 支持终端 resize
   - 通过 Wails Events 将输出推送到前端

3. **App 入口** (`app.go`)
   - 暴露给前端的方法：
     - `CreateSession(name string) Session`
     - `ResumeSession(id string) error`
     - `CloseSession(id string) error`
     - `ListSessions() []Session`
     - `SendInput(id string, data string) error`
     - `ResizeTerminal(id string, rows, cols int) error`

**Session 恢复流程：**
1. 启动时读取 `~/.emt/sessions.json`
2. 扫描 `~/.codex/sessions/` 验证 session 文件是否存在
3. 前端展示 session 列表
4. 用户点击 session → 调用 `codex resume <session-path>` spawn 新 PTY
5. PTY 输出通过 Wails Events 推送到前端 xterm.js

**通信方式：**
- MVP 阶段使用 Wails Events（`runtime.EventsEmit` / `runtime.EventsOn`）
- 如遇性能瓶颈再切换 WebSocket

### Frontend (Vue 3 + TypeScript)

**依赖：**
- `xterm` + `xterm-addon-fit` — 终端模拟器
- `pinia` — 状态管理

**组件结构：**
```
App.vue
├── Sidebar.vue          # Session 列表 + 新建按钮
└── TerminalPanel.vue    # xterm.js 终端视图
```

**交互流程：**
1. 左侧 sidebar 展示所有 session（活跃 + 历史）
2. 点击 session → 切换终端视图
3. 点击 "+" → 创建新 Codex session
4. 终端内直接与 Codex 交互
5. 关闭应用 → 自动保存 session 状态
6. 重新打开 → 展示上次的 session 列表，点击即可 resume

### 数据模型

```json
// ~/.emt/sessions.json
{
  "sessions": [
    {
      "id": "uuid-1",
      "name": "feature-auth",
      "codex_session_path": "~/.codex/sessions/2026/05/26/rollout-1716700000-abc123.jsonl",
      "working_dir": "/home/user/project",
      "created_at": "2026-05-26T10:00:00Z",
      "last_active_at": "2026-05-26T12:30:00Z",
      "status": "idle"
    }
  ]
}
```

---

## Codex CLI 集成细节

**Session 存储位置：** `~/.codex/sessions/YYYY/MM/DD/rollout-<timestamp>-<id>.jsonl`

**恢复命令：** `codex resume`（交互式选择）或指定 session 文件路径

**新建命令：** `codex`（直接启动新 session）

**关键行为：**
- Codex 自动将每个 session 保存为 JSONL 文件
- Session 数据纯本地，不依赖远程状态
- EMT 只需维护 session 索引，实际数据由 Codex 管理

---

## 文件变更计划

### 新增文件

| 文件 | 用途 |
|------|------|
| `session.go` | SessionManager + Session 数据结构 |
| `pty.go` | PTY 进程管理 |
| `frontend/src/components/Sidebar.vue` | Session 列表组件 |
| `frontend/src/components/TerminalPanel.vue` | xterm.js 终端组件 |
| `frontend/src/stores/session.ts` | Pinia session store |

### 修改文件

| 文件 | 变更 |
|------|------|
| `app.go` | 移除 Greet demo，集成 SessionManager 和 PTYManager |
| `main.go` | 注册新的 bindings |
| `frontend/src/App.vue` | 替换为 Sidebar + TerminalPanel 布局 |
| `frontend/src/main.ts` | 注册 Pinia |
| `frontend/package.json` | 添加 xterm、pinia 依赖 |
| `go.mod` | 添加 creack/pty 依赖 |

---

## 跨平台注意事项

- `creack/pty` 支持 macOS/Linux，Windows 需要用 `conpty`
- MVP 阶段先支持 macOS + Linux，Windows 支持放后续
- Wails 本身已有 Windows 构建配置，后续切换 PTY 实现即可

---

## 验证方式

1. `wails dev` 启动开发模式
2. 点击 "+" 创建新 session → 应看到 Codex CLI 启动
3. 在终端内与 Codex 对话
4. 关闭应用 → 重新 `wails dev`
5. 看到之前的 session 列表 → 点击 resume → 能继续对话

---

## 后续迭代（不在 MVP 范围）

- IDE 跳转（解析 file:line 模式，调用 `code --goto` / `goland --line`）
- 语义搜索历史对话（本地 embedding 或 LLM 摘要 + 标签）
- 自动语义分组
- 多 agent 适配（Claude Code `--resume`、通用 PTY wrapper）
- Windows 支持（conpty）
- Agent 上下文注入（MCP server 方式将历史摘要喂给 agent）
