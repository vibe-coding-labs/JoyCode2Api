import React, { useEffect, useState } from 'react';
import {
  Card, Col, Row, Statistic, Empty, Typography, Table, Tag, Divider, Skeleton,
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

// Semantic palette — accent for primary series, danger for errors, info for secondary.
// Per ui-ux-pro-max: don't rely on color alone; bars are also sorted + value-labeled.
const CHART_COLORS = {
  primary: '#22C55E',
  primaryFill: 'rgba(34, 197, 94, 0.15)',
  secondary: '#3B82F6',
  secondaryFill: 'rgba(59, 130, 246, 0.12)',
  danger: '#EF4444',
  dangerFill: 'rgba(239, 68, 68, 0.12)',
  warning: '#F59E0B',
  muted: '#94A3B8',
  grid: '#1F2937',
  axis: '#475569',
};

// Distinct hues for category bars (model/account distribution) — accessible on dark bg
const CATEGORY_COLORS = ['#22C55E', '#3B82F6', '#F59E0B', '#EF4444', '#A855F7', '#06B6D4', '#EC4899', '#84CC16'];

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

  if (loading) {
    return (
      <div className="jc-page">
        <Skeleton.Button active block style={{ height: 96, marginBottom: 16, borderRadius: 10 }} />
        <Row gutter={[16, 16]}>
          <Col xs={24} lg={12}><Card size="small"><Skeleton active paragraph={{ rows: 6 }} /></Card></Col>
          <Col xs={24} lg={12}><Card size="small"><Skeleton active paragraph={{ rows: 6 }} /></Card></Col>
        </Row>
        <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
          <Col xs={24} md={8}><Card size="small"><Skeleton active paragraph={{ rows: 5 }} /></Card></Col>
          <Col xs={24} md={8}><Card size="small"><Skeleton active paragraph={{ rows: 5 }} /></Card></Col>
          <Col xs={24} md={8}><Card size="small"><Skeleton active paragraph={{ rows: 5 }} /></Card></Col>
        </Row>
      </div>
    );
  }
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
    <div className="jc-page">
      {/* Banner — dark accent header (replaces old green gradient) */}
      <div className="jc-banner">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: 16 }}>
          <div>
            <div className="jc-banner-sub">JoyCode API 代理服务 · 数据概览</div>
            <div className="jc-banner-title">系统运行状态</div>
          </div>
          <div className="jc-kpi-grid" style={{ flex: 1, maxWidth: 720 }}>
            <div className="jc-kpi jc-kpi-accent">
              <div className="jc-kpi-label">今日请求</div>
              <div className="jc-kpi-value">{stats.total_requests.toLocaleString()}</div>
            </div>
            <div className="jc-kpi jc-kpi-accent">
              <div className="jc-kpi-label">今日 Token</div>
              <div className="jc-kpi-value">{fmt(totalTokens)}</div>
            </div>
            <div className="jc-kpi">
              <div className="jc-kpi-label">累计请求</div>
              <div className="jc-kpi-value" style={{ fontSize: 20 }}>{(stats.all_time?.total_requests ?? 0).toLocaleString()}</div>
            </div>
            <div className="jc-kpi">
              <div className="jc-kpi-label">累计 Token</div>
              <div className="jc-kpi-value" style={{ fontSize: 20 }}>{fmt(allTimeTokens)}</div>
            </div>
            <div className="jc-kpi">
              <div className="jc-kpi-label">账号数</div>
              <div className="jc-kpi-value">{stats.accounts_count}</div>
            </div>
            <div className="jc-kpi jc-kpi-accent">
              <div className="jc-kpi-label">成功率</div>
              <div className="jc-kpi-value">{successRate}%</div>
            </div>
          </div>
        </div>
      </div>

      {/* 24h 时序图表 */}
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={12}>
          <Card size="small" title={<span className="jc-section-title"><ApiOutlined />24 小时请求趋势</span>}>
            <ResponsiveContainer width="100%" height={200}>
              <AreaChart data={hourlyChartData} margin={{ top: 5, right: 10, left: 0, bottom: 0 }}>
                <defs>
                  <linearGradient id="gradRequests" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor={CHART_COLORS.primary} stopOpacity={0.4} />
                    <stop offset="100%" stopColor={CHART_COLORS.primary} stopOpacity={0} />
                  </linearGradient>
                  <linearGradient id="gradErrors" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor={CHART_COLORS.danger} stopOpacity={0.3} />
                    <stop offset="100%" stopColor={CHART_COLORS.danger} stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke={CHART_COLORS.grid} />
                <XAxis dataKey="label" tick={{ fontSize: 10, fill: CHART_COLORS.axis }} interval={2} stroke={CHART_COLORS.grid} />
                <YAxis tick={{ fontSize: 11, fill: CHART_COLORS.axis }} stroke={CHART_COLORS.grid} />
                <Tooltip
                  contentStyle={{ background: '#0E1223', border: '1px solid #334155', borderRadius: 8, fontSize: 12 }}
                  labelStyle={{ color: '#94A3B8' }}
                  itemStyle={{ color: '#F8FAFC' }}
                  formatter={(v: unknown) => [Number(v).toLocaleString(), '请求数']}
                />
                <Area type="monotone" dataKey="requests" name="requests" stroke={CHART_COLORS.primary} fill="url(#gradRequests)" strokeWidth={2} />
                <Area type="monotone" dataKey="errors" name="errors" stroke={CHART_COLORS.danger} fill="url(#gradErrors)" strokeWidth={1.5} />
              </AreaChart>
            </ResponsiveContainer>
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card size="small" title={<span className="jc-section-title"><FireOutlined />24 小时 Token 消耗趋势</span>}>
            <ResponsiveContainer width="100%" height={200}>
              <AreaChart data={hourlyChartData} margin={{ top: 5, right: 10, left: 0, bottom: 0 }}>
                <defs>
                  <linearGradient id="gradTokens" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor={CHART_COLORS.secondary} stopOpacity={0.4} />
                    <stop offset="100%" stopColor={CHART_COLORS.secondary} stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke={CHART_COLORS.grid} />
                <XAxis dataKey="label" tick={{ fontSize: 10, fill: CHART_COLORS.axis }} interval={2} stroke={CHART_COLORS.grid} />
                <YAxis tick={{ fontSize: 11, fill: CHART_COLORS.axis }} stroke={CHART_COLORS.grid} tickFormatter={(v: number) => fmt(v)} />
                <Tooltip
                  contentStyle={{ background: '#0E1223', border: '1px solid #334155', borderRadius: 8, fontSize: 12 }}
                  labelStyle={{ color: '#94A3B8' }}
                  itemStyle={{ color: '#F8FAFC' }}
                  formatter={(v: unknown) => [fmt(Number(v)), 'Token 用量']}
                />
                <Area type="monotone" dataKey="tokens" stroke={CHART_COLORS.secondary} fill="url(#gradTokens)" strokeWidth={2} />
              </AreaChart>
            </ResponsiveContainer>
          </Card>
        </Col>
      </Row>

      {/* 统计面板：今日 + 累计 */}
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        {/* 请求统计 */}
        <Col xs={24} md={8}>
          <Card size="small" style={{ height: '100%' }} title={<span className="jc-section-title"><ApiOutlined />请求统计</span>}>
            <Row gutter={[8, 12]}>
              <Col span={12}>
                <Statistic title="今日请求" value={stats.total_requests} valueStyle={{ fontSize: 20, color: CHART_COLORS.primary }} />
              </Col>
              <Col span={12}>
                <Statistic title="累计请求" value={stats.all_time?.total_requests ?? 0} valueStyle={{ fontSize: 20 }} />
              </Col>
              <Col span={12}>
                <Statistic
                  title="今日成功"
                  value={stats.success_count}
                  prefix={<CheckCircleOutlined />}
                  valueStyle={{ fontSize: 18, color: CHART_COLORS.primary }}
                />
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>占比 {successRate}%</Typography.Text>
              </Col>
              <Col span={12}>
                <Statistic
                  title="今日失败"
                  value={stats.error_count}
                  prefix={<CloseCircleOutlined />}
                  valueStyle={{ fontSize: 18, color: stats.error_count > 0 ? CHART_COLORS.danger : CHART_COLORS.primary }}
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
          <Card size="small" style={{ height: '100%' }} title={<span className="jc-section-title"><FireOutlined />Token 消费</span>}>
            <Row gutter={[8, 12]}>
              <Col span={12}>
                <Statistic title="今日 Token" value={fmt(totalTokens)} valueStyle={{ fontSize: 20, color: CHART_COLORS.secondary }} />
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
          <Card size="small" style={{ height: '100%' }} title={<span className="jc-section-title"><DashboardOutlined />响应质量</span>}>
            <Row gutter={[8, 12]}>
              <Col span={12}>
                <Statistic
                  title="平均延迟"
                  value={fmtLatency(avgLatency)}
                  prefix={<ThunderboltOutlined />}
                  valueStyle={{ fontSize: 20, color: avgLatency < 5000 ? CHART_COLORS.primary : avgLatency < 15000 ? CHART_COLORS.warning : CHART_COLORS.danger }}
                />
              </Col>
              <Col span={12}>
                <Statistic
                  title="成功率"
                  value={successRate}
                  suffix="%"
                  prefix={<CheckCircleOutlined />}
                  valueStyle={{ fontSize: 20, color: successRate >= 95 ? CHART_COLORS.primary : successRate >= 80 ? CHART_COLORS.warning : CHART_COLORS.danger }}
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
        {/* 模型分布 — 横向条形图降序排列 + 值标签 (per skill chart guidance) */}
        <Col xs={24} lg={12}>
          <Card size="small" title={<span className="jc-section-title"><RiseOutlined />模型使用分布</span>}>
            {modelData.length > 0 ? (
              <Row>
                <Col xs={24} md={14}>
                  <ResponsiveContainer width="100%" height={220}>
                    <BarChart data={modelData} layout="vertical" margin={{ left: 10 }}>
                      <CartesianGrid strokeDasharray="3 3" stroke={CHART_COLORS.grid} />
                      <XAxis type="number" tick={{ fontSize: 11, fill: CHART_COLORS.axis }} stroke={CHART_COLORS.grid} />
                      <YAxis dataKey="name" type="category" width={110} tick={{ fontSize: 11, fill: CHART_COLORS.axis }} stroke={CHART_COLORS.grid} />
                      <Tooltip
                        contentStyle={{ background: '#0E1223', border: '1px solid #334155', borderRadius: 8, fontSize: 12 }}
                        itemStyle={{ color: '#F8FAFC' }}
                        labelStyle={{ color: '#94A3B8' }}
                        formatter={(v: unknown) => [Number(v).toLocaleString(), '请求数']}
                      />
                      <Bar dataKey="value" name="请求数" fill={CHART_COLORS.primary} radius={[0, 4, 4, 0]} />
                    </BarChart>
                  </ResponsiveContainer>
                </Col>
                <Col xs={24} md={10}>
                  <div style={{ padding: '4px 0 0 12px' }}>
                    {modelData.map((m, i) => (
                      <div key={m.name} style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '6px 0', borderBottom: '1px solid #1F2937' }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                          <div style={{ width: 8, height: 8, borderRadius: '50%', background: CATEGORY_COLORS[i % CATEGORY_COLORS.length], flexShrink: 0 }} />
                          <Typography.Text style={{ fontSize: 12 }} ellipsis>{m.name}</Typography.Text>
                        </div>
                        <div style={{ display: 'flex', gap: 6, alignItems: 'baseline', flexShrink: 0 }}>
                          <Typography.Text style={{ fontSize: 12, fontWeight: 600 }} className="jc-mono">{m.value.toLocaleString()}</Typography.Text>
                          <Typography.Text type="secondary" style={{ fontSize: 11 }}>{m.pct}%</Typography.Text>
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
          <Card size="small" title={<span className="jc-section-title"><TeamOutlined />账号请求分布</span>}>
            {accountData.length > 0 ? (
              <Row>
                <Col xs={24} md={14}>
                  <ResponsiveContainer width="100%" height={220}>
                    <BarChart data={accountData}>
                      <CartesianGrid strokeDasharray="3 3" stroke={CHART_COLORS.grid} />
                      <XAxis dataKey="name" tick={{ fontSize: 11, fill: CHART_COLORS.axis }} stroke={CHART_COLORS.grid} />
                      <YAxis tick={{ fontSize: 11, fill: CHART_COLORS.axis }} stroke={CHART_COLORS.grid} />
                      <Tooltip
                        contentStyle={{ background: '#0E1223', border: '1px solid #334155', borderRadius: 8, fontSize: 12 }}
                        itemStyle={{ color: '#F8FAFC' }}
                        labelStyle={{ color: '#94A3B8' }}
                        formatter={(v: unknown) => [Number(v).toLocaleString(), '请求数']}
                      />
                      <Bar dataKey="value" name="请求数" fill={CHART_COLORS.primary} radius={[4, 4, 0, 0]} />
                    </BarChart>
                  </ResponsiveContainer>
                </Col>
                <Col xs={24} md={10}>
                  <div style={{ padding: '4px 0 0 12px' }}>
                    {accountData.map((a, i) => (
                      <div key={a.name} style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '6px 0', borderBottom: '1px solid #1F2937' }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                          <div style={{ width: 8, height: 8, borderRadius: '50%', background: CATEGORY_COLORS[i % CATEGORY_COLORS.length], flexShrink: 0 }} />
                          <Typography.Text style={{ fontSize: 12 }} ellipsis>{a.name}</Typography.Text>
                        </div>
                        <div style={{ display: 'flex', gap: 6, alignItems: 'baseline', flexShrink: 0 }}>
                          <Typography.Text style={{ fontSize: 12, fontWeight: 600 }} className="jc-mono">{a.value.toLocaleString()}</Typography.Text>
                          <Typography.Text type="secondary" style={{ fontSize: 11 }}>{a.pct}%</Typography.Text>
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
          size="small"
          style={{ marginTop: 16 }}
          title={<span className="jc-section-title"><TeamOutlined />账号概览</span>}
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
        <Card style={{ marginTop: 16 }}>
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
