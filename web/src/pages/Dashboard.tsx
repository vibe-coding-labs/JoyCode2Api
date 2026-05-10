import React, { useEffect, useState } from 'react';
import {
  Card, Col, Row, Statistic, Spin, Empty, Typography, Table, Tag, Divider,
} from 'antd';
import {
  ThunderboltOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  TeamOutlined,
  ApiOutlined,
  SwapOutlined,
  DashboardOutlined,
  FireOutlined,
  RiseOutlined,
} from '@ant-design/icons';
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
  AreaChart, Area,
} from 'recharts';
import { api, accountDisplayName } from '../api';
import type { Stats, Account } from '../api';

const COLORS = ['#00b578', '#36cfc9', '#73d13d', '#95de64', '#1890ff', '#722ed1', '#13c2c2', '#fa8c16'];

const fmt = (n: number) => {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(2) + 'M';
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K';
  return n.toLocaleString();
};

const fmtLatency = (ms: number) => {
  if (ms < 1000) return `${ms}ms`;
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  const remainS = s % 60;
  return `${m}m${remainS > 0 ? ` ${remainS}s` : ''}`;
};

const Dashboard: React.FC = () => {
  const [stats, setStats] = useState<Stats | null>(null);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchData = async () => {
    setLoading(true);
    try {
      const [statsData, accountsData] = await Promise.all([
        api.getStats(),
        api.listAccounts(),
      ]);
      setStats(statsData);
      setAccounts(accountsData);
    } catch (e) {
      console.error(e);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchData(); }, []);

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;
  if (!stats) return <Empty description="无法加载统计数据" />;

  const successRate = stats.total_requests > 0
    ? Math.round((stats.success_count / stats.total_requests) * 100) : 100;
  const errorRate = stats.total_requests > 0
    ? Math.round((stats.error_count / stats.total_requests) * 100) : 0;
  const streamRate = stats.total_requests > 0
    ? Math.round((stats.stream_count / stats.total_requests) * 100) : 0;
  const totalTokens = stats.total_input_tokens + stats.total_output_tokens;
  const allTimeTokens = (stats.all_time?.total_input_tokens ?? 0) + (stats.all_time?.total_output_tokens ?? 0);
  const avgTokensPerReq = stats.total_requests > 0
    ? Math.round(totalTokens / stats.total_requests) : 0;
  const avgLatency = Math.round(stats.avg_latency_ms);

  const modelData = stats.by_model.map((m) => ({
    name: m.model, value: m.count,
    pct: stats.total_requests > 0 ? Math.round((m.count / stats.total_requests) * 100) : 0,
  }));

  const accountData = stats.by_account.map((a) => ({
    name: accountDisplayName(a), value: a.count,
    pct: stats.total_requests > 0 ? Math.round((a.count / stats.total_requests) * 100) : 0,
  }));

  // Build hourly chart data — fill gaps with zeros
  // Key format matches backend strftime('%m-%d %H') to avoid cross-day hour merging
  const hourlyMap = new Map<string, { count: number; tokens: number; errors: number }>();
  for (const h of stats.hourly ?? []) {
    hourlyMap.set(h.hour, { count: h.count, tokens: h.input_tokens + h.output_tokens, errors: h.errors });
  }
  const now = new Date();
  const hourlyChartData: { hour: string; label: string; requests: number; tokens: number; errors: number }[] = [];
  for (let i = 23; i >= 0; i--) {
    const d = new Date(now.getTime() - i * 3600000);
    const key = `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}`;
    const label = `${String(d.getHours()).padStart(2, '0')}:00`;
    const entry = hourlyMap.get(key);
    hourlyChartData.push({
      hour: key,
      label,
      requests: entry?.count ?? 0,
      tokens: entry?.tokens ?? 0,
      errors: entry?.errors ?? 0,
    });
  }

  const accountCols = [
    {
      title: '账号',
      dataIndex: 'user_id',
      key: 'user_id',
      render: (_: unknown, record: Account) => <Typography.Text strong style={{ fontSize: 13 }}>{accountDisplayName(record)}</Typography.Text>,
    },
    {
      title: '默认模型',
      dataIndex: 'default_model',
      key: 'model',
      render: (m: string) => m ? <Tag>{m}</Tag> : <Typography.Text type="secondary">-</Typography.Text>,
    },
    {
      title: '请求量',
      key: 'count',
      render: (_: unknown, record: Account) => {
        const found = stats.by_account.find((a) => a.user_id === record.user_id);
        return found ? found.count.toLocaleString() : <Typography.Text type="secondary">0</Typography.Text>;
      },
    },
    {
      title: '状态',
      key: 'status',
      render: () => <Tag color="success">在线</Tag>,
    },
  ];

  return (
    <div>
      {/* Banner */}
      <Card
        style={{ marginBottom: 16, background: 'linear-gradient(135deg, #00b578 0%, #009a63 100%)', border: 'none', borderRadius: 12 }}
        bodyStyle={{ padding: '20px 24px' }}
      >
        <Row align="middle" justify="space-between">
          <Col>
            <Typography.Text style={{ color: 'rgba(255,255,255,0.85)', fontSize: 13 }}>
              JoyCode API 代理服务 · 数据概览
            </Typography.Text>
            <Typography.Title level={3} style={{ color: '#fff', margin: '4px 0 0' }}>
              系统运行状态
            </Typography.Title>
          </Col>
          <Col>
            <Row gutter={32}>
              <Col style={{ textAlign: 'center' }}>
                <div style={{ color: 'rgba(255,255,255,0.7)', fontSize: 12 }}>今日请求</div>
                <div style={{ color: '#fff', fontSize: 26, fontWeight: 700 }}>{stats.total_requests.toLocaleString()}</div>
              </Col>
              <Col style={{ textAlign: 'center' }}>
                <div style={{ color: 'rgba(255,255,255,0.7)', fontSize: 12 }}>今日 Token</div>
                <div style={{ color: '#fff', fontSize: 26, fontWeight: 700 }}>{fmt(totalTokens)}</div>
              </Col>
              <Col style={{ textAlign: 'center' }}>
                <div style={{ color: 'rgba(255,255,255,0.7)', fontSize: 12 }}>累计请求</div>
                <div style={{ color: '#fff', fontSize: 22, fontWeight: 600 }}>{(stats.all_time?.total_requests ?? 0).toLocaleString()}</div>
              </Col>
              <Col style={{ textAlign: 'center' }}>
                <div style={{ color: 'rgba(255,255,255,0.7)', fontSize: 12 }}>累计 Token</div>
                <div style={{ color: '#fff', fontSize: 22, fontWeight: 600 }}>{fmt(allTimeTokens)}</div>
              </Col>
              <Col style={{ textAlign: 'center' }}>
                <div style={{ color: 'rgba(255,255,255,0.7)', fontSize: 12 }}>账号数</div>
                <div style={{ color: '#fff', fontSize: 26, fontWeight: 700 }}>{stats.accounts_count}</div>
              </Col>
              <Col style={{ textAlign: 'center' }}>
                <div style={{ color: 'rgba(255,255,255,0.7)', fontSize: 12 }}>成功率</div>
                <div style={{ color: '#fff', fontSize: 26, fontWeight: 700 }}>{successRate}%</div>
              </Col>
            </Row>
          </Col>
        </Row>
      </Card>

      {/* 24h 时序图表 */}
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={12}>
          <Card
            title={<span><ApiOutlined style={{ marginRight: 6 }} />24 小时请求趋势</span>}
            size="small"
            style={{ borderRadius: 8 }}
          >
            <ResponsiveContainer width="100%" height={200}>
              <AreaChart data={hourlyChartData} margin={{ top: 5, right: 10, left: 0, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis dataKey="label" tick={{ fontSize: 10 }} interval={2} />
                <YAxis tick={{ fontSize: 11 }} />
                <Tooltip formatter={(v: unknown) => [Number(v).toLocaleString(), '请求数']} />
                <Area type="monotone" dataKey="requests" name="requests" stroke="#00b578" fill="#00b578" fillOpacity={0.15} strokeWidth={2} />
                <Area type="monotone" dataKey="errors" name="errors" stroke="#ff4d4f" fill="#ff4d4f" fillOpacity={0.1} strokeWidth={1.5} />
              </AreaChart>
            </ResponsiveContainer>
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card
            title={<span><FireOutlined style={{ marginRight: 6 }} />24 小时 Token 消耗趋势</span>}
            size="small"
            style={{ borderRadius: 8 }}
          >
            <ResponsiveContainer width="100%" height={200}>
              <AreaChart data={hourlyChartData} margin={{ top: 5, right: 10, left: 0, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis dataKey="label" tick={{ fontSize: 10 }} interval={2} />
                <YAxis tick={{ fontSize: 11 }} tickFormatter={(v: number) => fmt(v)} />
                <Tooltip formatter={(v: unknown) => [fmt(Number(v)), 'Token 用量']} />
                <Area type="monotone" dataKey="tokens" stroke="#389e0d" fill="#389e0d" fillOpacity={0.15} strokeWidth={2} />
              </AreaChart>
            </ResponsiveContainer>
          </Card>
        </Col>
      </Row>

      {/* 统计面板：今日 + 累计 */}
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        {/* 请求统计 */}
        <Col xs={24} md={8}>
          <Card
            title={<span><ApiOutlined style={{ marginRight: 6 }} />请求统计</span>}
            size="small"
            style={{ borderRadius: 8, height: '100%' }}
          >
            <Row gutter={[8, 12]}>
              <Col span={12}>
                <Statistic title="今日请求" value={stats.total_requests} valueStyle={{ fontSize: 20, color: '#00b578' }} />
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
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>占比 {successRate}%</Typography.Text>
              </Col>
              <Col span={12}>
                <Statistic
                  title="今日失败"
                  value={stats.error_count}
                  prefix={<CloseCircleOutlined />}
                  valueStyle={{ fontSize: 18, color: stats.error_count > 0 ? '#ff4d4f' : '#52c41a' }}
                />
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>占比 {errorRate}%</Typography.Text>
              </Col>
              <Col span={24}>
                <Divider style={{ margin: '4px 0 8px' }} />
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <Statistic
                    title="流式请求"
                    value={stats.stream_count}
                    valueStyle={{ fontSize: 16 }}
                    prefix={<SwapOutlined />}
                  />
                  <Tag color="blue" style={{ height: 'fit-content', marginTop: 20 }}>{streamRate}%</Tag>
                </div>
              </Col>
            </Row>
          </Card>
        </Col>

        {/* Token 消费 */}
        <Col xs={24} md={8}>
          <Card
            title={<span><FireOutlined style={{ marginRight: 6 }} />Token 消费</span>}
            size="small"
            style={{ borderRadius: 8, height: '100%' }}
          >
            <Row gutter={[8, 12]}>
              <Col span={12}>
                <Statistic title="今日 Token" value={fmt(totalTokens)} valueStyle={{ fontSize: 20, color: '#389e0d' }} />
              </Col>
              <Col span={12}>
                <Statistic title="累计 Token" value={fmt(allTimeTokens)} valueStyle={{ fontSize: 20 }} />
              </Col>
              <Col span={12}>
                <Statistic title="今日输入" value={fmt(stats.total_input_tokens)} valueStyle={{ fontSize: 16 }} />
              </Col>
              <Col span={12}>
                <Statistic title="今日输出" value={fmt(stats.total_output_tokens)} valueStyle={{ fontSize: 16 }} />
              </Col>
              <Col span={24}>
                <Divider style={{ margin: '4px 0 8px' }} />
                <Row gutter={8}>
                  <Col span={12}>
                    <Statistic title="平均每请求" value={avgTokensPerReq.toLocaleString()} suffix="tokens" valueStyle={{ fontSize: 15 }} />
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

        {/* 响应质量 */}
        <Col xs={24} md={8}>
          <Card
            title={<span><DashboardOutlined style={{ marginRight: 6 }} />响应质量</span>}
            size="small"
            style={{ borderRadius: 8, height: '100%' }}
          >
            <Row gutter={[8, 12]}>
              <Col span={12}>
                <Statistic
                  title="平均延迟"
                  value={fmtLatency(avgLatency)}
                  prefix={<ThunderboltOutlined />}
                  valueStyle={{ fontSize: 20, color: avgLatency < 5000 ? '#52c41a' : avgLatency < 15000 ? '#faad14' : '#ff4d4f' }}
                />
              </Col>
              <Col span={12}>
                <Statistic
                  title="成功率"
                  value={successRate}
                  suffix="%"
                  prefix={<CheckCircleOutlined />}
                  valueStyle={{ fontSize: 20, color: successRate >= 95 ? '#52c41a' : successRate >= 80 ? '#faad14' : '#ff4d4f' }}
                />
              </Col>
              <Col span={24}>
                <Divider style={{ margin: '4px 0 8px' }} />
                <Statistic title="流式占比" value={streamRate} suffix="%" prefix={<SwapOutlined />} valueStyle={{ fontSize: 18 }} />
              </Col>
              <Col span={12}>
                <Statistic title="配置账号" value={stats.accounts_count} prefix={<TeamOutlined />} valueStyle={{ fontSize: 16 }} />
              </Col>
              <Col span={12}>
                <Statistic title="使用模型" value={stats.by_model.length} prefix={<RiseOutlined />} valueStyle={{ fontSize: 16 }} />
              </Col>
            </Row>
          </Card>
        </Col>
      </Row>

      {/* 图表面板 */}
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        {/* 模型分布 */}
        <Col xs={24} lg={12}>
          <Card
            title={<span><RiseOutlined style={{ marginRight: 6 }} />模型使用分布</span>}
            size="small"
            style={{ borderRadius: 8 }}
          >
            {modelData.length > 0 ? (
              <Row>
                <Col xs={24} md={14}>
                  <ResponsiveContainer width="100%" height={220}>
                    <BarChart data={modelData} layout="vertical" margin={{ left: 10 }}>
                      <CartesianGrid strokeDasharray="3 3" />
                      <XAxis type="number" tick={{ fontSize: 11 }} />
                      <YAxis dataKey="name" type="category" width={110} tick={{ fontSize: 11 }} />
                      <Tooltip formatter={(v: unknown) => [Number(v).toLocaleString(), '请求数']} />
                      <Bar dataKey="value" name="请求数" fill="#00b578" radius={[0, 4, 4, 0]} />
                    </BarChart>
                  </ResponsiveContainer>
                </Col>
                <Col xs={24} md={10}>
                  <div style={{ padding: '4px 0 0 12px' }}>
                    {modelData.map((m, i) => (
                      <div key={m.name} style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '4px 0', borderBottom: '1px solid #f5f5f5' }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                          <div style={{ width: 8, height: 8, borderRadius: '50%', background: COLORS[i % COLORS.length] }} />
                          <Typography.Text style={{ fontSize: 12 }}>{m.name}</Typography.Text>
                        </div>
                        <div>
                          <Typography.Text style={{ fontSize: 12, fontWeight: 500 }}>{m.value.toLocaleString()}</Typography.Text>
                          <Typography.Text type="secondary" style={{ fontSize: 11, marginLeft: 4 }}>{m.pct}%</Typography.Text>
                        </div>
                      </div>
                    ))}
                  </div>
                </Col>
              </Row>
            ) : (
              <Empty description="暂无数据" image={Empty.PRESENTED_IMAGE_SIMPLE} />
            )}
          </Card>
        </Col>

        {/* 账号请求分布 */}
        <Col xs={24} lg={12}>
          <Card
            title={<span><TeamOutlined style={{ marginRight: 6 }} />账号请求分布</span>}
            size="small"
            style={{ borderRadius: 8 }}
          >
            {accountData.length > 0 ? (
              <Row>
                <Col xs={24} md={14}>
                  <ResponsiveContainer width="100%" height={220}>
                    <BarChart data={accountData}>
                      <CartesianGrid strokeDasharray="3 3" />
                      <XAxis dataKey="name" tick={{ fontSize: 11 }} />
                      <YAxis tick={{ fontSize: 11 }} />
                      <Tooltip formatter={(v: unknown) => [Number(v).toLocaleString(), '请求数']} />
                      <Bar dataKey="value" name="请求数" fill="#00b578" radius={[4, 4, 0, 0]} />
                    </BarChart>
                  </ResponsiveContainer>
                </Col>
                <Col xs={24} md={10}>
                  <div style={{ padding: '4px 0 0 12px' }}>
                    {accountData.map((a, i) => (
                      <div key={a.name} style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '4px 0', borderBottom: '1px solid #f5f5f5' }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                          <div style={{ width: 8, height: 8, borderRadius: '50%', background: COLORS[i % COLORS.length] }} />
                          <Typography.Text style={{ fontSize: 12 }}>{a.name}</Typography.Text>
                        </div>
                        <div>
                          <Typography.Text style={{ fontSize: 12, fontWeight: 500 }}>{a.value.toLocaleString()}</Typography.Text>
                          <Typography.Text type="secondary" style={{ fontSize: 11, marginLeft: 4 }}>{a.pct}%</Typography.Text>
                        </div>
                      </div>
                    ))}
                  </div>
                </Col>
              </Row>
            ) : (
              <Empty description="暂无数据" image={Empty.PRESENTED_IMAGE_SIMPLE} />
            )}
          </Card>
        </Col>
      </Row>

      {/* 账号详情表 */}
      {accounts.length > 0 && (
        <Card
          title={<span><TeamOutlined style={{ marginRight: 6 }} />账号概览</span>}
          size="small"
          style={{ marginTop: 16, borderRadius: 8 }}
          extra={<Tag>{accounts.length} 个账号</Tag>}
        >
          <Table
            dataSource={accounts}
            columns={accountCols}
            rowKey="user_id"
            size="small"
            pagination={false}
          />
        </Card>
      )}

      {/* 空状态 */}
      {stats.total_requests === 0 && (stats.all_time?.total_requests ?? 0) === 0 && (
        <Card style={{ marginTop: 16, borderRadius: 8 }}>
          <Empty description="暂无请求数据">
            <Typography.Text type="secondary">
              配置好账号后，使用 Claude Code 或 Codex 连接到本代理即可看到统计数据
            </Typography.Text>
          </Empty>
        </Card>
      )}
    </div>
  );
};

export default Dashboard;
