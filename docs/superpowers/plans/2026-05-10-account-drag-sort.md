# Account Drag-and-Drop Sort Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 在账号列表页面支持拖动排序，拖动后的顺序持久化到数据库，后端返回时按用户设置的排序优先级排列。

**Architecture:** 前端用户拖动表格行 → 计算新顺序 → 调用 `PUT /api/accounts/reorder` 批量更新 → 后端写入 accounts 表 display_order 列 → 后续 ListAccounts 查询按 display_order 排序返回。使用 `@dnd-kit/core` + `@dnd-kit/sortable` 库实现拖动，配合 antd Table 的自定义 `components.body.wrapper`。

**Tech Stack:** Go 1.23, SQLite 3, React 19, Antd 6.3, @dnd-kit/core ^6.x, @dnd-kit/sortable ^8.x, @dnd-kit/utilities ^3.x, TypeScript 6

**Risks:**
- display_order 列需兼容已有数据（ALTER TABLE 加列，默认值为 0，已有账户统一初始化为按 created_at 排序的递增值）→ 缓解：在 migration 中用 UPDATE 语句初始化
- Antd v6 Table 的拖动排序需要自定义 `components.body.wrapper`，实现较复杂 → 缓解：参考 dnd-kit 官方的 antd Table 示例模式
- 前端需安装新 npm 包 → 缓解：dnd-kit 是轻量级库，无额外依赖

---

### Task 1: 后端 Store 层 — 数据库 schema 新增 display_order 列及 ReorderAccounts 方法

**Depends on:** None
**Files:**
- Modify: `pkg/store/store.go:237-284` (schema + migration)
- Modify: `pkg/store/store.go:48-65` (AccountInfo 新增字段)
- Modify: `pkg/store/store.go:521-542` (ListAccounts 排序)
- Modify: `pkg/store/store.go:510-512` (AddAccount 设置初始 display_order)

- [ ] **Step 1: 在 AccountInfo 结构体新增 DisplayOrder 字段 — 支持前端和 JSON 序列化**
文件: `pkg/store/store.go:48-65`

```go
type AccountInfo struct {
	UserID          string `json:"user_id"`
	Nickname        string `json:"nickname"`
	Remark          string `json:"remark"`
	APIToken        string `json:"api_token"`
	IsDefault       bool   `json:"is_default"`
	DefaultModel    string `json:"default_model"`
	CreatedAt       string `json:"created_at,omitempty"`
	DisplayOrder    int    `json:"display_order"`
	ActiveSessions  int64  `json:"active_sessions"`
	TotalRequests   int    `json:"total_requests"`
	TodayRequests   int    `json:"today_requests"`
	TotalTokens     int    `json:"total_tokens"`
	TodayTokens     int    `json:"today_tokens"`
	CredentialValid      int    `json:"credential_valid"`
	CredentialCheckedAt string `json:"credential_checked_at,omitempty"`
	CredentialRefreshAt string `json:"credential_refreshed_at,omitempty"`
	CredentialError     string `json:"credential_error,omitempty"`
}
```

- [ ] **Step 2: 在 migrate() 函数中添加 display_order 列的 ALTER TABLE migration — 兼容已有数据库**
文件: `pkg/store/store.go:275-284`（在 `s.migrateUserIDAsPK()` 之前添加）

在 `s.db.Exec("ALTER TABLE request_logs ADD COLUMN output_tokens INTEGER DEFAULT 0")` 之后，`s.migrateUserIDAsPK()` 之前，添加：

```go
	// Migration: add display_order column to accounts
	s.db.Exec("ALTER TABLE accounts ADD COLUMN display_order INTEGER DEFAULT 0")
```

同时在 `migrate()` 函数末尾（`return nil` 之前）添加初始化逻辑：

```go
	// Initialize display_order for existing accounts (ordered by created_at)
	s.migrateDisplayOrder()
```

- [ ] **Step 3: 创建 migrateDisplayOrder() 方法 — 为已有账户初始化排序值**

在 `pkg/store/store.go` 中 `migrateUTCTimestamps()` 方法之后添加新方法：

```go
func (s *Store) migrateDisplayOrder() {
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE display_order = 0").Scan(&count)
	if count == 0 {
		return
	}
	slog.Info("store: initializing display_order for existing accounts", "count", count)
	rows, err := s.db.Query("SELECT user_id FROM accounts ORDER BY created_at")
	if err != nil {
		slog.Error("store: migrateDisplayOrder query failed", "error", err)
		return
	}
	defer rows.Close()
	order := 1
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			continue
		}
		s.db.Exec("UPDATE accounts SET display_order = ? WHERE user_id = ?", order, userID)
		order++
	}
}
```

- [ ] **Step 4: 修改 ListAccounts 查询 — 按 display_order 排序并返回该字段**
文件: `pkg/store/store.go:521-542`

```go
func (s *Store) ListAccounts() ([]AccountInfo, error) {
	rows, err := s.db.Query("SELECT user_id, nickname, remark, api_token, is_default, default_model, created_at, credential_valid, credential_refreshed_at, COALESCE(display_order, 0) FROM accounts ORDER BY display_order, created_at")
	if err != nil {
		slog.Error("store: list accounts query failed", "error", err)
		return nil, err
	}
	defer rows.Close()

	var accounts []AccountInfo
	for rows.Next() {
		var a AccountInfo
		var isDef int
		if err := rows.Scan(&a.UserID, &a.Nickname, &a.Remark, &a.APIToken, &isDef, &a.DefaultModel, &a.CreatedAt, &a.CredentialValid, &a.CredentialRefreshAt, &a.DisplayOrder); err != nil {
			slog.Error("store: list accounts scan failed", "error", err)
			return nil, err
		}
		a.IsDefault = isDef == 1
		a.CredentialCheckedAt = a.CredentialRefreshAt
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}
```

- [ ] **Step 5: 修改 AddAccount — 新账户自动获得最大 display_order + 1**

在 `pkg/store/store.go` 的 `AddAccount` 函数中，将 `INSERT` 语句（约第 510-512 行）修改为：

```go
		// Get max display_order
		var maxOrder int
		s.db.QueryRow("SELECT COALESCE(MAX(display_order), 0) FROM accounts").Scan(&maxOrder)

		token := generateToken()
		_, err = s.db.Exec(
			"INSERT INTO accounts (user_id, nickname, api_token, pt_key, is_default, default_model, display_order) VALUES (?, ?, ?, ?, ?, ?, ?)",
			userID, nickname, token, encPtKey, def, defaultModel, maxOrder+1,
		)
```

- [ ] **Step 6: 创建 ReorderAccounts 方法 — 批量更新账户排序**

在 `pkg/store/store.go` 中 `RemoveAccount` 方法之前添加：

```go
func (s *Store) ReorderAccounts(userIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for i, uid := range userIDs {
		if _, err := tx.Exec("UPDATE accounts SET display_order = ? WHERE user_id = ?", i+1, uid); err != nil {
			return fmt.Errorf("update display_order for %s: %w", uid, err)
		}
	}
	return tx.Commit()
}
```

- [ ] **Step 7: 验证编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./pkg/store/`
Expected:
  - Exit code: 0
  - Output does NOT contain: "error" or "cannot"

- [ ] **Step 8: 提交**
Run: `git add pkg/store/store.go && git commit -m "feat(store): add display_order column for account drag-sort persistence"`

---

### Task 2: 后端 Handler 层 — 新增 reorder API 端点

**Depends on:** Task 1
**Files:**
- Modify: `pkg/dashboard/handler.go:904-926` (路由分发新增 reorder case)

- [ ] **Step 1: 在 handleAccountAction 的 switch 中新增 reorder 路由分支**
文件: `pkg/dashboard/handler.go:904-926`

在 `case action == "remark" && r.Method == http.MethodPut:` 之后、`default:` 之前添加：

```go
		case action == "reorder" && r.Method == http.MethodPut:
			h.reorderAccounts(w, r)
```

- [ ] **Step 2: 创建 reorderAccounts handler 方法 — 接收 user_id 数组并更新排序**

在 `pkg/dashboard/handler.go` 中 `handleAccountAction` 函数之后添加：

```go
func (h *Handler) reorderAccounts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserIDs []string `json:"user_ids"`
	}
	if !readJSONBody(w, r, &body) {
		return
	}
	if len(body.UserIDs) == 0 {
		writeError(w, http.StatusBadRequest, "user_ids is required")
		return
	}
	if err := h.store.ReorderAccounts(body.UserIDs); err != nil {
		slog.Error("reorder accounts", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}
```

- [ ] **Step 3: 在路由注册中新增 /api/accounts/reorder 顶层路由**

需要检查 `handleAccountAction` 的路由匹配模式。由于当前路由格式是 `/api/accounts/{user_id}/{action}`，而 reorder 是批量操作不涉及单个 user_id，需要在 handler.go 中的路由注册处（`handleAccounts` 函数附近）添加对 `/api/accounts/reorder` 的独立匹配。

在 `pkg/dashboard/handler.go` 的 `ServeHTTP` 或路由注册方法中，找到 `/api/accounts/` 前缀匹配处，在进入 `handleAccountAction` 之前添加：

```go
		case r.URL.Path == "/api/accounts/reorder" && r.Method == http.MethodPut:
			h.reorderAccounts(w, r)
```

注意：需要确认路由注册的具体位置和模式。根据 handler.go:892 的代码，`handleAccountAction` 使用 `strings.Split(strings.TrimPrefix(path, "/api/accounts/"), "/")` 来解析路径。所以 `PUT /api/accounts/reorder` 会被解析为 `parts[0]="reorder", action=""`，这不会匹配到现有的任何 case。需要在 switch 中新增一个分支来处理这种情况。

实际上更简洁的方式是：在 `handleAccountAction` 的 switch 中，在现有 case 之前添加：

文件: `pkg/dashboard/handler.go:904-926`

将整个 switch 块替换为：

```go
	switch {
	case action == "" && parts[0] == "reorder" && r.Method == http.MethodPut:
		h.reorderAccounts(w, r)
	case action == "" && r.Method == http.MethodDelete:
		h.removeAccount(w, r, apiKey)
	case action == "default" && r.Method == http.MethodPut:
		h.setDefault(w, r, apiKey)
	case action == "validate" && r.Method == http.MethodPost:
		h.validateAccount(w, r, apiKey)
	case action == "model" && r.Method == http.MethodPut:
		h.updateModel(w, r, apiKey)
	case action == "models" && r.Method == http.MethodGet:
		h.listAccountModels(w, r, apiKey)
	case action == "stats" && r.Method == http.MethodGet:
		h.getAccountStats(w, r, apiKey)
	case action == "logs" && r.Method == http.MethodGet:
		h.getAccountLogs(w, r, apiKey)
	case action == "renew-token" && r.Method == http.MethodPost:
		h.renewToken(w, r, apiKey)
	case action == "remark" && r.Method == http.MethodPut:
		h.updateRemark(w, r, apiKey)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
```

- [ ] **Step 4: 验证编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./pkg/dashboard/`
Expected:
  - Exit code: 0
  - Output does NOT contain: "error" or "cannot"

- [ ] **Step 5: 提交**
Run: `git add pkg/dashboard/handler.go && git commit -m "feat(handler): add PUT /api/accounts/reorder endpoint for drag-sort"`

---

### Task 3: 前端 — 安装拖动库并实现账号表格拖动排序

**Depends on:** Task 2
**Files:**
- Modify: `web/src/api.ts:1-18` (Account 接口)
- Modify: `web/src/api.ts:174-221` (新增 reorder 方法)
- Modify: `web/src/pages/Accounts.tsx:1-544` (拖动排序实现)

- [ ] **Step 1: 安装 @dnd-kit 拖动排序库**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm install @dnd-kit/core @dnd-kit/sortable @dnd-kit/utilities`
Expected:
  - Exit code: 0
  - Output does NOT contain: "ERR!" or "npm error"
  - package.json contains "@dnd-kit/core" in dependencies

- [ ] **Step 2: 在 Account 接口新增 display_order 字段**
文件: `web/src/api.ts:1-18`

```typescript
export interface Account {
  user_id: string;
  nickname: string;
  remark: string;
  api_token: string;
  is_default: boolean;
  default_model: string;
  created_at?: string;
  display_order: number;
  active_sessions: number;
  total_requests: number;
  today_requests: number;
  total_tokens: number;
  today_tokens: number;
  credential_valid: number; // -1=unknown, 0=expired, 1=valid
  credential_checked_at?: string;
  credential_refreshed_at?: string;
  credential_error?: string;
}
```

- [ ] **Step 3: 在 api 对象中新增 reorderAccounts 方法**
文件: `web/src/api.ts:219-221`（在 `updateRemark` 方法之后添加）

```typescript
  reorderAccounts: (userIds: string[]) =>
    request<{ ok: boolean }>('/api/accounts/reorder', { method: 'PUT', body: JSON.stringify({ user_ids: userIds }) }),
```

- [ ] **Step 4: 重写 Accounts.tsx — 集成拖动排序功能**

替换整个 `web/src/pages/Accounts.tsx` 文件内容。主要改动：
1. 导入 dnd-kit 相关组件
2. 创建 DraggableRow 组件（可拖动的表格行）
3. 在 Table 的 components.body.wrapper 上绑定 sortable 容器
4. 拖动结束时调用 API 保存新顺序

```tsx
import React, { useEffect, useState, useState as useStateType } from 'react';
import {
  Table, Button, Space, Modal, Form, Input, Switch, Select,
  message, Popconfirm, Tag, Typography, Alert, Tooltip,
} from 'antd';
import {
  PlusOutlined, DeleteOutlined, StarOutlined,
  SafetyCertificateOutlined, ReloadOutlined,
  QuestionCircleOutlined, ClearOutlined, EditOutlined,
  CheckCircleOutlined, CloseCircleOutlined, ClockCircleOutlined,
  HolderOutlined,
} from '@ant-design/icons';
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core';
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
  arrayMove,
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import type { TableProps } from 'antd';
import SvgClaudeCode from '../components/ClaudeCodeIcon';
import SvgCodex from '../components/CodexIcon';
import CommandTooltip from '../components/CommandTooltip';
import QRLoginModal from '../components/QRLoginModal';
import { useNavigate } from 'react-router-dom';
import { api, accountDisplayName } from '../api';
import type { Account } from '../api';

const BUILTIN_MODELS = [
  { label: 'JoyAI-Code（推荐）', value: 'JoyAI-Code' },
  { label: 'GLM-5.1', value: 'GLM-5.1' },
  { label: 'GLM-5', value: 'GLM-5' },
  { label: 'GLM-4.7', value: 'GLM-4.7' },
  { label: 'Kimi-K2.6', value: 'Kimi-K2.6' },
  { label: 'Kimi-K2.5', value: 'Kimi-K2.5' },
  { label: 'MiniMax-M2.7', value: 'MiniMax-M2.7' },
  { label: 'Doubao-Seed-2.0-pro', value: 'Doubao-Seed-2.0-pro' },
];

const getBaseURL = () => `http://${window.location.host}`;

const maskUserId = (id: string): string => {
  if (!id) return '-';
  if (id.length <= 3) return id[0] + '***';
  return id.slice(0, 2) + '***' + id.slice(-2);
};

const fmtTokens = (n: number): string => {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M';
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K';
  return String(n);
};

const claudeCodeCmd = (apiKey: string, model = 'GLM-5.1') => [
  `API_TIMEOUT_MS=6000000 \\`,
  `CLAUDE_CODE_MAX_RETRIES=1000000 \\`,
  `ANTHROPIC_BASE_URL=${getBaseURL()} \\`,
  `ANTHROPIC_API_KEY="${apiKey}" \\`,
  `CLAUDE_CODE_MAX_OUTPUT_TOKENS=6553655 \\`,
  `ANTHROPIC_MODEL=${model} \\`,
  `claude --dangerously-skip-permissions`,
].join('\n');

const codexCmd = (apiKey: string, model = 'GLM-5.1') => [
  `OPENAI_BASE_URL=${getBaseURL()}/v1 \\`,
  `OPENAI_API_KEY="${apiKey}" \\`,
  `OPENAI_MODEL=${model} \\`,
  `codex`,
].join('\n');

const copyToClipboard = async (text: string, label: string) => {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
    } else {
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.style.cssText = 'position:fixed;left:-9999px';
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
    }
    message.success(`${label} 命令已复制`);
  } catch {
    message.error('复制失败');
  }
};

interface DraggableRowProps extends React.HTMLAttributes<HTMLTableRowElement> {
  'data-row-key': string;
}

const DraggableRow: React.FC<DraggableRowProps> = (props) => {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: props['data-row-key'],
  });

  const style: React.CSSProperties = {
    ...props.style,
    transform: CSS.Transform.toString(transform && { ...transform, scaleY: 1 }),
    transition,
    cursor: 'move',
    ...(isDragging ? { position: 'relative', zIndex: 9999 } : {}),
  };

  return (
    <tr
      {...props}
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
    />
  );
};

const Accounts: React.FC = () => {
  const navigate = useNavigate();
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [form] = Form.useForm();
  const [validating, setValidating] = useState<string | null>(null);
  const [autoLogging, setAutoLogging] = useState(false);
  const [qrModalOpen, setQrModalOpen] = useState(false);
  const [renameModalOpen, setRenameModalOpen] = useState(false);
  const [renameTarget, setRenameTarget] = useState<string>('');
  const [renameForm] = Form.useForm();

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor),
  );

  const fetchAccounts = async () => {
    setLoading(true);
    try {
      const data = await api.listAccounts();
      setAccounts(data);
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '获取账号列表失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchAccounts(); }, []);


  const handleAdd = async (values: { pt_key: string; user_id: string; is_default?: boolean; default_model?: string }) => {
    try {
      await api.addAccount(values);
      message.success(`账号「${values.user_id}」添加成功`);
      setModalOpen(false);
      form.resetFields();
      fetchAccounts();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '添加账号失败');
    }
  };

  const handleAutoLogin = async () => {
    setAutoLogging(true);
    try {
      const result = await api.autoLogin();
      message.success(`一键登录成功！账号「${result.nickname || result.user_id}」已添加`);
      fetchAccounts();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '一键登录失败');
    } finally {
      setAutoLogging(false);
    }
  };

  const handleRemove = async (userId: string, displayName: string) => {
    try {
      await api.removeAccount(userId);
      message.success(`账号「${displayName}」已删除`);
      fetchAccounts();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '删除账号失败');
    }
  };

  const handleSetDefault = async (userId: string, displayName: string) => {
    try {
      await api.setDefault(userId);
      message.success(`已将「${displayName}」设为默认账号`);
      fetchAccounts();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '设置默认账号失败');
    }
  };

  const handleRenewToken = async (userId: string) => {
    try {
      await api.renewToken(userId);
      message.success('API Token 已更新');
      fetchAccounts();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '更新 Token 失败');
    }
  };

  const handleValidate = async (userId: string, displayName: string) => {
    setValidating(userId);
    try {
      const result = await api.validateAccount(userId);
      if (result.valid) {
        message.success(`账号「${displayName}」验证通过，凭证有效`);
      } else {
        message.error(`账号「${displayName}」验证失败，凭证无效或已过期`);
      }
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '验证请求失败');
    } finally {
      setValidating(null);
    }
  };

  const handleRename = async (values: { new_name: string }) => {
    try {
      await api.updateRemark(renameTarget, values.new_name);
      message.success(`账号备注已更新为「${values.new_name}」`);
      setRenameModalOpen(false);
      renameForm.resetFields();
      fetchAccounts();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '更新备注失败');
    }
  };

  const handleDragEnd = async (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;

    const oldIndex = accounts.findIndex((a) => a.user_id === active.id);
    const newIndex = accounts.findIndex((a) => a.user_id === over.id);
    if (oldIndex === -1 || newIndex === -1) return;

    const newAccounts = arrayMove(accounts, oldIndex, newIndex);
    setAccounts(newAccounts);

    try {
      await api.reorderAccounts(newAccounts.map((a) => a.user_id));
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '保存排序失败');
      fetchAccounts();
    }
  };

  const columns = [
    {
      title: '',
      key: 'drag',
      width: 40,
      render: () => <HolderOutlined style={{ cursor: 'grab', color: '#999' }} />,
    },
    {
      title: '账户名',
      dataIndex: 'user_id',
      key: 'user_id',
      render: (_: unknown, record: Account) => (
        <Typography.Text strong>{accountDisplayName(record)}</Typography.Text>
      ),
    },
    {
      title: 'API Token',
      dataIndex: 'api_token',
      key: 'api_token',
      render: (token: string) => (
        <Typography.Text code copyable style={{ fontSize: 12 }}>
          {token.slice(0, 12)}...{token.slice(-4)}
        </Typography.Text>
      ),
    },
    {
      title: '用户 ID',
      dataIndex: 'user_id',
      key: 'user_id',
      render: (text: string) => (
        <Typography.Text type="secondary" style={{ fontSize: 13 }}>{maskUserId(text)}</Typography.Text>
      ),
    },
    {
      title: '活跃会话',
      dataIndex: 'active_sessions',
      key: 'active_sessions',
      render: (val: number) => val > 0 ? (
        <Tag color="blue">{val} 个活跃</Tag>
      ) : (
        <Typography.Text type="secondary">无</Typography.Text>
      ),
    },
    {
      title: '今日请求',
      dataIndex: 'today_requests',
      key: 'today_requests',
      render: (val: number, record: Account) => (
        <div style={{ lineHeight: 1.4 }}>
          <Typography.Text strong style={{ fontSize: 13 }}>{val}</Typography.Text>
          <br />
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>累计 {record.total_requests}</Typography.Text>
        </div>
      ),
    },
    {
      title: '今日 Token',
      dataIndex: 'today_tokens',
      key: 'today_tokens',
      render: (val: number, record: Account) => (
        <div style={{ lineHeight: 1.4 }}>
          <Typography.Text strong style={{ fontSize: 13 }}>{fmtTokens(val)}</Typography.Text>
          <br />
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>累计 {fmtTokens(record.total_tokens)}</Typography.Text>
        </div>
      ),
    },
    {
      title: '凭证状态',
      key: 'credential_status',
      render: (_: unknown, record: Account) => {
        const cv = record.credential_valid;
        if (cv === 1) {
          return (
            <Tooltip title={`上次刷新：${record.credential_refreshed_at || record.credential_checked_at || '未知'}`}>
              <Tag color="success" icon={<CheckCircleOutlined />}>有效</Tag>
            </Tooltip>
          );
        }
        if (cv === 0) {
          return (
            <Tooltip title={record.credential_error || '凭证已过期，请使用 OAuth 授权登录重新获取'}>
              <Tag color="error" icon={<CloseCircleOutlined />}>已过期</Tag>
            </Tooltip>
          );
        }
        return (
          <Tooltip title="keepalive 将在启动后 10 分钟内完成首次检测">
            <Tag color="processing" icon={<ClockCircleOutlined />}>首次检测中</Tag>
          </Tooltip>
        );
      },
    },
    {
      title: '状态',
      dataIndex: 'is_default',
      key: 'is_default',
      render: (val: boolean) => val ? <Tag color="blue"><StarOutlined /> 默认账号</Tag> : null,
    },
    {
      title: '默认模型',
      dataIndex: 'default_model',
      key: 'default_model',
      render: (val: string) => val ? <Tag color="green">{val}</Tag> : <Typography.Text type="secondary">未设置</Typography.Text>,
    },
    {
      title: '快速启动',
      key: 'quickstart',
      width: 90,
      render: (_: unknown, record: Account) => {
        const claudeCmd = claudeCodeCmd(record.api_token, record.default_model || undefined);
        const cxCmd = codexCmd(record.api_token, record.default_model || undefined);
        return (
          <Space size={4}>
            <CommandTooltip command={claudeCmd} label="Claude Code">
              <Button
                type="text"
                size="small"
                icon={<SvgClaudeCode />}
                onClick={(e) => { e.stopPropagation(); copyToClipboard(claudeCmd, 'Claude Code'); }}
              />
            </CommandTooltip>
            <CommandTooltip command={cxCmd} label="Codex">
              <Button
                type="text"
                size="small"
                icon={<SvgCodex />}
                onClick={(e) => { e.stopPropagation(); copyToClipboard(cxCmd, 'Codex'); }}
              />
            </CommandTooltip>
          </Space>
        );
      },
    },
    {
      title: '操作',
      key: 'actions',
      render: (_: unknown, record: Account) => (
        <Space>
          <Button size="small" onClick={(e) => {
            e.stopPropagation();
            setRenameTarget(record.user_id);
            renameForm.setFieldsValue({ new_name: record.remark || accountDisplayName(record) });
            setRenameModalOpen(true);
          }}>
            <EditOutlined /> 备注
          </Button>
          {!record.is_default && (
            <Button size="small" onClick={(e) => { e.stopPropagation(); handleSetDefault(record.user_id, accountDisplayName(record)); }}>
              <StarOutlined /> 设为默认
            </Button>
          )}
          <Popconfirm
            title="确定要重置 API Token 吗？"
            description="重置后旧 Token 将立即失效"
            onConfirm={() => handleRenewToken(record.user_id)}
          >
            <Button size="small" onClick={(e) => e.stopPropagation()}>重置 Token</Button>
          </Popconfirm>
          <Button
            size="small"
            onClick={(e) => { e.stopPropagation(); handleValidate(record.user_id, accountDisplayName(record)); }}
            loading={validating === record.user_id}
          >
            <SafetyCertificateOutlined /> 验证
          </Button>
          <Popconfirm
            title={`确定要删除账号「${accountDisplayName(record)}」吗？`}
            description="删除后使用该密钥的客户端将无法访问"
            onConfirm={() => handleRemove(record.user_id, accountDisplayName(record))}
          >
            <Button size="small" danger onClick={(e) => e.stopPropagation()}><DeleteOutlined /> 删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>账号管理</Typography.Title>
        <Space>
          <Button onClick={fetchAccounts} icon={<ReloadOutlined />}>刷新</Button>
          <Button
            onClick={async () => {
              try {
                const result = await api.browserLogin();
                window.open(result.url, '_blank');
                message.info('请在打开的页面中完成 OAuth 授权，授权成功后会自动同步到此处');
                setTimeout(() => fetchAccounts(), 10000);
              } catch (e: unknown) {
                message.error(e instanceof Error ? e.message : '获取登录链接失败');
              }
            }}
            icon={<SafetyCertificateOutlined />}
          >
            OAuth授权登录
          </Button>
          <Button
            type="primary"
            onClick={handleAutoLogin}
            loading={autoLogging}
            icon={<SafetyCertificateOutlined />}
          >
            一键导入本地JoyCode已登录账户
          </Button>
          <Popconfirm
            title="确定要清空本地 JoyCode IDE 的登录会话吗？"
            description="清除后 JoyCode IDE 将需要重新登录，此操作不影响已导入的账号"
            onConfirm={async () => {
              try {
                const result = await api.clearJoyCodeSession();
                message.success(result.message || 'JoyCode 本地会话已清除');
              } catch (e: unknown) {
                message.error(e instanceof Error ? e.message : '清除会话失败');
              }
            }}
          >
            <Button danger icon={<ClearOutlined />}>
              清空本地JoyCode会话
            </Button>
          </Popconfirm>
          <Button onClick={() => setModalOpen(true)} icon={<PlusOutlined />}>
            手动添加
          </Button>
        </Space>
      </div>
      <Alert
        type="info"
        showIcon
        message="多账号路由说明"
        description="每个账号对应一个 JoyCode 后端凭证。客户端通过 API Token 来指定使用哪个账号。配置 Claude Code 时，将 API Token 填入 ANTHROPIC_API_KEY 环境变量即可。拖动行可调整排序。"
        style={{ marginBottom: 16 }}
      />

      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragEnd={handleDragEnd}
      >
        <SortableContext
          items={accounts.map((a) => a.user_id)}
          strategy={verticalListSortingStrategy}
        >
          <Table
            dataSource={accounts}
            columns={columns}
            rowKey="user_id"
            loading={loading}
            pagination={false}
            components={{
              body: {
                row: DraggableRow,
              },
            }}
            onRow={(record) => ({
              onClick: () => navigate(`/accounts/${encodeURIComponent(record.user_id)}`),
            })}
            locale={{ emptyText: '暂无账号，请点击「一键导入」或「OAuth授权登录」按钮配置您的第一个 JoyCode 账号' }}
          />
        </SortableContext>
      </DndContext>

      <Modal
        title="手动添加 JoyCode 账号"
        open={modalOpen}
        onCancel={() => { setModalOpen(false); form.resetFields(); }}
        onOk={() => form.submit()}
        okText="添加"
        cancelText="取消"
        width={560}
      >
        <Alert
          type="info"
          showIcon
          message="手动添加账号"
          description="填写 JoyCode 客户端凭证信息。推荐使用「一键导入」自动导入本地已登录账户，此处适合手动配置多个账号。"
          style={{ marginBottom: 16 }}
        />
        <Form form={form} layout="vertical" onFinish={handleAdd}>
          <Form.Item
            name="pt_key"
            label={
              <Space size={4}>
                JoyCode ptKey 凭证
                <Tooltip title="从 JoyCode 客户端获取的 ptKey，用于后端 API 认证。获取方式：打开 JoyCode 桌面客户端 → 设置 → 开发者 → 复制 ptKey。凭证将以加密形式存储在本地数据库中">
                  <QuestionCircleOutlined style={{ color: '#999' }} />
                </Tooltip>
              </Space>
            }
            rules={[{ required: true, message: '请输入 ptKey' }]}
          >
            <Input.Password placeholder="粘贴从 JoyCode 客户端复制的 ptKey，例如：eyJhbGci..." />
          </Form.Item>
          <Form.Item
            name="user_id"
            label={
              <Space size={4}>
                JoyCode 用户 ID
                <Tooltip title="与 ptKey 对应的用户 ID。获取方式：打开 JoyCode 桌面客户端 → 设置 → 个人信息 → 复制用户 ID">
                  <QuestionCircleOutlined style={{ color: '#999' }} />
                </Tooltip>
              </Space>
            }
            rules={[{ required: true, message: '请输入用户 ID' }]}
          >
            <Input placeholder="例如：user-12345 或从 JoyCode 客户端复制" />
          </Form.Item>
          <Form.Item
            name="default_model"
            label={
              <Space size={4}>
                默认模型
                <Tooltip title="此账号使用的默认模型。留空则使用系统全局默认模型。添加账号后，可在账号列表中实时获取该账号支持的全部模型">
                  <QuestionCircleOutlined style={{ color: '#999' }} />
                </Tooltip>
              </Space>
            }
          >
            <Select
              placeholder="留空使用系统默认模型"
              options={BUILTIN_MODELS}
              allowClear
            />
          </Form.Item>
          <Form.Item
            name="is_default"
            valuePropName="checked"
            label={
              <Space size={4}>
                设为默认账号
                <Tooltip title="当客户端未提供路由密钥时，请求将自动路由到此默认账号。建议将最常用的账号设为默认">
                  <QuestionCircleOutlined style={{ color: '#999' }} />
                </Tooltip>
              </Space>
            }
          >
            <Switch />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="修改账号备注"
        open={renameModalOpen}
        onCancel={() => { setRenameModalOpen(false); renameForm.resetFields(); }}
        onOk={() => renameForm.submit()}
        okText="确认"
        cancelText="取消"
      >
        <Form form={renameForm} layout="vertical" onFinish={handleRename}>
          <Form.Item
            name="new_name"
            label="备注名"
            rules={[{ required: true, message: '请输入备注名' }]}
          >
            <Input placeholder="输入备注名，例如：我的主账号" />
          </Form.Item>
        </Form>
      </Modal>

      <QRLoginModal
        open={qrModalOpen}
        onClose={() => setQrModalOpen(false)}
        onSuccess={fetchAccounts}
        onAutoLogin={handleAutoLogin}
      />
    </div>
  );
};

export default Accounts;
```

- [ ] **Step 5: 验证前端编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npx tsc --noEmit`
Expected:
  - Exit code: 0
  - Output does NOT contain: "error"

- [ ] **Step 6: 提交**
Run: `git add web/src/api.ts web/src/pages/Accounts.tsx web/package.json web/package-lock.json && git commit -m "feat(accounts): add drag-and-drop sorting with persistent display_order"`

---

### Task 4: 构建部署验证

**Depends on:** Task 3
**Files:**
- Modify: (无代码修改，构建验证)

- [ ] **Step 1: 构建前端资源**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build`
Expected:
  - Exit code: 0
  - Output contains: "built in"

- [ ] **Step 2: 构建 Go 二进制**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0
  - Output does NOT contain: "error"

- [ ] **Step 3: 部署并重启服务**
Run: `cp /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/joycode_proxy_bin ~/.joycode-proxy/joycode_proxy_bin && launchctl unload ~/Library/LaunchAgents/com.joycode.proxy.plist && launchctl load ~/Library/LaunchAgents/com.joycode.proxy.plist`
Expected:
  - Exit code: 0
  - Service responds to health check

- [ ] **Step 4: 验证 display_order migration 和 API**
Run: `sleep 2 && curl -sk -H "Authorization: Bearer $(cat ~/.joycode-proxy/jwt_token 2>/dev/null || echo '')" https://127.0.0.1:34891/api/accounts | python3 -m json.tool | head -30`
Expected:
  - Exit code: 0
  - Output contains: "display_order"
  - Accounts are returned with display_order values

- [ ] **Step 5: 验证 reorder API**
Run: `curl -sk -X PUT -H "Authorization: Bearer $(cat ~/.joycode-proxy/jwt_token 2>/dev/null || echo '')" -H "Content-Type: application/json" -d '{"user_ids":["test-reorder"]}' https://127.0.0.1:34891/api/accounts/reorder`
Expected:
  - Returns JSON response (may be error if test user doesn't exist, but endpoint should be reachable, not 404)
