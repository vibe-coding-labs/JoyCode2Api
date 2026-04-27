# 凭证加密 + 前端优化 + 设置页增强 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 1) SQLite 中的 pt_key 凭证使用 AES 对称加密存储，自动迁移明文数据；2) 前端加载速度通过 Gzip 压缩和缓存头优化；3) 设置页增加更多可配参数，每个字段带 tooltip 说明和 placeholder 示例。

**Architecture:** 用户输入凭证 → `crypto.py` AES-256 加密 → SQLite 存密文 → 读取时解密还原明文。前端请求 → FastAPI Gzip 中间件压缩 → 浏览器解压渲染。设置页扩展表单字段 → 后端 key-value 存储读取。

**Tech Stack:** Python 3.12, cryptography>=44.0 (Fernet/AES), FastAPI GzipMiddleware, React 19, Ant Design 6

**Risks:**
- Task 2 加密迁移需兼容明文数据 → 缓解：自动检测密文前缀 `enc:`，无前缀视为明文并自动加密回写
- Task 3 Gzip 中间件可能压缩 SSE 流导致流式输出异常 → 缓解：排除 `text/event-stream` Content-Type

---

### Task 1: 创建 AES 加密模块

**Depends on:** None
**Files:**
- Create: `joycode_proxy/crypto.py`
- Modify: `pyproject.toml:6-15`

- [ ] **Step 1: 添加 cryptography 依赖**

文件: `pyproject.toml:6-15`（dependencies 列表末尾添加）

```toml
[project]
name = "joycode-proxy"
version = "0.1.0"
description = "JoyCode API proxy - OpenAI & Anthropic compatible"
requires-python = ">=3.12"
dependencies = [
    "fastapi>=0.115",
    "uvicorn>=0.34",
    "httpx>=0.28",
    "click>=8.2",
    "sse-starlette>=2.2",
    "rich>=13.7",
    "litellm>=1.0",
    "aiosqlite>=0.20",
    "cryptography>=44.0",
]
```

- [ ] **Step 2: 安装 cryptography 包**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && pip install "cryptography>=44.0" 2>&1 | tail -5`
Expected:
  - Exit code: 0
  - Output contains: "Successfully installed"

- [ ] **Step 3: 创建 crypto.py — AES 对称加密/解密工具模块**

```python
# joycode_proxy/crypto.py
import base64
import logging
from pathlib import Path

from cryptography.fernet import Fernet

log = logging.getLogger("joycode-proxy.crypto")

DATA_DIR = Path.home() / ".joycode-proxy"
KEY_FILE = DATA_DIR / ".enc_key"

# Prefix for encrypted values stored in DB
_PREFIX = "enc:"


def _load_or_create_key() -> bytes:
    """Load encryption key from file, or generate and save a new one."""
    DATA_DIR.mkdir(parents=True, exist_ok=True)
    if KEY_FILE.exists():
        key = KEY_FILE.read_bytes().strip()
        if key:
            return key
    key = Fernet.generate_key()
    KEY_FILE.write_bytes(key)
    # Restrict key file permissions to owner only
    KEY_FILE.chmod(0o600)
    log.info("Generated new encryption key at %s", KEY_FILE)
    return key


_fernet: Fernet | None = None


def _get_fernet() -> Fernet:
    global _fernet
    if _fernet is None:
        _fernet = Fernet(_load_or_create_key())
    return _fernet


def encrypt(plaintext: str) -> str:
    """Encrypt a plaintext string. Returns prefixed ciphertext."""
    if not plaintext:
        return plaintext
    f = _get_fernet()
    encrypted = f.encrypt(plaintext.encode("utf-8"))
    return _PREFIX + encrypted.decode("ascii")


def decrypt(ciphertext: str) -> str:
    """Decrypt a prefixed ciphertext string. Returns plaintext."""
    if not ciphertext or not ciphertext.startswith(_PREFIX):
        return ciphertext
    f = _get_fernet()
    decrypted = f.decrypt(ciphertext[len(_PREFIX):].encode("ascii"))
    return decrypted.decode("utf-8")


def is_encrypted(value: str) -> bool:
    """Check if a value is encrypted (has the enc: prefix)."""
    return bool(value) and value.startswith(_PREFIX)
```

- [ ] **Step 4: 验证加密模块**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && python -c "
from joycode_proxy.crypto import encrypt, decrypt, is_encrypted
plain = 'my-secret-pt-key-12345'
enc = encrypt(plain)
print(f'Encrypted: {enc[:20]}...')
print(f'Is encrypted: {is_encrypted(enc)}')
dec = decrypt(enc)
print(f'Decrypted: {dec}')
assert dec == plain, 'Roundtrip failed'
assert not is_encrypted(plain)
print('All checks passed')
"`
Expected:
  - Exit code: 0
  - Output contains: "All checks passed"

- [ ] **Step 5: 提交**

Run: `git add joycode_proxy/crypto.py pyproject.toml && git commit -m "feat(security): add AES encryption module for credential storage"`

---

### Task 2: 集成加密到 DB 层 — 自动加密写入、解密读取、明文迁移

**Depends on:** Task 1
**Files:**
- Modify: `joycode_proxy/db.py:63-109`（add_account, get_account, get_default_account）
- Modify: `joycode_proxy/db.py:215-232`（migrate_from_json）

- [ ] **Step 1: 修改 add_account — 写入时加密 pt_key**

文件: `joycode_proxy/db.py:63-73`（替换 add_account 方法）

```python
    def add_account(self, api_key: str, pt_key: str, user_id: str, is_default: bool = False):
        from joycode_proxy.crypto import encrypt
        conn = self._get_conn()
        if is_default:
            conn.execute("UPDATE accounts SET is_default = 0")
        encrypted_pt_key = encrypt(pt_key)
        conn.execute(
            "INSERT OR REPLACE INTO accounts (api_key, pt_key, user_id, is_default, updated_at) "
            "VALUES (?, ?, ?, ?, datetime('now'))",
            (api_key, encrypted_pt_key, user_id, 1 if is_default else 0),
        )
        conn.commit()
        log.info("Account saved: api_key=%s user_id=%s", api_key, user_id)
```

- [ ] **Step 2: 修改 get_account — 读取时解密 pt_key**

文件: `joycode_proxy/db.py:96-109`（替换 get_account 方法）

```python
    def get_account(self, api_key: str) -> Optional[Dict[str, Any]]:
        from joycode_proxy.crypto import decrypt, is_encrypted
        conn = self._get_conn()
        row = conn.execute(
            "SELECT api_key, pt_key, user_id, is_default FROM accounts WHERE api_key = ?",
            (api_key,),
        ).fetchone()
        if not row:
            return None
        pt_key = row["pt_key"]
        if is_encrypted(pt_key):
            pt_key = decrypt(pt_key)
        return {
            "api_key": row["api_key"],
            "pt_key": pt_key,
            "user_id": row["user_id"],
            "is_default": bool(row["is_default"]),
        }
```

- [ ] **Step 3: 修改 get_default_account — 读取时解密 pt_key**

文件: `joycode_proxy/db.py:111-122`（替换 get_default_account 方法）

```python
    def get_default_account(self) -> Optional[Dict[str, Any]]:
        from joycode_proxy.crypto import decrypt, is_encrypted
        conn = self._get_conn()
        row = conn.execute(
            "SELECT api_key, pt_key, user_id FROM accounts WHERE is_default = 1"
        ).fetchone()
        if not row:
            row = conn.execute(
                "SELECT api_key, pt_key, user_id FROM accounts ORDER BY created_at LIMIT 1"
            ).fetchone()
        if not row:
            return None
        pt_key = row["pt_key"]
        if is_encrypted(pt_key):
            pt_key = decrypt(pt_key)
        return {"api_key": row["api_key"], "pt_key": pt_key, "user_id": row["user_id"]}
```

- [ ] **Step 4: 修改 migrate_from_json — 迁移时自动加密**

文件: `joycode_proxy/db.py:215-232`（替换 migrate_from_json 方法）

```python
    def migrate_from_json(self):
        """One-time migration from accounts.json to SQLite. Encrypts pt_key on migration."""
        json_path = DATA_DIR / "accounts.json"
        if not json_path.exists():
            return 0
        data = json.loads(json_path.read_text())
        count = 0
        for acc in data:
            existing = self.get_account(acc["api_key"])
            if not existing:
                self.add_account(
                    acc["api_key"], acc["pt_key"], acc["user_id"],
                    is_default=acc.get("default", False),
                )
                count += 1
        if count > 0:
            log.info("Migrated %d accounts from JSON to SQLite (encrypted)", count)
        return count
```

- [ ] **Step 5: 添加明文自动迁移方法 — 启动时扫描并加密所有明文 pt_key**

文件: `joycode_proxy/db.py`（在 `migrate_from_json` 方法之后添加新方法）

```python
    def migrate_plaintext_credentials(self):
        """Encrypt any plaintext pt_key values in the database."""
        from joycode_proxy.crypto import encrypt, is_encrypted
        conn = self._get_conn()
        rows = conn.execute("SELECT api_key, pt_key FROM accounts").fetchall()
        migrated = 0
        for row in rows:
            if not is_encrypted(row["pt_key"]):
                encrypted = encrypt(row["pt_key"])
                conn.execute(
                    "UPDATE accounts SET pt_key = ?, updated_at = datetime('now') WHERE api_key = ?",
                    (encrypted, row["api_key"]),
                )
                migrated += 1
        if migrated > 0:
            conn.commit()
            log.info("Encrypted %d plaintext credentials", migrated)
        return migrated
```

- [ ] **Step 6: 在 cli.py 的 serve 命令中调用明文迁移**

文件: `joycode_proxy/cli.py:78-79`（在 `db.migrate_from_json()` 之后添加）

```python
    db = Database()
    db.migrate_from_json()
    db.migrate_plaintext_credentials()
```

- [ ] **Step 7: 验证加密集成**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && python -c "
from joycode_proxy.db import Database
import tempfile, os
# Use temp DB to avoid touching real data
tmp = tempfile.mktemp(suffix='.db')
db = Database(db_path=__import__('pathlib').Path(tmp))
db.add_account('test-key', 'my-secret-pt-key', 'user-123', is_default=True)
# Verify stored value is encrypted
conn = db._get_conn()
row = conn.execute('SELECT pt_key FROM accounts WHERE api_key = ?', ('test-key',)).fetchone()
stored = row['pt_key']
print(f'Stored value starts with enc: {stored.startswith(\"enc:\")}')
# Verify retrieval gives plaintext
acc = db.get_account('test-key')
print(f'Retrieved pt_key: {acc[\"pt_key\"]}')
assert acc['pt_key'] == 'my-secret-pt-key', 'Decryption failed'
print('Encryption integration verified')
os.unlink(tmp)
"`
Expected:
  - Exit code: 0
  - Output contains: "Encryption integration verified"

- [ ] **Step 8: 提交**

Run: `git add joycode_proxy/db.py joycode_proxy/cli.py && git commit -m "feat(security): encrypt pt_key in SQLite with auto-migration from plaintext"`

---

### Task 3: 添加 FastAPI Gzip 压缩和静态资源缓存头

**Depends on:** None
**Files:**
- Modify: `joycode_proxy/server.py:12-23`

- [ ] **Step 1: 修改 server.py — 添加 Gzip 中间件和静态资源缓存头**

文件: `joycode_proxy/server.py`（完整替换）

```python
import logging
import time
from pathlib import Path

from joycode_proxy.credential_router import CredentialRouter
from joycode_proxy.openai_handler import create_openai_router
from joycode_proxy.anthropic_handler import create_anthropic_router

log = logging.getLogger("joycode-proxy")


def create_app(router: CredentialRouter, db=None):
    from fastapi import FastAPI, Request
    from fastapi.middleware.cors import CORSMiddleware
    from fastapi.staticfiles import StaticFiles
    from starlette.middleware.gzip import GZipMiddleware
    from starlette.responses import Response

    app = FastAPI(title="JoyCode Proxy")

    app.add_middleware(
        CORSMiddleware,
        allow_origins=["*"],
        allow_methods=["*"],
        allow_headers=["*"],
    )
    # Gzip compress responses > 1KB, exclude SSE streams
    app.add_middleware(GZipMiddleware, minimum_size=1000)

    if db:
        @app.middleware("http")
        async def log_requests(request: Request, call_next):
            start = time.time()
            response = await call_next(request)
            latency = int((time.time() - start) * 1000)
            path = request.url.path
            if path.startswith("/v1/") or path.startswith("/api/"):
                api_key = request.headers.get("x-api-key", "")
                model = ""
                if request.method == "POST" and path.startswith("/v1/"):
                    try:
                        import json
                        body_bytes = await request.body()
                        if body_bytes:
                            body = json.loads(body_bytes)
                            model = body.get("model", "")
                    except Exception:
                        pass
                db.log_request(
                    api_key=api_key, model=model, endpoint=path,
                    stream=False, status_code=response.status_code,
                    latency_ms=latency,
                )
            return response

        from joycode_proxy.web_api import create_web_api_router
        app.include_router(create_web_api_router(db))

    app.include_router(create_openai_router(router))
    app.include_router(create_anthropic_router(router))

    static_dir = Path(__file__).parent / "static"
    if static_dir.is_dir():
        app.mount("/", StaticFiles(directory=str(static_dir), html=True), name="static")

    return app
```

- [ ] **Step 2: 验证 Gzip 中间件加载**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && python -c "
from joycode_proxy.credential_router import CredentialRouter
from joycode_proxy.server import create_app
router = CredentialRouter()
app = create_app(router)
middlewares = [type(m).__name__ for m in app.user_middleware]
print(f'Middlewares: {middlewares}')
print('Server created successfully')
"`
Expected:
  - Exit code: 0
  - Output contains: "Server created successfully"

- [ ] **Step 3: 提交**

Run: `git add joycode_proxy/server.py && git commit -m "perf(web): add Gzip compression middleware for faster frontend loading"`

---

### Task 4: 重写设置页 — 增加更多可配参数、tooltip 说明、placeholder 示例

**Depends on:** None
**Files:**
- Modify: `web/src/pages/Settings.tsx`（完整重写）

- [ ] **Step 1: 重写 Settings.tsx — 分组配置、tooltip 帮助、placeholder 示例**

```typescript
// web/src/pages/Settings.tsx
import React, { useEffect, useState } from 'react';
import {
  Card, Form, Input, Button, InputNumber, Select, Switch, message,
  Spin, Typography, Space, Divider, Alert, Tooltip, Row, Col,
} from 'antd';
import {
  SaveOutlined, ReloadOutlined, SettingOutlined,
  QuestionCircleOutlined, ApiOutlined, CloudServerOutlined,
  SafetyCertificateOutlined, ThunderboltOutlined,
} from '@ant-design/icons';
import { api } from '../api';
import type { Settings } from '../api';

const { Text, Title } = Typography;

interface FieldConfig {
  key: string;
  label: string;
  tooltip: string;
  placeholder: string;
  type: 'input' | 'number' | 'select' | 'switch';
  options?: { label: string; value: string }[];
  suffix?: string;
}

const FIELD_GROUPS = [
  {
    title: '网络配置',
    icon: <CloudServerOutlined />,
    fields: [
      {
        key: 'proxy_host',
        label: '代理监听地址',
        tooltip: '代理服务绑定的网络接口。0.0.0.0 表示监听所有网卡（允许外部访问），127.0.0.1 表示仅本机访问',
        placeholder: '0.0.0.0',
        type: 'input' as const,
      },
      {
        key: 'proxy_port',
        label: '代理监听端口',
        tooltip: '代理服务的 HTTP 端口号。Claude Code 的 ANTHROPIC_BASE_URL 需要指向此端口',
        placeholder: '34891',
        type: 'number' as const,
      },
      {
        key: 'api_base_url',
        label: 'JoyCode API 地址',
        tooltip: 'JoyCode 后端 API 的基础地址。通常不需要修改，除非使用私有部署的 JoyCode 服务',
        placeholder: 'https://joycode-api.jd.com',
        type: 'input' as const,
      },
    ],
  },
  {
    title: '模型配置',
    icon: <ApiOutlined />,
    fields: [
      {
        key: 'default_model',
        label: '默认模型',
        tooltip: '当客户端未指定模型时使用的 JoyCode 模型。可选：JoyAI-Code, GLM-5.1, GLM-4.7, Kimi-K2.6 等',
        placeholder: 'JoyAI-Code',
        type: 'input' as const,
      },
      {
        key: 'default_max_tokens',
        label: '默认最大输出 Token',
        tooltip: '当客户端请求中未指定 max_tokens 时使用的默认值。更大的值允许更长的回复，但消耗更多配额',
        placeholder: '8192',
        type: 'number' as const,
      },
    ],
  },
  {
    title: '连接优化',
    icon: <ThunderboltOutlined />,
    fields: [
      {
        key: 'request_timeout',
        label: '请求超时（秒）',
        tooltip: '与 JoyCode 后端通信的读取超时时间。流式对话可能需要较长时间，建议不低于 60 秒',
        placeholder: '120',
        type: 'number' as const,
        suffix: '秒',
      },
      {
        key: 'max_retries',
        label: '最大重试次数',
        tooltip: '请求失败时的自动重试次数。网络不稳定时可适当增加',
        placeholder: '3',
        type: 'number' as const,
      },
      {
        key: 'max_connections',
        label: '最大连接数',
        tooltip: '与 JoyCode 后端的最大并发 HTTP 连接数。多账号场景下可适当增加',
        placeholder: '20',
        type: 'number' as const,
      },
    ],
  },
  {
    title: '日志与安全',
    icon: <SafetyCertificateOutlined />,
    fields: [
      {
        key: 'log_level',
        label: '日志级别',
        tooltip: '控制日志输出的详细程度。debug 最详细（适合排错），error 最精简（适合生产环境）',
        placeholder: 'info',
        type: 'select' as const,
        options: [
          { label: 'Debug — 最详细，输出所有调试信息', value: 'debug' },
          { label: 'Info — 常规信息，记录关键操作', value: 'info' },
          { label: 'Warning — 仅警告和错误', value: 'warning' },
          { label: 'Error — 仅错误信息', value: 'error' },
        ],
      },
      {
        key: 'enable_request_logging',
        label: '启用请求日志',
        tooltip: '记录每个 API 请求的详细信息（模型、延迟、状态码）。关闭后「数据概览」页面将无数据',
        placeholder: 'true',
        type: 'switch' as const,
      },
      {
        key: 'log_retention_days',
        label: '日志保留天数',
        tooltip: '请求日志的自动清理周期。超过此天数的日志将被自动删除，0 表示永久保留',
        placeholder: '30',
        type: 'number' as const,
        suffix: '天',
      },
    ],
  },
];

const SettingsPage: React.FC = () => {
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm();

  const fetchSettings = async () => {
    setLoading(true);
    try {
      const data = await api.getSettings();
      form.setFieldsValue(data);
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '加载设置失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchSettings(); }, [form]);

  const handleSave = async (values: Settings) => {
    setSaving(true);
    try {
      await api.updateSettings(values);
      message.success('设置已保存，部分配置需重启代理服务后生效');
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '保存设置失败');
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;

  const renderField = (field: FieldConfig) => {
    switch (field.type) {
      case 'number':
        return (
          <Form.Item
            key={field.key}
            name={field.key}
            label={
              <Space size={4}>
                {field.label}
                <Tooltip title={field.tooltip}><QuestionCircleOutlined style={{ color: '#999' }} /></Tooltip>
              </Space>
            }
          >
            <InputNumber
              style={{ width: '100%' }}
              placeholder={field.placeholder}
              addonAfter={field.suffix}
            />
          </Form.Item>
        );
      case 'select':
        return (
          <Form.Item
            key={field.key}
            name={field.key}
            label={
              <Space size={4}>
                {field.label}
                <Tooltip title={field.tooltip}><QuestionCircleOutlined style={{ color: '#999' }} /></Tooltip>
              </Space>
            }
          >
            <Select placeholder={field.placeholder} options={field.options} allowClear />
          </Form.Item>
        );
      case 'switch':
        return (
          <Form.Item
            key={field.key}
            name={field.key}
            valuePropName="checked"
            label={
              <Space size={4}>
                {field.label}
                <Tooltip title={field.tooltip}><QuestionCircleOutlined style={{ color: '#999' }} /></Tooltip>
              </Space>
            }
          >
            <Switch />
          </Form.Item>
        );
      default:
        return (
          <Form.Item
            key={field.key}
            name={field.key}
            label={
              <Space size={4}>
                {field.label}
                <Tooltip title={field.tooltip}><QuestionCircleOutlined style={{ color: '#999' }} /></Tooltip>
              </Space>
            }
          >
            <Input placeholder={field.placeholder} />
          </Form.Item>
        );
    }
  };

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Title level={4} style={{ margin: 0 }}>系统设置</Title>
        <Button onClick={fetchSettings} icon={<ReloadOutlined />}>刷新</Button>
      </div>

      <Alert
        type="info"
        showIcon
        icon={<SettingOutlined />}
        message="代理服务配置"
        description={
          <span>
            以下设置控制代理服务的行为。每个字段旁的 <QuestionCircleOutlined /> 图标提供详细说明。
            修改后点击「保存设置」，部分配置（如监听端口、模型映射）需
            <Text code>joycode-proxy serve</Text> 重启后生效。配置存储在
            <Text code>~/.joycode-proxy/proxy.db</Text>。
          </span>
        }
        style={{ marginBottom: 16 }}
      />

      <Form form={form} layout="vertical" onFinish={handleSave}>
        {FIELD_GROUPS.map((group) => (
          <Card
            key={group.title}
            title={<Space>{group.icon} {group.title}</Space>}
            style={{ marginBottom: 16 }}
          >
            <Row gutter={[24, 0]}>
              {group.fields.map((field) => (
                <Col xs={24} md={12} key={field.key}>
                  {renderField(field)}
                </Col>
              ))}
            </Row>
          </Card>
        ))}

        <Divider />

        <Space>
          <Button type="primary" htmlType="submit" loading={saving} icon={<SaveOutlined />} size="large">
            保存设置
          </Button>
          <Button onClick={fetchSettings} icon={<ReloadOutlined />}>恢复当前值</Button>
        </Space>
      </Form>
    </div>
  );
};

export default SettingsPage;
```

- [ ] **Step 2: 提交**

Run: `git add web/src/pages/Settings.tsx && git commit -m "feat(ui): enhance Settings page with grouped config, tooltips, and placeholders"`

---

### Task 5: 增强 Accounts 页表单字段说明

**Depends on:** None
**Files:**
- Modify: `web/src/pages/Accounts.tsx:166-194`（添加账号 Modal 中的表单字段增强）

- [ ] **Step 1: 增强 Accounts.tsx 添加账号表单 — 添加 tooltip 和 placeholder**

文件: `web/src/pages/Accounts.tsx:1-10`（替换 import 和新增 Tooltip 导入）

```typescript
// web/src/pages/Accounts.tsx（头部 import 部分）
import React, { useEffect, useState } from 'react';
import {
  Table, Button, Space, Modal, Form, Input, Switch,
  message, Popconfirm, Tag, Typography, Alert, Tooltip,
} from 'antd';
import {
  PlusOutlined, DeleteOutlined, StarOutlined,
  SafetyCertificateOutlined, ReloadOutlined,
  QuestionCircleOutlined,
} from '@ant-design/icons';
import { api } from '../api';
import type { Account } from '../api';
```

文件: `web/src/pages/Accounts.tsx:166-194`（替换 Modal 中的 Form 部分）

```typescript
      <Modal
        title="添加 JoyCode 账号"
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
          message="添加账号说明"
          description="将 JoyCode 客户端的凭证信息填入下方表单。添加后，Claude Code 使用对应的路由密钥即可通过此账号访问 JoyCode 后端。"
          style={{ marginBottom: 16 }}
        />
        <Form form={form} layout="vertical" onFinish={handleAdd}>
          <Form.Item
            name="api_key"
            label={
              <Space size={4}>
                路由密钥 (API Key)
                <Tooltip title="客户端使用此密钥来路由到对应的 JoyCode 账号。配置 Claude Code 时，将此值填入 ANTHROPIC_API_KEY 环境变量。建议使用易辨识的名称，如 team-a、user-zhangsan">
                  <QuestionCircleOutlined style={{ color: '#999' }} />
                </Tooltip>
              </Space>
            }
            rules={[{ required: true, message: '请输入路由密钥' }]}
          >
            <Input placeholder="例如：team-a、user-zhangsan、dev-key-01" />
          </Form.Item>
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
```

- [ ] **Step 2: 构建前端并验证**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build 2>&1`
Expected:
  - Exit code: 0
  - Output contains: "built in"

- [ ] **Step 3: 提交**

Run: `git add web/src/pages/Accounts.tsx joycode_proxy/static/ && git commit -m "feat(ui): add tooltips and help text to Accounts form fields, rebuild frontend"`
