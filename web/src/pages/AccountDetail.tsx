import React, { useEffect, useState } from 'react';
import {
  Card, Row, Col, Statistic, Typography, Spin, Tag, Select, Button,
  message, Space, Table, Badge, Segmented, Popconfirm, Tooltip, Divider,
} from 'antd';
import {
  ArrowLeftOutlined, ApiOutlined, ThunderboltOutlined,
  CheckCircleOutlined, ReloadOutlined,
  ClockCircleOutlined, GlobalOutlined, FireOutlined, CopyOutlined,
  DeleteOutlined, QuestionCircleOutlined, InfoCircleOutlined,
  CloseCircleOutlined, SwapOutlined,
} from '@ant-design/icons';
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip as RTooltip,
  ResponsiveContainer, PieChart, Pie, Cell, AreaChart, Area,
} from 'recharts';
import { useParams, useNavigate } from 'react-router-dom';
import { api } from '../api';
import type { Account, AccountStats, ModelInfo, RequestLog } from '../api';
import SvgClaudeCode from '../components/ClaudeCodeIcon';
import SvgCodex from '../components/CodexIcon';
import CommandTooltip from '../components/CommandTooltip';

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

const PIE_COLORS = ['#00b578', '#36cfc9', '#73d13d', '#95de64', '#1890ff', '#13c2c2', '#eb2f96', '#fa8c16'];

const latencyColor = (ms: number) => {
  if (ms < 500) return '#52c41a';
  if (ms < 1500) return '#faad14';
  return '#ff4d4f';
};

const fmtTokens = (n: number) => {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(2) + 'M';
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K';
  return n.toLocaleString();
};

const statusTag = (code: number) => {
  if (code >= 200 && code < 300) return <Tag color="success">{code}</Tag>;
  if (code >= 400 && code < 500) return <Tag color="warning">{code}</Tag>;
  return <Tag color="error">{code}</Tag>;
};

const formatTime = (t: string) => {
  if (!t) return '-';
  const d = new Date(t + (t.includes('Z') || t.includes('+') ? '' : 'Z'));
  if (isNaN(d.getTime())) return t;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
};

const formatLatency = (ms: number) => {
  if (ms < 1000) return `${ms}ms`;
  const s = Math.floor(ms / 1000);
  const remainMs = ms % 1000;
  if (s < 60) return `${s}s${remainMs > 0 ? ` ${remainMs}ms` : ''}`;
  const m = Math.floor(s / 60);
  const remainS = s % 60;
  return `${m}m${remainS > 0 ? ` ${remainS}s` : ''}`;
};

const getBaseURL = () => `http://${window.location.host}`;

const buildClaudeCodeCmd = (apiKey: string, model = 'GLM-5.1') => [
  `API_TIMEOUT_MS=6000000 \\`,
  `CLAUDE_CODE_MAX_RETRIES=3 \\`,
  `ANTHROPIC_BASE_URL=${getBaseURL()} \\`,
  `ANTHROPIC_API_KEY="${apiKey}" \\`,
  `CLAUDE_CODE_MAX_OUTPUT_TOKENS=6553655 \\`,
  `ANTHROPIC_MODEL=${model} \\`,
  `claude --dangerously-skip-permissions`,
].join('\n');

const buildCodexCmd = (apiKey: string, model = 'GLM-5.1') => [
  `OPENAI_BASE_URL=${getBaseURL()}/v1 \\`,
  `OPENAI_API_KEY="${apiKey}" \\`,
  `OPENAI_MODEL=${model} \\`,
  `codex`,
].join('\n');

const copyCmd = async (text: string, label: string) => {
  try {
    await navigator.clipboard.writeText(text);
    message.success(`${label} 命令已复制到剪贴板`);
  } catch {
    message.error('复制失败');
  }
};

const AccountDetail: React.FC = () => {
  const { apiKey } = useParams<{ apiKey: string }>();
  const navigate = useNavigate();
  const [account, setAccount] = useState<Account | null>(null);
  const [stats, setStats] = useState<AccountStats | null>(null);
  const [logs, setLogs] = useState<RequestLog[]>([]);
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [modelLoading, setModelLoading] = useState(false);
  const [savingModel, setSavingModel] = useState(false);
  const [logFilter, setLogFilter] = useState<string>('all');

  const decodedKey = apiKey ? decodeURIComponent(apiKey) : '';

  const fetchData = async () => {
    setLoading(true);
    try {
      const [accounts, statsData, logsData] = await Promise.all([
        api.listAccounts(),
        api.getAccountStats(decodedKey),
        api.getAccountLogs(decodedKey, 500),
      ]);
      const acc = accounts.find((a) => a.api_key === decodedKey);
      setAccount(acc || null);
      setStats(statsData);
      setLogs(logsData.logs || []);
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '加载失败');
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
      // fallback to builtin
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
      message.error(e instanceof Error ? e.message : '更新失败');
    } finally {
      setSavingModel(false);
    }
  };

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;
  if (!account) return <div style={{ textAlign: 'center', padding: 100 }}>账号不存在</div>;

  const allModelOptions = [
    ...BUILTIN_MODELS,
    ...models
      .filter((m) => !BUILTIN_MODELS.some((b) => b.value === m.id))
      .map((m) => ({ label: m.name || m.id, value: m.id })),
  ];

  const filteredLogs = logFilter === 'all'
    ? logs
    : logFilter === 'errors'
      ? logs.filter((l) => l.status_code >= 400)
      : logs.filter((l) => l.stream);

  const endpointData = stats?.by_endpoint.map((e) => ({
    name: e.endpoint.replace('/v1/', ''),
    value: e.count,
  })) || [];

  const logColumns = [
    {
      title: '时间',
      dataIndex: 'created_at',
      key: 'time',
      width: 170,
      render: (t: string) => (
        <Typography.Text style={{ fontSize: 12, fontFamily: 'monospace' }}>
          {formatTime(t)}
        </Typography.Text>
      ),
    },
    {
      title: '端点',
      dataIndex: 'endpoint',
      key: 'endpoint',
      width: 200,
      render: (ep: string) => (
        <Typography.Text code style={{ fontSize: 12 }}>{ep}</Typography.Text>
      ),
    },
    {
      title: '模型',
      dataIndex: 'model',
      key: 'model',
      width: 140,
      ellipsis: true,
      render: (m: string) => m || <Typography.Text type="secondary">-</Typography.Text>,
    },
    {
      title: '流式',
      dataIndex: 'stream',
      key: 'stream',
      width: 60,
      render: (s: boolean) => s
        ? <Badge status="processing" text="" />
        : <Badge status="default" text="" />,
    },
    {
      title: '状态',
      dataIndex: 'status_code',
      key: 'status',
      width: 70,
      render: (code: number) => statusTag(code),
    },
    {
      title: '输入',
      dataIndex: 'input_tokens',
      key: 'input',
      width: 80,
      sorter: (a: RequestLog, b: RequestLog) => a.input_tokens - b.input_tokens,
      render: (n: number) => (
        <Typography.Text style={{ fontSize: 12, fontFamily: 'monospace' }}>
          {n > 0 ? fmtTokens(n) : '-'}
        </Typography.Text>
      ),
    },
    {
      title: '输出',
      dataIndex: 'output_tokens',
      key: 'output',
      width: 80,
      sorter: (a: RequestLog, b: RequestLog) => a.output_tokens - b.output_tokens,
      render: (n: number) => (
        <Typography.Text style={{ fontSize: 12, fontFamily: 'monospace' }}>
          {n > 0 ? fmtTokens(n) : '-'}
        </Typography.Text>
      ),
    },
    {
      title: '延迟',
      dataIndex: 'latency_ms',
      key: 'latency',
      width: 100,
      sorter: (a: RequestLog, b: RequestLog) => a.latency_ms - b.latency_ms,
      render: (ms: number) => (
        <Typography.Text style={{ color: latencyColor(ms), fontFamily: 'monospace', fontWeight: 500 }}>
          {formatLatency(ms)}
        </Typography.Text>
      ),
    },
  ];

  return (
    <div>
      {/* Header */}
      <div style={{
        marginBottom: 20, display: 'flex', alignItems: 'center', gap: 12,
        borderBottom: '1px solid #f0f0f0', paddingBottom: 16,
      }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/accounts')} type="text" />
        <div style={{ flex: 1 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <Typography.Title level={4} style={{ margin: 0 }}>{decodedKey}</Typography.Title>
            {account.is_default && <Tag color="blue">默认</Tag>}
          </div>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {account.user_id} · 创建于 {account.created_at?.slice(0, 10) || '-'}
          </Typography.Text>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginTop: 4 }}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>Token:</Typography.Text>
            <Typography.Text code copyable style={{ fontSize: 11 }}>
              {account.api_token}
            </Typography.Text>
          </div>
        </div>
        <Space>
          <Tooltip title="此模型的用途仅限生成下方的快速启动命令。实际请求中的模型由客户端指定（如 ANTHROPIC_MODEL 环境变量），始终优先于本设置。模型列表来自 JoyCode API 支持的模型 + 服务器动态获取的扩展模型。">
            <QuestionCircleOutlined style={{ color: '#999', cursor: 'help' }} />
          </Tooltip>
          <Select
            style={{ width: 220 }}
            value={account.default_model || undefined}
            placeholder="默认模型"
            options={allModelOptions}
            allowClear
            loading={modelLoading}
            onChange={handleModelChange}
            disabled={savingModel}
            size="small"
          />
          <Button size="small" onClick={async () => {
            try {
              await api.renewToken(decodedKey);
              message.success('API Token 已更新');
              fetchData();
            } catch (e: unknown) {
              message.error(e instanceof Error ? e.message : '更新失败');
            }
          }}>
            重置 Token
          </Button>
          <Button size="small" icon={<ReloadOutlined />} onClick={() => { fetchData(); fetchModels(); }}>
            刷新
          </Button>
          <Popconfirm
            title={`确定要删除账号「${decodedKey}」吗？`}
            description="删除后使用该密钥的客户端将无法访问"
            onConfirm={async () => {
              try {
                await api.removeAccount(decodedKey);
                message.success(`账号「${decodedKey}」已删除`);
                navigate('/accounts');
              } catch (e: unknown) {
                message.error(e instanceof Error ? e.message : '删除账号失败');
              }
            }}
          >
            <Button size="small" danger icon={<DeleteOutlined />}>删除</Button>
          </Popconfirm>
        </Space>
      </div>

      {/* Quick start commands */}
      <Card size="small" style={{ marginBottom: 16 }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 10 }}>
          <Typography.Text strong style={{ fontSize: 13 }}>
            快速启动命令
          </Typography.Text>
          <Tooltip title="模型优先级：客户端指定的模型（如启动命令中的 ANTHROPIC_MODEL）始终优先。上方设置的「默认模型」仅用于生成这些命令中的模型参数。如果你手动修改了启动命令中的模型，以你手动指定的为准。">
            <Typography.Text style={{ fontSize: 12, color: '#999', cursor: 'help' }}>
              <InfoCircleOutlined /> 模型优先级说明
            </Typography.Text>
          </Tooltip>
        </div>
        <Row gutter={[16, 12]}>
          <Col xs={24} md={12}>
            <div style={{
              background: '#f6f5f0', borderRadius: 6, padding: '10px 14px',
            }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 6 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                  <SvgClaudeCode />
                  <Typography.Text strong style={{ fontSize: 13 }}>Claude Code</Typography.Text>
                </div>
                <CommandTooltip
                  command={buildClaudeCodeCmd(account.api_token, account.default_model || undefined)}
                  label="Claude Code"
                >
                  <Button
                    type="text" size="small" icon={<CopyOutlined />}
                    onClick={() => copyCmd(buildClaudeCodeCmd(account.api_token, account.default_model || undefined), 'Claude Code')}
                  />
                </CommandTooltip>
              </div>
              <pre style={{ margin: 0, fontFamily: 'monospace', fontSize: 11, lineHeight: 1.6, whiteSpace: 'pre-wrap', color: '#333' }}>
{buildClaudeCodeCmd(account.api_token, account.default_model || undefined)}
              </pre>
            </div>
          </Col>
          <Col xs={24} md={12}>
            <div style={{
              background: '#f0faf5', borderRadius: 6, padding: '10px 14px',
            }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 6 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                  <SvgCodex />
                  <Typography.Text strong style={{ fontSize: 13 }}>Codex</Typography.Text>
                </div>
                <CommandTooltip
                  command={buildCodexCmd(account.api_token, account.default_model || undefined)}
                  label="Codex"
                >
                  <Button
                    type="text" size="small" icon={<CopyOutlined />}
                    onClick={() => copyCmd(buildCodexCmd(account.api_token, account.default_model || undefined), 'Codex')}
                  />
                </CommandTooltip>
              </div>
              <pre style={{ margin: 0, fontFamily: 'monospace', fontSize: 11, lineHeight: 1.6, whiteSpace: 'pre-wrap', color: '#333' }}>
{buildCodexCmd(account.api_token, account.default_model || undefined)}
              </pre>
            </div>
          </Col>
        </Row>
      </Card>

      {/* Stats panels */}
      {stats && (
        <Row gutter={[16, 16]} style={{ marginBottom: 20 }}>
          <Col xs={24} md={12}>
            <Card
              title={<span><ApiOutlined style={{ marginRight: 6 }} />请求统计</span>}
              size="small"
              style={{ borderRadius: 8, height: '100%' }}
            >
              <Row gutter={[8, 12]}>
                <Col span={12}>
                  <Statistic title="今日请求" value={stats.total_requests} valueStyle={{ fontSize: 20, color: '#00b578' }} prefix={<ApiOutlined />} />
                </Col>
                <Col span={12}>
                  <Statistic title="累计请求" value={stats.all_time?.total_requests ?? 0} valueStyle={{ fontSize: 20 }} />
                </Col>
                <Col span={12}>
                  <Statistic
                    title="今日成功"
                    value={stats.success_count}
                    prefix={<CheckCircleOutlined />}
                    valueStyle={{ fontSize: 18, color: '#52c41a' }}
                  />
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                    占比 {stats.total_requests > 0 ? Math.round((stats.success_count / stats.total_requests) * 100) : 100}%
                  </Typography.Text>
                </Col>
                <Col span={12}>
                  <Statistic
                    title="今日失败"
                    value={stats.error_count}
                    prefix={<CloseCircleOutlined />}
                    valueStyle={{ fontSize: 18, color: stats.error_count > 0 ? '#ff4d4f' : '#52c41a' }}
                  />
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                    占比 {stats.total_requests > 0 ? Math.round((stats.error_count / stats.total_requests) * 100) : 0}%
                  </Typography.Text>
                </Col>
                <Col span={24}>
                  <Divider style={{ margin: '4px 0 8px' }} />
                  <Row gutter={8}>
                    <Col span={12}>
                      <Statistic
                        title="流式请求"
                        value={stats.stream_count}
                        prefix={<SwapOutlined />}
                        valueStyle={{ fontSize: 16 }}
                      />
                      <Tag color="blue" style={{ marginTop: 2 }}>
                        {stats.total_requests > 0 ? Math.round((stats.stream_count / stats.total_requests) * 100) : 0}%
                      </Tag>
                    </Col>
                    <Col span={12}>
                      <Statistic
                        title="平均延迟"
                        value={Math.round(stats.avg_latency_ms)}
                        suffix="ms"
                        prefix={<ThunderboltOutlined />}
                        valueStyle={{ fontSize: 16, color: stats.avg_latency_ms < 500 ? '#52c41a' : stats.avg_latency_ms < 1500 ? '#faad14' : '#ff4d4f' }}
                      />
                    </Col>
                  </Row>
                </Col>
              </Row>
            </Card>
          </Col>

          <Col xs={24} md={12}>
            <Card
              title={<span><FireOutlined style={{ marginRight: 6 }} />Token 消费</span>}
              size="small"
              style={{ borderRadius: 8, height: '100%' }}
            >
              <Row gutter={[8, 12]}>
                <Col span={12}>
                  <Statistic
                    title="今日 Token"
                    value={fmtTokens(stats.total_input_tokens + stats.total_output_tokens)}
                    valueStyle={{ fontSize: 20, color: '#389e0d' }}
                  />
                </Col>
                <Col span={12}>
                  <Statistic
                    title="累计 Token"
                    value={fmtTokens((stats.all_time?.total_input_tokens ?? 0) + (stats.all_time?.total_output_tokens ?? 0))}
                    valueStyle={{ fontSize: 20 }}
                  />
                </Col>
                <Col span={12}>
                  <Statistic title="今日输入" value={fmtTokens(stats.total_input_tokens)} valueStyle={{ fontSize: 16 }} />
                </Col>
                <Col span={12}>
                  <Statistic title="今日输出" value={fmtTokens(stats.total_output_tokens)} valueStyle={{ fontSize: 16 }} />
                </Col>
                <Col span={24}>
                  <Divider style={{ margin: '4px 0 8px' }} />
                  <Row gutter={8}>
                    <Col span={12}>
                      <Statistic
                        title="平均每请求"
                        value={stats.total_requests > 0 ? fmtTokens(Math.round((stats.total_input_tokens + stats.total_output_tokens) / stats.total_requests)) : '-'}
                        suffix={stats.total_requests > 0 ? 'tokens' : ''}
                        valueStyle={{ fontSize: 15 }}
                      />
                    </Col>
                    <Col span={12}>
                      <Statistic
                        title="输入/输出比"
                        value={stats.total_output_tokens > 0 ? (stats.total_input_tokens / stats.total_output_tokens).toFixed(1) : '-'}
                        suffix={stats.total_output_tokens > 0 ? ':1' : ''}
                        valueStyle={{ fontSize: 15 }}
                      />
                    </Col>
                  </Row>
                </Col>
              </Row>
            </Card>
          </Col>
        </Row>
      )}

      {/* Hourly trend charts */}
      {stats && stats.hourly && stats.hourly.length > 0 && (
        <Row gutter={[16, 16]} style={{ marginBottom: 20 }}>
          <Col xs={24} lg={12}>
            <Card title="24 小时请求趋势" size="small">
              <ResponsiveContainer width="100%" height={200}>
                <AreaChart data={stats.hourly} margin={{ left: -10 }}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="hour" tick={{ fontSize: 11 }} />
                  <YAxis tick={{ fontSize: 11 }} />
                  <RTooltip />
                  <Area type="monotone" dataKey="count" name="请求数" stroke="#00b578" fill="#00b578" fillOpacity={0.15} />
                  <Area type="monotone" dataKey="errors" name="错误数" stroke="#ff4d4f" fill="#ff4d4f" fillOpacity={0.15} />
                </AreaChart>
              </ResponsiveContainer>
            </Card>
          </Col>
          <Col xs={24} lg={12}>
            <Card title="24 小时 Token 消耗趋势" size="small">
              <ResponsiveContainer width="100%" height={200}>
                <AreaChart data={stats.hourly} margin={{ left: -10 }}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="hour" tick={{ fontSize: 11 }} />
                  <YAxis tick={{ fontSize: 11 }} />
                  <RTooltip />
                  <Area type="monotone" dataKey="input_tokens" name="输入 Token" stroke="#1890ff" fill="#1890ff" fillOpacity={0.15} />
                  <Area type="monotone" dataKey="output_tokens" name="输出 Token" stroke="#73d13d" fill="#73d13d" fillOpacity={0.15} />
                </AreaChart>
              </ResponsiveContainer>
            </Card>
          </Col>
        </Row>
      )}

      {/* Charts row */}
      {stats && (stats.by_model.length > 0 || endpointData.length > 0) && (
        <Row gutter={[16, 16]} style={{ marginBottom: 20 }}>
          {stats.by_model.length > 0 && (
            <Col xs={24} lg={14}>
              <Card title={<><FireOutlined /> 模型使用分布</>} size="small">
                <ResponsiveContainer width="100%" height={200}>
                  <BarChart data={stats.by_model} layout="vertical" margin={{ left: 10 }}>
                    <CartesianGrid strokeDasharray="3 3" />
                    <XAxis type="number" />
                    <YAxis dataKey="model" type="category" width={100} tick={{ fontSize: 11 }} />
                    <RTooltip />
                    <Bar dataKey="count" name="请求数" fill="#00b578" radius={[0, 4, 4, 0]} />
                  </BarChart>
                </ResponsiveContainer>
              </Card>
            </Col>
          )}
          {endpointData.length > 0 && (
            <Col xs={24} lg={10}>
              <Card title={<><GlobalOutlined /> 端点调用分布</>} size="small">
                <ResponsiveContainer width="100%" height={200}>
                  <PieChart>
                    <Pie
                      data={endpointData}
                      dataKey="value"
                      nameKey="name"
                      cx="50%"
                      cy="50%"
                      outerRadius={70}
                      label={({ name, percent }: any) => `${name || ''} ${((percent || 0) * 100).toFixed(0)}%`}
                      labelLine={{ strokeWidth: 1 }}
                    >
                      {endpointData.map((_, i) => (
                        <Cell key={i} fill={PIE_COLORS[i % PIE_COLORS.length]} />
                      ))}
                    </Pie>
                    <RTooltip />
                  </PieChart>
                </ResponsiveContainer>
              </Card>
            </Col>
          )}
        </Row>
      )}

      {/* Request logs */}
      <Card
        title={
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <ClockCircleOutlined />
            <span>请求日志</span>
            <Tag>{logs.length} 条</Tag>
          </div>
        }
        size="small"
        extra={
          <Segmented
            size="small"
            value={logFilter}
            onChange={(v) => setLogFilter(v as string)}
            options={[
              { label: '全部', value: 'all' },
              { label: '流式', value: 'stream' },
              { label: '错误', value: 'errors' },
            ]}
          />
        }
      >
        <Table
          dataSource={filteredLogs}
          columns={logColumns}
          rowKey="id"
          size="small"
          pagination={{ pageSize: 20, showSizeChanger: false, showTotal: (t) => `共 ${t} 条` }}
          scroll={{ x: 900 }}
          locale={{ emptyText: '暂无请求记录' }}
          expandable={{
            rowExpandable: (record) => record.status_code >= 400 && !!record.error_message,
            expandedRowRender: (record) => (
              <div style={{ padding: '8px 0' }}>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>错误详情：</Typography.Text>
                <Typography.Text style={{ fontSize: 12, color: '#cf1322', fontFamily: 'monospace' }}>
                  {record.error_message}
                </Typography.Text>
              </div>
            ),
          }}
        />
      </Card>
    </div>
  );
};

export default AccountDetail;
