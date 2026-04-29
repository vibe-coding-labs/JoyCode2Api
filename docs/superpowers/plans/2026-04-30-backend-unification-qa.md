# Backend Unification QA & Cleanup Plan

**Goal:** 清理 Python 遗留、补全关键测试、确保前后端对接正确，交付可直接使用的单 Go 后端。

**Architecture:** Go 后端 (`cmd/JoyCodeProxy/`) 嵌入 React 前端 (`cmd/JoyCodeProxy/static/`)，通过 SQLite (`pkg/store/`) 提供账户/设置管理，Dashboard API (`pkg/dashboard/`) 服务 `/api/*` 端点。

**Tech Stack:** Go 1.23, SQLite (go-sqlite3), React 19 + Ant Design 6 + Vite

**Risks:**
- Python 目录 `joycode_proxy/` 需用户手动 `rm -rf`（沙箱限制）
- 新测试可能暴露 store/dashboard 隐藏 bug

---

### Task 1: 清理 Python 遗留

**Depends on:** None
**Files:**
- Delete: `pyproject.toml`
- Delete: `tests/` (Python 测试)
- Delete: `joycode_proxy/` (需用户手动)

- [ ] **Step 1: 删除 pyproject.toml**
- [ ] **Step 2: 删除 tests/ 目录**
- [ ] **Step 3: 提示用户手动 rm -rf joycode_proxy/**

---

### Task 2: pkg/store 单元测试

**Depends on:** None
**Files:**
- Create: `pkg/store/store_test.go`

覆盖：数据库初始化、加密解密、账户 CRUD、设置管理、请求日志、统计查询。

---

### Task 3: pkg/dashboard HTTP 测试

**Depends on:** None
**Files:**
- Create: `pkg/dashboard/handler_test.go`

覆盖：所有 `/api/*` 端点的 happy path + error path，使用 httptest.NewRecorder。

---

### Task 4: 集成测试 — Dashboard + Static

**Depends on:** Task 2, Task 3
**Files:**
- Create: `cmd/JoyCodeProxy/dashboard_test.go`

测试：构建二进制 → 启动 → 测试 `/api/health`、`/api/accounts` CRUD、前端 HTML 返回、SPA fallback。

---

### Task 5: 全量构建 + 测试验证

**Depends on:** Task 4
**Files:**
- Run: `go test ./...`
- Run: `go vet ./...`
- Run: 前端 `npm run build`
- Run: 端到端验证
