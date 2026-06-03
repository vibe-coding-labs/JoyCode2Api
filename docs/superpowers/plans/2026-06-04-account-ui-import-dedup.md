# Account Name Column Width + Import Dedup Fix

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 修复账号管理页面账户名列显示不全的问题（120px → 200px），并增强导入去重逻辑：当相同 pt_key 凭证以不同 user_id 导入时，自动更新已有记录而不是创建重复账号。

**Architecture:** 前端直接加宽列宽；后端在 AddAccount 和 ImportAccounts 中增加 pt_key 去重检查——先解密已有账号的 pt_key，如果发现匹配则按已有 user_id 更新而非插入新记录。pt_key 是实际凭证标识，比 user_id 更可靠。

**Tech Stack:** Go 1.22, SQLite 3, React 18, Ant Design 5

**Risks:**
- pt_key 在数据库中是加密存储的，去重时需要解密比较，增加少量锁持有时间 → 缓解：只在账号数 ≤ 10 时遍历，性能影响可忽略
- 如果用户有意用同一 pt_key 创建多个账号（不同 user_id），新逻辑会合并为一个 → 缓解：打印日志说明合并行为

---

### Task 1: 加宽账户名列宽度

**Depends on:** None
**Files:**
- Modify: `web/src/pages/Accounts.tsx:299-307`（账户名列定义）

- [ ] **Step 1: 修改账户名列宽度 — 从 120px 加宽到 200px**

文件: `web/src/pages/Accounts.tsx:299-307`

```typescript
    {
      title: '账户名',
      dataIndex: 'user_id',
      key: 'user_id',
      width: 200,
      ellipsis: true,
      render: (_: unknown, record: Account) => (
        <Tooltip title={accountDisplayName(record)} placement="topLeft">
          <Typography.Text strong>{accountDisplayName(record)}</Typography.Text>
        </Tooltip>
      );
    },
```

- [ ] **Step 2: 重新构建前端并验证**
Run: `cd /home/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build`
Expected:
  - Exit code: 0
  - Output does NOT contain: "ERROR" or "Build failed"

- [ ] **Step 3: 提交**
Run: `cd /home/cc11001100/github/vibe-coding-labs/JoyCodeProxy && git add web/src/pages/Accounts.tsx && git commit -m "fix(ui): widen account name column and add ellipsis tooltip"`

---

### Task 2: 增强 AddAccount 的 pt_key 去重检查

**Depends on:** None
**Files:**
- Modify: `pkg/store/store.go:499-568`（AddAccount 函数）

- [ ] **Step 1: 在 AddAccount 中添加 pt_key 去重逻辑 — 同一凭证不同 user_id 时自动合并**

在 `AddAccount` 函数中，当 user_id 不匹配时，额外检查是否有相同 pt_key 的已有账号。如果有，更新该已有账号的 pt_key 和 user_id 信息。

文件: `pkg/store/store.go:499-568`（替换整个 AddAccount 函数）

```go
func (s *Store) AddAccount(userID, ptKey, nickname string, isDefault bool, defaultModel string) error {
	if userID == "" {
		return fmt.Errorf("user_id cannot be empty")
	}
	if ptKey == "" {
		return fmt.Errorf("pt_key cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if account already exists by user_id — updates bypass the limit
	var existingToken string
	err := s.db.QueryRow("SELECT api_token FROM accounts WHERE user_id = ?", userID).Scan(&existingToken)
	if err == nil {
		encPtKey, err := s.encrypt(ptKey)
		if err != nil {
			slog.Error("store: encrypt pt_key failed", "user_id", userID, "error", err)
			return fmt.Errorf("encrypt pt_key: %w", err)
		}
		_, err = s.db.Exec(
			"UPDATE accounts SET pt_key = ?, nickname = CASE WHEN nickname = '' OR nickname IS NULL THEN ? ELSE nickname END, updated_at = datetime('now', 'localtime') WHERE user_id = ?",
			encPtKey, nickname, userID,
		)
		if err != nil {
			slog.Error("store: update account failed", "user_id", userID, "error", err)
			return err
		}
		slog.Info("store: updated existing account credentials", "user_id", userID)
		return nil
	}

	// Check if another account already has the same pt_key (dedup by credential)
	rows, err := s.db.Query("SELECT user_id, pt_key FROM accounts")
	if err == nil {
		for rows.Next() {
			var existingUserID, encExistingPtKey string
			if rows.Scan(&existingUserID, &encExistingPtKey) != nil {
				continue
			}
			existingPtKey, decErr := s.decrypt(encExistingPtKey)
			if decErr != nil {
				continue
			}
			if existingPtKey == ptKey {
				rows.Close()
				encPtKey, encErr := s.encrypt(ptKey)
				if encErr != nil {
					slog.Error("store: encrypt pt_key failed", "user_id", userID, "error", encErr)
					return fmt.Errorf("encrypt pt_key: %w", encErr)
				}
				// Update the existing account with new nickname and pt_key
				_, err = s.db.Exec(
					"UPDATE accounts SET user_id = ?, pt_key = ?, nickname = CASE WHEN nickname = '' OR nickname IS NULL THEN ? ELSE nickname END, updated_at = datetime('now', 'localtime') WHERE user_id = ?",
					userID, encPtKey, nickname, existingUserID,
				)
				if err != nil {
					slog.Error("store: update account (pt_key dedup) failed", "old_user_id", existingUserID, "new_user_id", userID, "error", err)
					return err
				}
				slog.Info("store: merged account by pt_key dedup", "old_user_id", existingUserID, "new_user_id", userID)
				return nil
			}
		}
		rows.Close()
	}

	// New account — enforce limit
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM accounts").Scan(&count)
	if count >= MaxAccounts {
		return fmt.Errorf("账号数量已达上限（%d 个）。本工具仅供个人学习和研究使用，禁止用于商业转售、API 中转服务或任何违法违规用途", MaxAccounts)
	}

	encPtKey, err := s.encrypt(ptKey)
	if err != nil {
		slog.Error("store: encrypt pt_key failed", "user_id", userID, "error", err)
		return fmt.Errorf("encrypt pt_key: %w", err)
	}

	// New account
	if isDefault {
		s.db.Exec("UPDATE accounts SET is_default = 0 WHERE is_default = 1")
	}

	def := 0
	if isDefault {
		def = 1
	}

	// Get max display_order
	var maxOrder int
	s.db.QueryRow("SELECT COALESCE(MAX(display_order), 0) FROM accounts").Scan(&maxOrder)

	token := generateToken()
	_, err = s.db.Exec(
		"INSERT INTO accounts (user_id, nickname, api_token, pt_key, is_default, default_model, display_order) VALUES (?, ?, ?, ?, ?, ?, ?)",
		userID, nickname, token, encPtKey, def, defaultModel, maxOrder+1,
	)
	if err != nil {
		slog.Error("store: add account failed", "user_id", userID, "error", err)
		return err
	}
	return nil
}
```

- [ ] **Step 2: 验证编译通过**
Run: `cd /home/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./...`
Expected:
  - Exit code: 0
  - Output does NOT contain: "cannot" or "undefined" or "not used"

- [ ] **Step 3: 提交**
Run: `cd /home/cc11001100/github/vibe-coding-labs/JoyCodeProxy && git add pkg/store/store.go && git commit -m "fix(import): add pt_key dedup check in AddAccount to prevent duplicate accounts"`
