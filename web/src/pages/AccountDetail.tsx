import React, { useEffect, useState } from 'react';
import {
  Card, Row, Col, Statistic, Typography, Spin, Tag, Select, Button,
  message, Space, Table, Badge, Segmented, Popconfirm,
} from 'antd';
import {
  ArrowLeftOutlined, ApiOutlined, ThunderboltOutlined,
  CheckCircleOutlined, WarningOutlined, ReloadOutlined,
  ClockCircleOutlined, GlobalOutlined, FireOutlined, CopyOutlined,
  DeleteOutlined,
} from '@ant-design/icons';
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip as RTooltip,
  ResponsiveContainer, PieChart, Pie, Cell,
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

const PIE_COLORS = ['#1677ff', '#52c41a', '#faad14', '#ff4d4f', '#722ed1', '#13c2c2', '#eb2f96', '#fa8c16'];

const latencyColor = (ms: number) => {
  if (ms < 500) return '#52c41a';
  if (ms < 1500) return '#faad14';
  return '#ff4d4f';
};

const statusTag = (code: number) => {
  if (code >= 200 && code < 300) return <Tag color="success">{code}</Tag>;
  if (code >= 400 && code < 500) return <Tag color="warning">{code}</Tag>;
  return <Tag color="error">{code}</Tag>;
};

const formatTime = (t: string) => {
  if (!t) return '-';
  return t.replace('T', ' ').slice(0, 19);
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

  const successRate = stats && stats.total_requests > 0
    ? Math.round(((stats.total_requests - stats.error_count) / stats.total_requests) * 100)
    : 100;

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
      title: '延迟',
      dataIndex: 'latency_ms',
      key: 'latency',
      width: 100,
      sorter: (a: RequestLog, b: RequestLog) => a.latency_ms - b.latency_ms,
      render: (ms: number) => (
        <Typography.Text style={{ color: latencyColor(ms), fontFamily: 'monospace', fontWeight: 500 }}>
          {ms}ms
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
        <Typography.Text strong style={{ display: 'block', marginBottom: 10, fontSize: 13 }}>
          快速启动命令
        </Typography.Text>
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

      {/* Stats row */}
      {stats && (
        <Row gutter={[12, 12]} style={{ marginBottom: 20 }}>
          <Col xs={12} sm={6}>
            <Card size="small" bodyStyle={{ padding: '12px 16px' }}>
              <Statistic
                title={<span style={{ fontSize: 12 }}>总请求</span>}
                value={stats.total_requests}
                prefix={<ApiOutlined />}
                valueStyle={{ fontSize: 20 }}
              />
            </Card>
          </Col>
          <Col xs={12} sm={6}>
            <Card size="small" bodyStyle={{ padding: '12px 16px' }}>
              <Statistic
                title={<span style={{ fontSize: 12 }}>平均延迟</span>}
                value={Math.round(stats.avg_latency_ms)}
                suffix="ms"
                prefix={<ThunderboltOutlined />}
                valueStyle={{ fontSize: 20 }}
              />
            </Card>
          </Col>
          <Col xs={12} sm={6}>
            <Card size="small" bodyStyle={{ padding: '12px 16px' }}>
              <Statistic
                title={<span style={{ fontSize: 12 }}>成功率</span>}
                value={successRate}
                suffix="%"
                prefix={<CheckCircleOutlined />}
                valueStyle={{ fontSize: 20, color: successRate >= 95 ? '#52c41a' : successRate >= 80 ? '#faad14' : '#ff4d4f' }}
              />
            </Card>
          </Col>
          <Col xs={12} sm={6}>
            <Card size="small" bodyStyle={{ padding: '12px 16px' }}>
              <Statistic
                title={<span style={{ fontSize: 12 }}>错误</span>}
                value={stats.error_count}
                prefix={<WarningOutlined />}
                valueStyle={{ fontSize: 20, color: stats.error_count > 0 ? '#ff4d4f' : undefined }}
              />
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
                    <Bar dataKey="count" name="请求数" fill="#1677ff" radius={[0, 4, 4, 0]} />
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
          scroll={{ x: 740 }}
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
