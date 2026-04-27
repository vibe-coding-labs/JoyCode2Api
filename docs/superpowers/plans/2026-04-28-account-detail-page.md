# Account Detail Page & Model Configuration

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 为每个 JoyCode 账号添加可点击进入的详情页面，包含默认模型编辑、请求统计、模型使用分布等功能。

**Architecture:** 用户在账号列表点击行 → 路由跳转 `/accounts/:apiKey` → AccountDetail 页面加载 → 调用 `GET /api/accounts/{key}/stats` 获取该账号统计 → 调用 `PUT /api/accounts/{key}/model` 编辑默认模型。后端复用 `request_logs` 表做 per-account 聚合查询。

**Tech Stack:** Python 3.9+ / FastAPI / SQLite, React 19 / TypeScript / Ant Design 6 / Recharts

**Risks:**
- `request_logs` 中 `api_key` 可能为空（未认证请求）→ SQL 统计时用 `WHERE api_key = ?` 天然过滤
- 详情页中 `apiKey` 通过 URL 参数传递，含特殊字符需 `encodeURIComponent` → 已有 `path` 路由模式支持

---

### Task 1: Backend — 添加账号统计和模型更新 API

**Depends on:** None
**Files:**
- Modify: `joycode_proxy/db.py:206-224`（在 `get_stats()` 之后添加 `get_account_stats()`）
- Modify: `joycode_proxy/db.py:84-90`（在 `add_account()` 之后添加 `update_account_model()`）
- Modify: `joycode_proxy/web_api.py:96-108`（在 health 端点之前添加两个新端点）

- [ ] **Step 1: 在 db.py 添加 `update_account_model()` 方法 — 支持更新账号默认模型**

文件: `joycode_proxy/db.py:84-90`（在 `add_account` 方法之后、`remove_account` 之前）

```python
    def update_account_model(self, api_key: str, default_model: str):
        """Update the default_model for an account."""
        conn = self._get_conn()
        conn.execute(
            "UPDATE accounts SET default_model = ?, updated_at = datetime('now') WHERE api_key = ?",
            (default_model, api_key),
        )
        conn.commit()
        log.info("Updated default_model for %s: %s", api_key, default_model)
```

- [ ] **Step 2: 在 db.py 添加 `get_account_stats()` 方法 — 按账号统计请求日志**

文件: `joycode_proxy/db.py:224-225`（在 `get_stats()` 方法之后、`get_credential_router()` 之前）

```python
    def get_account_stats(self, api_key: str) -> Dict[str, Any]:
        """Get per-account statistics from request_logs."""
        conn = self._get_conn()
        total = conn.execute(
            "SELECT COUNT(*) as cnt FROM request_logs WHERE api_key = ?", (api_key,)
        ).fetchone()["cnt"]
        by_model = conn.execute(
            "SELECT model, COUNT(*) as cnt FROM request_logs WHERE api_key = ? GROUP BY model ORDER BY cnt DESC",
            (api_key,),
        ).fetchall()
        avg_latency = conn.execute(
            "SELECT AVG(latency_ms) as avg FROM request_logs WHERE api_key = ? AND latency_ms > 0",
            (api_key,),
        ).fetchone()["avg"]
        by_endpoint = conn.execute(
            "SELECT endpoint, COUNT(*) as cnt FROM request_logs WHERE api_key = ? GROUP BY endpoint ORDER BY cnt DESC",
            (api_key,),
        ).fetchall()
        stream_count = conn.execute(
            "SELECT COUNT(*) as cnt FROM request_logs WHERE api_key = ? AND stream = 1",
            (api_key,),
        ).fetchone()["cnt"]
        error_count = conn.execute(
            "SELECT COUNT(*) as cnt FROM request_logs WHERE api_key = ? AND status_code >= 400",
            (api_key,),
        ).fetchone()["cnt"]
        recent_logs = conn.execute(
            "SELECT * FROM request_logs WHERE api_key = ? ORDER BY id DESC LIMIT 20",
            (api_key,),
        ).fetchall()
        return {
            "api_key": api_key,
            "total_requests": total,
            "by_model": [{"model": r["model"], "count": r["cnt"]} for r in by_model],
            "by_endpoint": [{"endpoint": r["endpoint"], "count": r["cnt"]} for r in by_endpoint],
            "avg_latency_ms": round(avg_latency or 0, 1),
            "stream_count": stream_count,
            "error_count": error_count,
            "recent_logs": [dict(r) for r in recent_logs],
        }
```

- [ ] **Step 3: 在 web_api.py 添加两个新端点 — 更新模型 + 获取统计**

文件: `joycode_proxy/web_api.py:96-108`（在 `list_account_models` 之后、health 之前）

```python
    @router.put("/accounts/{api_key:path}/model")
    async def update_account_model(api_key: str, request: Request):
        body = await request.json()
        default_model = body.get("default_model", "").strip()
        acc = db.get_account(api_key)
        if not acc:
            raise HTTPException(404, f"Account '{api_key}' not found")
        db.update_account_model(api_key, default_model)
        return {"ok": True, "api_key": api_key, "default_model": default_model}

    @router.get("/accounts/{api_key:path}/stats")
    async def get_account_stats(api_key: str):
        acc = db.get_account(api_key)
        if not acc:
            raise HTTPException(404, f"Account '{api_key}' not found")
        return db.get_account_stats(api_key)
```

- [ ] **Step 4: 验证后端 API**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && python3 -m pytest tests/test_db.py tests/test_credential_router.py -v --tb=short 2>&1 | tail -20`
Expected:
  - Exit code: 0
  - Output contains: "passed"

- [ ] **Step 5: 提交**
Run: `git add joycode_proxy/db.py joycode_proxy/web_api.py && git commit -m "feat(api): add account model update and per-account stats endpoints"`

---

### Task 2: Frontend API 层 — 添加类型和 API 方法

**Depends on:** None
**Files:**
- Modify: `web/src/api.ts:1-56`

- [ ] **Step 1: 在 api.ts 添加 AccountStats 类型定义和新 API 方法**

文件: `web/src/api.ts`（在 `Settings` 接口之后添加 `AccountStats`，在 `api` 对象中添加两个方法）

```typescript
export interface AccountStats {
  api_key: string;
  total_requests: number;
  by_model: { model: string; count: number }[];
  by_endpoint: { endpoint: string; count: number }[];
  avg_latency_ms: number;
  stream_count: number;
  error_count: number;
  recent_logs: {
    id: number;
    api_key: string;
    model: string;
    endpoint: string;
    stream: number;
    status_code: number;
    latency_ms: number;
    created_at: string;
  }[];
}
```

在 `api` 对象中添加两个方法（在 `getHealth` 之后）:

```typescript
  updateAccountModel: (apiKey: string, defaultModel: string) =>
    request<{ ok: boolean }>(`/api/accounts/${encodeURIComponent(apiKey)}/model`, {
      method: 'PUT',
      body: JSON.stringify({ default_model: defaultModel }),
    }),
  getAccountStats: (apiKey: string) =>
    request<AccountStats>(`/api/accounts/${encodeURIComponent(apiKey)}/stats`),
```

- [ ] **Step 2: 提交**
Run: `git add web/src/api.ts && git commit -m "feat(api): add AccountStats type and account model/stats API methods"`

---

### Task 3: Frontend — 创建 AccountDetail 页面 + 路由集成

**Depends on:** Task 2
**Files:**
- Create: `web/src/pages/AccountDetail.tsx`
- Modify: `web/src/App.tsx:7-20`
- Modify: `web/src/pages/Accounts.tsx:94-174`
- Modify: `web/src/hooks/useDocumentTitle.ts:4-8`
- Modify: `web/src/layouts/MainLayout.tsx:15-19`

- [ ] **Step 1: 创建 AccountDetail.tsx — 账号详情页面**

```typescript
import React, { useEffect, useState } from 'react';
import {
  Card, Row, Col, Statistic, Typography, Spin, Tag, Select, Button,
  Table, Descriptions, message, Tooltip, Space, Empty,
} from 'antd';
import {
  ArrowLeftOutlined, ApiOutlined, ThunderboltOutlined,
  CheckCircleOutlined, WarningOutlined, ReloadOutlined,
  QuestionCircleOutlined,
} from '@ant-design/icons';
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip as RTooltip, ResponsiveContainer, PieChart, Pie, Cell } from 'recharts';
import { useParams, useNavigate } from 'react-router-dom';
import { api } from '../api';
import type { Account, AccountStats, ModelInfo } from '../api';

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

const COLORS = ['#1677ff', '#52c41a', '#faad14', '#ff4d4f', '#722ed1', '#13c2c2', '#eb2f96', '#fa8c16'];

const AccountDetail: React.FC = () => {
  const { apiKey } = useParams<{ apiKey: string }>();
  const navigate = useNavigate();
  const [account, setAccount] = useState<Account | null>(null);
  const [stats, setStats] = useState<AccountStats | null>(null);
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [modelLoading, setModelLoading] = useState(false);
  const [savingModel, setSavingModel] = useState(false);

  const decodedKey = apiKey ? decodeURIComponent(apiKey) : '';

  const fetchData = async () => {
    setLoading(true);
    try {
      const [accounts, statsData] = await Promise.all([
        api.listAccounts(),
        api.getAccountStats(decodedKey),
      ]);
      const acc = accounts.find((a) => a.api_key === decodedKey);
      setAccount(acc || null);
      setStats(statsData);
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '加载账号详情失败');
    } finally {
      setLoading(false);
    }
  };

  const fetchModels = async () => {
    setModelLoading(true);
    try {
      const data = await api.listAccountModels(decodedKey);
      setModels(data);
    } catch {
      // Fallback to builtin models — silently ignore
    } finally {
      setModelLoading(false);
    }
  };

  useEffect(() => { fetchData(); }, [decodedKey]);
  useEffect(() => { fetchModels(); }, [decodedKey]);

  const handleModelChange = async (newModel: string) => {
    setSavingModel(true);
    try {
      await api.updateAccountModel(decodedKey, newModel);
      message.success(`默认模型已更新为「${newModel || '未设置'}」`);
      fetchData();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '更新默认模型失败');
    } finally {
      setSavingModel(false);
    }
  };

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;
  if (!account) return <Empty description="账号不存在" />;

  const allModelOptions = [
    ...BUILTIN_MODELS,
    ...models
      .filter((m) => !BUILTIN_MODELS.some((b) => b.value === m.id))
      .map((m) => ({ label: m.name || m.id, value: m.id })),
  ];

  const logColumns = [
    { title: '时间', dataIndex: 'created_at', key: 'created_at', width: 170 },
    {
      title: '模型',
      dataIndex: 'model',
      key: 'model',
      render: (v: string) => v ? <Tag>{v}</Tag> : <Typography.Text type="secondary">-</Typography.Text>,
    },
    {
      title: '端点',
      dataIndex: 'endpoint',
      key: 'endpoint',
      render: (v: string) => <Typography.Text code style={{ fontSize: 12 }}>{v}</Typography.Text>,
    },
    {
      title: '流式',
      dataIndex: 'stream',
      key: 'stream',
      render: (v: number) => v ? <Tag color="blue">是</Tag> : <Tag>否</Tag>,
      width: 60,
    },
    {
      title: '状态码',
      dataIndex: 'status_code',
      key: 'status_code',
      render: (v: number) => v < 400
        ? <Tag color="green">{v}</Tag>
        : <Tag color="red">{v}</Tag>,
      width: 80,
    },
    {
      title: '延迟',
      dataIndex: 'latency_ms',
      key: 'latency_ms',
      render: (v: number) => v ? `${v}ms` : '-',
      width: 80,
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', alignItems: 'center', gap: 12 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/accounts')}>
          返回列表
        </Button>
        <Typography.Title level={4} style={{ margin: 0 }}>
          账号详情 — {decodedKey}
        </Typography.Title>
        <Button icon={<ReloadOutlined />} onClick={() => { fetchData(); fetchModels(); }} style={{ marginLeft: 'auto' }}>
          刷新
        </Button>
      </div>

      {/* 账号基本信息 + 模型配置 */}
      <Card style={{ marginBottom: 24 }}>
        <Descriptions column={{ xs: 1, sm: 2, md: 3 }}>
          <Descriptions.Item label="路由密钥">
            <Typography.Text code>{account.api_key}</Typography.Text>
          </Descriptions.Item>
          <Descriptions.Item label="用户 ID">{account.user_id}</Descriptions.Item>
          <Descriptions.Item label="状态">
            {account.is_default
              ? <Tag color="blue">默认账号</Tag>
              : <Tag>普通账号</Tag>}
          </Descriptions.Item>
          <Descriptions.Item label="创建时间">{account.created_at || '-'}</Descriptions.Item>
          <Descriptions.Item label={
            <Space size={4}>
              默认模型
              <Tooltip title="此账号的默认模型。当客户端未指定模型时使用。可从下拉列表选择，也可点击「获取在线模型」从 JoyCode API 获取该账号支持的全部模型">
                <QuestionCircleOutlined style={{ color: '#999' }} />
              </Tooltip>
            </Space>
          }>
            <Space>
              <Select
                style={{ width: 240 }}
                value={account.default_model || undefined}
                placeholder="未设置 — 使用系统默认"
                options={allModelOptions}
                allowClear
                loading={modelLoading}
                onChange={handleModelChange}
                disabled={savingModel}
              />
              <Button size="small" onClick={fetchModels} loading={modelLoading}>
                获取在线模型
              </Button>
            </Space>
          </Descriptions.Item>
        </Descriptions>
      </Card>

      {/* 统计卡片 */}
      {stats && (
        <>
          <Row gutter={[16, 16]} style={{ marginBottom: 24 }}>
            <Col xs={24} sm={12} md={6}>
              <Card>
                <Statistic
                  title="总请求数"
                  value={stats.total_requests}
                  prefix={<ApiOutlined />}
                />
              </Card>
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Card>
                <Statistic
                  title="平均延迟"
                  value={stats.avg_latency_ms}
                  suffix="毫秒"
                  prefix={<ThunderboltOutlined />}
                />
              </Card>
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Card>
                <Statistic
                  title="流式请求"
                  value={stats.stream_count}
                  prefix={<CheckCircleOutlined />}
                />
              </Card>
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Card>
                <Statistic
                  title="错误请求"
                  value={stats.error_count}
                  prefix={<WarningOutlined />}
                  valueStyle={stats.error_count > 0 ? { color: '#ff4d4f' } : undefined}
                />
              </Card>
            </Col>
          </Row>

          {/* 模型使用分布 */}
          {stats.by_model.length > 0 && (
            <Row gutter={[16, 16]} style={{ marginBottom: 24 }}>
              <Col xs={24} md={14}>
                <Card title="模型使用分布">
                  <ResponsiveContainer width="100%" height={280}>
                    <BarChart data={stats.by_model}>
                      <CartesianGrid strokeDasharray="3 3" />
                      <XAxis dataKey="model" />
                      <YAxis />
                      <RTooltip />
                      <Bar dataKey="count" name="请求次数" fill="#1677ff" radius={[4, 4, 0, 0]} />
                    </BarChart>
                  </ResponsiveContainer>
                </Card>
              </Col>
              <Col xs={24} md={10}>
                <Card title="端点调用分布">
                  <ResponsiveContainer width="100%" height={280}>
                    <PieChart>
                      <Pie
                        data={stats.by_endpoint}
                        dataKey="count"
                        nameKey="endpoint"
                        cx="50%"
                        cy="50%"
                        outerRadius={90}
                        label={({ endpoint, count }) => `${endpoint.split('/').pop()} (${count})`}
                      >
                        {stats.by_endpoint.map((_entry, index) => (
                          <Cell key={index} fill={COLORS[index % COLORS.length]} />
                        ))}
                      </Pie>
                      <RTooltip />
                    </PieChart>
                  </ResponsiveContainer>
                </Card>
              </Col>
            </Row>
          )}

          {/* 最近请求日志 */}
          <Card title="最近请求记录">
            <Table
              dataSource={stats.recent_logs}
              columns={logColumns}
              rowKey="id"
              size="small"
              pagination={false}
              locale={{ emptyText: '暂无请求记录' }}
            />
          </Card>
        </>
      )}
    </div>
  );
};

export default AccountDetail;
```

- [ ] **Step 2: 在 App.tsx 添加 AccountDetail 路由**

文件: `web/src/App.tsx`（添加 lazy import 和 Route）

替换整个 `App.tsx`:

```typescript
import React, { Suspense, lazy } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { ConfigProvider, Spin } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import MainLayout from './layouts/MainLayout';

const Dashboard = lazy(() => import('./pages/Dashboard'));
const Accounts = lazy(() => import('./pages/Accounts'));
const AccountDetail = lazy(() => import('./pages/AccountDetail'));
const Settings = lazy(() => import('./pages/Settings'));

const pageLoading = <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;

const App: React.FC = () => (
  <ConfigProvider locale={zhCN} theme={{ token: { colorPrimary: '#1677ff' } }}>
    <BrowserRouter>
      <Routes>
        <Route element={<MainLayout />}>
          <Route path="/" element={<Suspense fallback={pageLoading}><Dashboard /></Suspense>} />
          <Route path="/accounts" element={<Suspense fallback={pageLoading}><Accounts /></Suspense>} />
          <Route path="/accounts/:apiKey" element={<Suspense fallback={pageLoading}><AccountDetail /></Suspense>} />
          <Route path="/settings" element={<Suspense fallback={pageLoading}><Settings /></Suspense>} />
        </Route>
      </Routes>
    </BrowserRouter>
  </ConfigProvider>
);

export default App;
```

- [ ] **Step 3: 修改 Accounts.tsx 使表格行可点击跳转详情**

文件: `web/src/pages/Accounts.tsx`（添加 `useNavigate` import 和 `onRow` 属性）

在 `Accounts.tsx:1-10` 的 import 区域添加 `useNavigate`:

```typescript
import React, { useEffect, useState } from 'react';
import {
  Table, Button, Space, Modal, Form, Input, Switch, Select,
  message, Popconfirm, Tag, Typography, Alert, Tooltip,
} from 'antd';
import {
  PlusOutlined, DeleteOutlined, StarOutlined,
  SafetyCertificateOutlined, ReloadOutlined,
  QuestionCircleOutlined,
} from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { api } from '../api';
import type { Account } from '../api';
```

在组件函数体中（`const Accounts: React.FC = () => {` 之后），添加 `useNavigate`:

```typescript
  const navigate = useNavigate();
```

在 `<Table` 组件上添加 `onRow` 属性（文件: `Accounts.tsx:167-174`，在 `pagination={false}` 之后添加）:

```typescript
      <Table
        dataSource={accounts}
        columns={columns}
        rowKey="api_key"
        loading={loading}
        pagination={false}
        onRow={(record) => ({
          onClick: () => navigate(`/accounts/${encodeURIComponent(record.api_key)}`),
          style: { cursor: 'pointer' },
        })}
        locale={{ emptyText: '暂无账号，请点击「添加账号」按钮配置您的第一个 JoyCode 账号' }}
      />
```

- [ ] **Step 4: 更新 useDocumentTitle 添加详情页标题**

文件: `web/src/hooks/useDocumentTitle.ts`（在 `TITLES` 映射中添加新路由）

```typescript
import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';

const TITLES: Record<string, string> = {
  '/': '数据概览 — JoyCode 代理',
  '/accounts': '账号管理 — JoyCode 代理',
  '/settings': '系统设置 — JoyCode 代理',
};

const DEFAULT_TITLE = 'JoyCode 代理';

const useDocumentTitle = () => {
  const location = useLocation();
  useEffect(() => {
    if (location.pathname.startsWith('/accounts/')) {
      const key = decodeURIComponent(location.pathname.replace('/accounts/', ''));
      document.title = `${key} — 账号详情 — JoyCode 代理`;
    } else {
      document.title = TITLES[location.pathname] || DEFAULT_TITLE;
    }
  }, [location.pathname]);
};

export default useDocumentTitle;
```

- [ ] **Step 5: 更新 MainLayout 侧边栏高亮匹配**

文件: `web/src/layouts/MainLayout.tsx:48`（修改 `selectedKeys` 使详情页也高亮「账号管理」）

替换 `selectedKeys={[location.pathname]}` 为:

```typescript
          selectedKeys={[location.pathname.startsWith('/accounts') ? '/accounts' : location.pathname]}
```

- [ ] **Step 6: 构建前端验证**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build 2>&1 | tail -15`
Expected:
  - Exit code: 0
  - Output contains: "built in"
  - Output does NOT contain: "error" or "TS"

- [ ] **Step 7: 提交**
Run: `git add web/src/pages/AccountDetail.tsx web/src/App.tsx web/src/pages/Accounts.tsx web/src/hooks/useDocumentTitle.ts web/src/layouts/MainLayout.tsx && git commit -m "feat(ui): add account detail page with stats, model config, and request logs"`

---

### Task 4: 运行全部测试 + 端到端验证

**Depends on:** Task 1, Task 2, Task 3
**Files:** None (verification only)

- [ ] **Step 1: 运行后端全部测试**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && python3 -m pytest tests/ -v 2>&1 | tail -30`
Expected:
  - Exit code: 0
  - Output contains: "passed"
  - Output does NOT contain: "FAIL" or "ERROR"

- [ ] **Step 2: 验证前端静态资源已更新**
Run: `ls -la /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/joycode_proxy/static/assets/ 2>&1`
Expected:
  - Contains `AccountDetail` chunk file (e.g. `AccountDetail-*.js`)
  - Exit code: 0
