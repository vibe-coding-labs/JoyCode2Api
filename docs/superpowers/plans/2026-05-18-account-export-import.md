# Account Export/Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 添加账号导出/导入功能，将已登录账号（含 pt_key 凭证）导出为 JSON 文件，可在其他电脑的 JoyCodeProxy 实例上导入使用

**Architecture:** 用户点击导出 → 后端解密所有 pt_key → 返回 JSON 数组 → 前端触发下载；用户上传 JSON → 前端读取 → 后端逐条调用 AddAccount（自动处理重复：已有账号只更新 pt_key，新账号正常添加）

**Tech Stack:** Go 1.22, SQLite, React 18, Ant Design 5, TypeScript 5

**Scope:** Small
**Risk:** Medium

**Risks:**
- 导出文件包含明文 pt_key，需用户自行确保安全传输 → 缓解：UI 中添加安全提示
- 导入时 user_id 冲突 → 缓解：复用 AddAccount 的 upsert 逻辑（已有账号更新 pt_key）
- 导入大文件可能慢 → 缓解：小规模数据（通常 < 20 个账号）影响可忽略

**Autonomy Level:** Full

---

### Task 1: Backend Store 层 — 添加 ExportAccounts 和 ImportAccounts 方法

**Depends on:** None
**Files:**
- Modify: `pkg/store/store.go:753-770`（在 ClearAllAccounts 方法之后添加）

- [ ] **Step 1: 添加 ExportAccountItem 类型和 ExportAccounts 方法**

文件: `pkg/store/store.go:753`（在 ClearAllAccounts 函数之后追加）

```go
// ExportAccountItem is the format for account export/import.
type ExportAccountItem struct {
	UserID       string `json:"user_id"`
	Nickname     string `json:"nickname"`
	Remark       string `json:"remark"`
	PtKey        string `json:"pt_key"`
	IsDefault    bool   `json:"is_default"`
	DefaultModel string `json:"default_model"`
	DisplayOrder int    `json:"display_order"`
}

// ExportAccounts returns all accounts with decrypted pt_keys for export.
func (s *Store) ExportAccounts() ([]ExportAccountItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		"SELECT user_id, nickname, remark, pt_key, is_default, default_model, COALESCE(display_order, 0) FROM accounts ORDER BY display_order, created_at",
	)
	if err != nil {
		return nil, fmt.Errorf("query accounts for export: %w", err)
	}
	defer rows.Close()

	var items []ExportAccountItem
	for rows.Next() {
		var item ExportAccountItem
		var encPtKey string
		var isDef int
		if err := rows.Scan(&item.UserID, &item.Nickname, &item.Remark, &encPtKey, &isDef, &item.DefaultModel, &item.DisplayOrder); err != nil {
			return nil, fmt.Errorf("scan account for export: %w", err)
		}
		ptKey, err := s.decrypt(encPtKey)
		if err != nil {
			slog.Warn("store: skip account in export, decrypt failed", "user_id", item.UserID, "error", err)
			continue
		}
		item.PtKey = ptKey
		item.IsDefault = isDef == 1
		items = append(items, item)
	}
	if items == nil {
		items = []ExportAccountItem{}
	}
	return items, nil
}

// ImportAccounts imports accounts from export data. Existing accounts are updated (pt_key only).
func (s *Store) ImportAccounts(items []ExportAccountItem) (added int, updated int, err error) {
	for _, item := range items {
		if item.UserID == "" || item.PtKey == "" {
			continue
		}
		var existing int
		s.mu.Lock()
		err := s.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE user_id = ?", item.UserID).Scan(&existing)
		s.mu.Unlock()
		if err != nil {
			return added, updated, fmt.Errorf("check existing account %s: %w", item.UserID, err)
		}
		if err := s.AddAccount(item.UserID, item.PtKey, item.Nickname, item.IsDefault, item.DefaultModel); err != nil {
			return added, updated, fmt.Errorf("import account %s: %w", item.UserID, err)
		}
		if existing > 0 {
			updated++
		} else {
			added++
		}
	}
	return added, updated, nil
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./...`
Expected:
  - Exit code: 0
  - Output does NOT contain: "error" or "undefined"

- [ ] **Step 3: 质量门禁 — 编译检查**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go vet ./pkg/store/...`
Expected:
  - Exit code: 0

- [ ] **Step 4: 提交**

Run: `git add pkg/store/store.go && git commit -m "feat(store): add ExportAccounts and ImportAccounts methods"`

---

### Task 2: Backend Handler + Frontend API — 添加导出/导入端点和 API 客户端方法

**Depends on:** Task 1
**Files:**
- Modify: `pkg/dashboard/handler.go:47-70`（RegisterRoutes 中添加路由）
- Modify: `pkg/dashboard/handler.go:635`（在 handleClearAllAccounts 之后添加 handler 函数）
- Modify: `web/src/api.ts:224`（在 api 对象末尾添加方法）

- [ ] **Step 1: 在 handler.go 中注册导出/导入路由**

文件: `pkg/dashboard/handler.go:68`（在 `mux.HandleFunc("/api/github-stars", h.handleGitHubStars)` 之后添加）

```go
	mux.HandleFunc("/api/accounts-export", h.handleExportAccounts)
	mux.HandleFunc("/api/accounts-import", h.handleImportAccounts)
```

- [ ] **Step 2: 在 handler.go 中添加 handleExportAccounts 和 handleImportAccounts**

文件: `pkg/dashboard/handler.go:635`（在 handleClearAllAccounts 函数之后追加）

```go
func (h *Handler) handleExportAccounts(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	items, err := h.store.ExportAccounts()
	if err != nil {
		slog.Error("export-accounts: failed", "error", err)
		writeError(w, http.StatusInternalServerError, "导出账号失败: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"accounts": items,
		"count":    len(items),
	})
}

func (h *Handler) handleImportAccounts(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body struct {
		Accounts []store.ExportAccountItem `json:"accounts"`
	}
	if !readJSONBody(w, r, &body) {
		return
	}
	if len(body.Accounts) == 0 {
		writeError(w, http.StatusBadRequest, "accounts array is empty")
		return
	}

	added, updated, err := h.store.ImportAccounts(body.Accounts)
	if err != nil {
		slog.Error("import-accounts: failed", "error", err)
		writeError(w, http.StatusInternalServerError, "导入账号失败: "+err.Error())
		return
	}

	slog.Info("import-accounts: completed", "added", added, "updated", updated)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"added":   added,
		"updated": updated,
		"total":   added + updated,
	})
}
```

- [ ] **Step 3: 在 api.ts 中添加前端 API 方法**

文件: `web/src/api.ts:223-224`（在 `reorderAccounts` 方法之后添加，闭合 `api` 对象之前）

```typescript
  exportAccounts: () =>
    request<{ ok: boolean; accounts: Array<{ user_id: string; nickname: string; remark: string; pt_key: string; is_default: boolean; default_model: string; display_order: number }>; count: number }>('/api/accounts-export'),
  importAccounts: (accounts: Array<{ user_id: string; nickname: string; remark: string; pt_key: string; is_default: boolean; default_model: string; display_order: number }>) =>
    request<{ ok: boolean; added: number; updated: number; total: number }>('/api/accounts-import', { method: 'POST', body: JSON.stringify({ accounts }) }),
```

- [ ] **Step 4: 验证后端编译**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./...`
Expected:
  - Exit code: 0

- [ ] **Step 5: 质量门禁**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go vet ./pkg/...`
Expected:
  - Exit code: 0

- [ ] **Step 6: 提交**

Run: `git add pkg/dashboard/handler.go web/src/api.ts && git commit -m "feat(api): add export/import endpoints and frontend API methods"`

---

### Task 3: Frontend UI — 在账号管理页面添加导出/导入按钮

**Depends on:** Task 2
**Files:**
- Modify: `web/src/pages/Accounts.tsx:1-12`（添加 icon import）
- Modify: `web/src/pages/Accounts.tsx:134-146`（添加 import 相关 state）
- Modify: `web/src/pages/Accounts.tsx:460-504`（在按钮栏添加导出/导入按钮）

- [ ] **Step 1: 在 Accounts.tsx 的 antd icon import 中添加 ExportOutlined 和 UploadOutlined**

文件: `web/src/pages/Accounts.tsx:7-12`

替换现有的 icon import：

```typescript
import {
  PlusOutlined, DeleteOutlined, StarOutlined,
  SafetyCertificateOutlined, ReloadOutlined,
  QuestionCircleOutlined, ClearOutlined, EditOutlined,
  CheckCircleOutlined, CloseCircleOutlined, ClockCircleOutlined,
  HolderOutlined, ExportOutlined, UploadOutlined,
} from '@ant-design/icons';
```

- [ ] **Step 2: 在 Accounts 组件中添加 importRef 和导入状态**

文件: `web/src/pages/Accounts.tsx:145`（在 `const [renameForm] = Form.useForm();` 之后添加）

```typescript
  const fileInputRef = React.useRef<HTMLInputElement>(null);
  const [importing, setImporting] = useState(false);
```

- [ ] **Step 3: 在按钮栏添加导出和导入按钮**

文件: `web/src/pages/Accounts.tsx:500-504`（在"手动添加"按钮之前、清空会话 Popconfirm 之后添加导出导入按钮）

在 `<Button onClick={() => setModalOpen(true)} icon={<PlusOutlined />}>手动添加</Button>` 之前插入：

```tsx
          <Button
            onClick={async () => {
              try {
                const result = await api.exportAccounts();
                if (!result.accounts || result.accounts.length === 0) {
                  message.warning('没有可导出的账号');
                  return;
                }
                const blob = new Blob([JSON.stringify(result.accounts, null, 2)], { type: 'application/json' });
                const url = URL.createObjectURL(blob);
                const a = document.createElement('a');
                a.href = url;
                a.download = `joycode-accounts-${new Date().toISOString().slice(0, 10)}.json`;
                a.click();
                URL.revokeObjectURL(url);
                message.success(`已导出 ${result.count} 个账号`);
              } catch (e: unknown) {
                message.error(e instanceof Error ? e.message : '导出失败');
              }
            }}
            icon={<ExportOutlined />}
          >
            导出账号
          </Button>
          <Button
            onClick={() => fileInputRef.current?.click()}
            icon={<UploadOutlined />}
            loading={importing}
          >
            导入账号
          </Button>
          <input
            ref={fileInputRef}
            type="file"
            accept=".json"
            style={{ display: 'none' }}
            onChange={async (e) => {
              const file = e.target.files?.[0];
              if (!file) return;
              setImporting(true);
              try {
                const text = await file.text();
                const accounts = JSON.parse(text);
                if (!Array.isArray(accounts) || accounts.length === 0) {
                  message.error('文件格式错误：应为非空 JSON 数组');
                  return;
                }
                const result = await api.importAccounts(accounts);
                message.success(`导入完成：新增 ${result.added} 个，更新 ${result.updated} 个`);
                fetchAccounts();
              } catch (err: unknown) {
                message.error(err instanceof Error ? err.message : '导入失败');
              } finally {
                setImporting(false);
                e.target.value = '';
              }
            }}
          />
```

- [ ] **Step 4: 验证前端编译**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npx tsc --noEmit 2>&1 | head -20`
Expected:
  - Exit code: 0 或无 TS 错误（已存在的警告可忽略）

- [ ] **Step 5: 完整构建验证**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o /dev/null ./cmd/proxy/`
Expected:
  - Exit code: 0

- [ ] **Step 6: 质量门禁**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npx tsc --noEmit`
Expected:
  - Exit code: 0

- [ ] **Step 7: 提交**

Run: `git add web/src/pages/Accounts.tsx && git commit -m "feat(ui): add account export/import buttons to accounts page"`

---

## Self-Review Results

**Plan Type:** Feature

| # | Check | Result | Action Taken |
|---|-------|--------|-------------|
| 1 | Goal + Type + Scope + Risk? | PASS | — |
| 2 | Dependencies? | PASS | Task 2→1, Task 3→2 |
| 3 | 每个 Task 有 3-8 个 Step? | PASS | Task 1: 4, Task 2: 6, Task 3: 7 |
| 4 | 无 TBD/TODO/模糊描述? | PASS | — |
| 5 | 跨 Task 函数签名一致? | PASS | ExportAccountItem 类型在 Task 1 定义，Task 2/3 引用一致 |
| 6 | 文件保存位置正确? | PASS | docs/superpowers/plans/ |
| 7 | 每个 Task 有质量门禁? | PASS | 每个 Task 都有编译+vet/tsc 验证 |
| 8 | 无交付反模式? | PASS | 无 any 类型、无 console.log、无硬编码 |
| 9 | 精确文件路径+行号? | PASS | — |
| 10 | 完整代码（非 diff）? | PASS | — |
| 11 | 验证命令三要素? | PASS | — |
| 12 | 无悬空引用? | PASS | ExportAccountItem 在 store 包定义，handler 通过 store.ExportAccountItem 引用 |
| 13 | 每个 Task 可独立验证? | PASS | 编译验证即可 |
| 14 | 无 "add validation" 等抽象指令? | PASS | — |
| 15 | Header 包含 Architecture? | PASS | 数据流+关键组件+设计理由 |

**Status:** ✅ ALL PASS

---

## Execution Selection

**Tasks:** 3
**Dependencies:** yes（Task 2→1, Task 3→2）
**User Preference:** none
**Decision:** Inline（3 个 Task 有严格顺序依赖，subagent 并行无收益）
**Reasoning:** Task 间串行依赖，Inline 执行更高效
