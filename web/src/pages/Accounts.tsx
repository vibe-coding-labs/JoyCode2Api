import React, { useEffect, useState } from 'react';
import {
  Alert, Button, Form, Input, message, Modal, Popconfirm, Select, Space, Switch, Table, Tag, Typography,
} from 'antd';
import {
  CheckCircleOutlined, ClockCircleOutlined, CloseCircleOutlined, DeleteOutlined, EditOutlined,
  HolderOutlined, PlusOutlined, ReloadOutlined, SafetyCertificateOutlined, StarOutlined, CopyOutlined,
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
import { useNavigate } from 'react-router-dom';
import SvgClaudeCode from '../components/ClaudeCodeIcon';
import SvgCodex from '../components/CodexIcon';
import CommandTooltip from '../components/CommandTooltip';
import { api, accountDisplayName } from '../api';
import type { Account } from '../api';

const BUILTIN_MODELS = [
  { label: 'JoyAI-Code（推荐）', value: 'JoyAI-Code' },
  { label: 'MiniMax-M2.7', value: 'MiniMax-M2.7' },
  { label: 'Kimi-K2.6', value: 'Kimi-K2.6' },
  { label: 'GLM-5.1', value: 'GLM-5.1' },
  { label: 'GLM-5', value: 'GLM-5' },
  { label: 'GLM-5-jcloud', value: 'GLM-5-jcloud' },
  { label: 'Doubao-Seed-2.0-pro', value: 'Doubao-Seed-2.0-pro' },
  { label: 'Claude-Opus-4.7', value: 'Claude-Opus-4.7' },
  { label: 'Claude-Sonnet-4.6', value: 'Claude-Sonnet-4.6' },
  { label: 'Claude-Opus-4.6', value: 'Claude-Opus-4.6' },
  { label: 'GPT-5.3-codex', value: 'GPT-5.3-codex' },
];

const getBaseURL = () => `${window.location.protocol}//${window.location.host}`;

const maskUserId = (id: string) => {
  if (!id) return '-';
  if (id.length <= 3) return `${id[0]}***`;
  return `${id.slice(0, 2)}***${id.slice(-2)}`;
};

const fmtTokens = (n: number) => {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
};

const keyLabel = (set: boolean, suffix?: string) => {
  if (!set) return '未配置';
  return suffix ? `已配置 · ...${suffix}` : '已配置';
};

const claudeCodeCmd = (apiKey: string, model = 'GLM-5.1') => [
  'API_TIMEOUT_MS=6000000 \\',
  'CLAUDE_CODE_MAX_RETRIES=1000000 \\',
  'NODE_TLS_REJECT_UNAUTHORIZED=0 \\',
  `ANTHROPIC_BASE_URL=${getBaseURL()} \\`,
  `ANTHROPIC_API_KEY="${apiKey}" \\`,
  'CLAUDE_CODE_MAX_OUTPUT_TOKENS=6553655 \\',
  `ANTHROPIC_MODEL=${model} \\`,
  'claude --dangerously-skip-permissions',
].join('\n');

const codexCmd = (apiKey: string, model = 'GLM-5.1') => [
  `OPENAI_BASE_URL=${getBaseURL()}/v1 \\`,
  `OPENAI_API_KEY="${apiKey}" \\`,
  `OPENAI_MODEL=${model} \\`,
  'codex',
].join('\n');

const copyToClipboard = async (text: string, label: string) => {
  try {
    await navigator.clipboard?.writeText(text);
    message.success(`${label}已复制`);
  } catch {
    message.error('复制失败');
  }
};

interface DraggableRowProps extends React.HTMLAttributes<HTMLTableRowElement> {
  'data-row-key': string;
}

const DraggableRow: React.FC<DraggableRowProps> = (props) => {
  const { attributes, setNodeRef, transform, transition, isDragging } = useSortable({ id: props['data-row-key'] });
  const style: React.CSSProperties = {
    ...props.style,
    transform: CSS.Transform.toString(transform && { ...transform, scaleY: 1 }),
    transition,
    ...(isDragging ? { position: 'relative', zIndex: 9999 } : {}),
  };
  return <tr {...props} ref={setNodeRef} style={style} {...attributes} />;
};

const DragHandle: React.FC<{ id: string }> = ({ id }) => {
  const { listeners, setActivatorNodeRef } = useSortable({ id });
  return (
    <td ref={setActivatorNodeRef} {...listeners} style={{ cursor: 'grab', width: 40, textAlign: 'center' }}>
      <HolderOutlined style={{ color: '#999' }} />
    </td>
  );
};

const Accounts: React.FC = () => {
  const navigate = useNavigate();
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [validating, setValidating] = useState<string | null>(null);
  const [copyingCredential, setCopyingCredential] = useState<string | null>(null);
  const [clearingLogs, setClearingLogs] = useState(false);
  const [renameModalOpen, setRenameModalOpen] = useState(false);
  const [renameTarget, setRenameTarget] = useState('');
  const [form] = Form.useForm();
  const [renameForm] = Form.useForm();

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor),
  );

  const fetchAccounts = async () => {
    setLoading(true);
    try {
      setAccounts(await api.listAccounts());
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

  const handleCredentialLogin = async () => {
    try {
      const result = await api.ideLogin();
      window.open(result.url, '_blank');
      message.info('请在打开的授权页面完成登录，成功后会自动保存账号凭证');
      setTimeout(() => fetchAccounts(), 10000);
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '获取授权登录链接失败');
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
      if (result.valid) message.success(`账号「${displayName}」验证通过，凭证有效`);
      else message.error(`账号「${displayName}」验证失败，凭证无效或已过期`);
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '验证请求失败');
    } finally {
      setValidating(null);
    }
  };

  const handleCopyCredential = async (userId: string) => {
    setCopyingCredential(userId);
    try {
      const result = await api.getAccountCredential(userId);
      await copyToClipboard(result.credential, '授权凭证');
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '复制授权凭证失败');
    } finally {
      setCopyingCredential(null);
    }
  };

  const handleClearLogs = async () => {
    setClearingLogs(true);
    try {
      const result = await api.clearRequestLogs();
      message.success(`已清理 ${result.deleted} 条请求日志`);
      fetchAccounts();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '清理请求日志失败');
    } finally {
      setClearingLogs(false);
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
    const next = arrayMove(accounts, oldIndex, newIndex);
    setAccounts(next);
    try {
      await api.reorderAccounts(next.map((a) => a.user_id));
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '保存排序失败');
      fetchAccounts();
    }
  };

  const columns = [
    { title: '', key: 'drag', width: 40, render: (_: unknown, record: Account) => <DragHandle id={record.user_id} /> },
    {
      title: '账户名', dataIndex: 'user_id', key: 'user_id', width: 140,
      render: (_: unknown, record: Account) => <Typography.Text strong>{accountDisplayName(record)}</Typography.Text>,
    },
    {
      title: '凭证', key: 'credentials', width: 300,
      render: (_: unknown, record: Account) => (
        <Space direction="vertical" size={2}>
          <Space size={4} wrap={false}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>用户 ID: {maskUserId(record.user_id)}</Typography.Text>
            <Button
              type="text"
              size="small"
              icon={<CopyOutlined />}
              onClick={(e) => { e.stopPropagation(); copyToClipboard(record.user_id, '用户 ID'); }}
            />
          </Space>
          <Space size={4} wrap={false}>
            <Typography.Text type="secondary" style={{ fontSize: 12, whiteSpace: 'nowrap' }}>授权凭证:</Typography.Text>
            <Tag color={record.claude_pt_key_set ? 'blue' : 'orange'} style={{ marginInlineEnd: 0 }}>{keyLabel(record.claude_pt_key_set, record.claude_pt_key_suffix)}</Tag>
            <Button
              type="text"
              size="small"
              icon={<CopyOutlined />}
              disabled={!record.claude_pt_key_set && !record.pt_key_set}
              loading={copyingCredential === record.user_id}
              onClick={(e) => { e.stopPropagation(); handleCopyCredential(record.user_id); }}
            />
          </Space>
        </Space>
      ),
    },
    {
      title: 'API Token', dataIndex: 'api_token', key: 'api_token', width: 180,
      render: (token: string) => (
        <Space size={4} wrap={false}>
          <Typography.Text code style={{ fontSize: 12 }}>{token.slice(0, 12)}...{token.slice(-4)}</Typography.Text>
          <Button
            type="text"
            size="small"
            icon={<CopyOutlined />}
            onClick={(e) => { e.stopPropagation(); copyToClipboard(token, 'API Token'); }}
          />
        </Space>
      ),
    },
    {
      title: '活跃会话', dataIndex: 'active_sessions', key: 'active_sessions', width: 100,
      render: (val: number) => val > 0 ? <Tag color="blue">{val} 个活跃</Tag> : <Typography.Text type="secondary">无</Typography.Text>,
    },
    {
      title: '今日请求', dataIndex: 'today_requests', key: 'today_requests', width: 100,
      render: (val: number, record: Account) => (
        <div style={{ lineHeight: 1.4 }}>
          <Typography.Text strong style={{ fontSize: 13 }}>{val}</Typography.Text><br />
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>累计 {record.total_requests}</Typography.Text>
        </div>
      ),
    },
    {
      title: '今日 Token', dataIndex: 'today_tokens', key: 'today_tokens', width: 100,
      render: (val: number, record: Account) => (
        <div style={{ lineHeight: 1.4 }}>
          <Typography.Text strong style={{ fontSize: 13 }}>{fmtTokens(val)}</Typography.Text><br />
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>累计 {fmtTokens(record.total_tokens)}</Typography.Text>
        </div>
      ),
    },
    {
      title: '凭证状态', key: 'credential_status', width: 110,
      render: (_: unknown, record: Account) => {
        if (record.credential_valid === 1) return <Tag color="success" icon={<CheckCircleOutlined />}>有效</Tag>;
        if (record.credential_valid === 0) return <Tag color="error" icon={<CloseCircleOutlined />}>已过期</Tag>;
        return <Tag color="processing" icon={<ClockCircleOutlined />}>首次检测中</Tag>;
      },
    },
    { title: '状态', dataIndex: 'is_default', key: 'is_default', width: 100, render: (val: boolean) => val ? <Tag color="blue"><StarOutlined /> 默认账号</Tag> : null },
    { title: '默认模型', dataIndex: 'default_model', key: 'default_model', width: 150, render: (val: string) => val ? <Tag color="green">{val}</Tag> : <Typography.Text type="secondary">未设置</Typography.Text> },
    {
      title: '快速启动', key: 'quickstart', width: 90,
      render: (_: unknown, record: Account) => {
        const claudeCmd = claudeCodeCmd(record.api_token, record.default_model || undefined);
        const cxCmd = codexCmd(record.api_token, record.default_model || undefined);
        return (
          <Space size={4}>
            <CommandTooltip command={claudeCmd} label="Claude Code">
              <Button type="text" size="small" icon={<SvgClaudeCode />} onClick={(e) => { e.stopPropagation(); copyToClipboard(claudeCmd, 'Claude Code'); }} />
            </CommandTooltip>
            <CommandTooltip command={cxCmd} label="Codex">
              <Button type="text" size="small" icon={<SvgCodex />} onClick={(e) => { e.stopPropagation(); copyToClipboard(cxCmd, 'Codex'); }} />
            </CommandTooltip>
          </Space>
        );
      },
    },
    {
      title: '操作', key: 'actions',
      render: (_: unknown, record: Account) => (
        <Space>
          <Button size="small" onClick={(e) => { e.stopPropagation(); setRenameTarget(record.user_id); renameForm.setFieldsValue({ new_name: record.remark || accountDisplayName(record) }); setRenameModalOpen(true); }}><EditOutlined /> 备注</Button>
          {!record.is_default && <Button size="small" onClick={(e) => { e.stopPropagation(); handleSetDefault(record.user_id, accountDisplayName(record)); }}><StarOutlined /> 设为默认</Button>}
          <Popconfirm title="确定要重置 API Token 吗？" description="重置后旧 Token 将立即失效" onConfirm={() => handleRenewToken(record.user_id)}>
            <Button size="small" onClick={(e) => e.stopPropagation()}>重置 Token</Button>
          </Popconfirm>
          <Button size="small" onClick={(e) => { e.stopPropagation(); handleValidate(record.user_id, accountDisplayName(record)); }} loading={validating === record.user_id}><SafetyCertificateOutlined /> 验证</Button>
          <Popconfirm title={`确定要删除账号「${accountDisplayName(record)}」吗？`} description="删除后使用该密钥的客户端将无法访问" onConfirm={() => handleRemove(record.user_id, accountDisplayName(record))}>
            <Button size="small" danger onClick={(e) => e.stopPropagation()}><DeleteOutlined /> 删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', gap: 12 }}>
        <Typography.Title level={4} style={{ margin: 0 }}>账号管理</Typography.Title>
        <Space wrap>
          <Button onClick={fetchAccounts} icon={<ReloadOutlined />}>刷新</Button>
          <Popconfirm
            title="清理请求日志"
            description="只会删除请求日志和统计记录，不会删除账号、API Token 或授权凭证。"
            okText="清理"
            cancelText="取消"
            onConfirm={handleClearLogs}
          >
            <Button danger icon={<DeleteOutlined />} loading={clearingLogs}>清理请求日志</Button>
          </Popconfirm>
          <Button type="primary" onClick={handleCredentialLogin} icon={<SafetyCertificateOutlined />}>授权登录</Button>
          <Button onClick={() => setModalOpen(true)} icon={<PlusOutlined />}>手动添加</Button>
        </Space>
      </div>
      <Alert
        type="info"
        showIcon
        message="授权模式"
        description="账号凭证通过 JoyCode IDE 授权获取。客户端通过 API Token 路由到对应账号。"
        style={{ marginBottom: 16 }}
      />

      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
        <SortableContext items={accounts.map((a) => a.user_id)} strategy={verticalListSortingStrategy}>
          <Table
            dataSource={accounts}
            columns={columns}
            rowKey="user_id"
            loading={loading}
            pagination={false}
            scroll={{ x: 1320 }}
            components={{ body: { row: DraggableRow } }}
            onRow={(record) => ({ onClick: () => navigate(`/accounts/${encodeURIComponent(record.user_id)}`), style: { cursor: 'pointer' } })}
            locale={{ emptyText: '暂无账号，请点击「授权登录」或「手动添加」配置账号' }}
          />
        </SortableContext>
      </DndContext>

      <Modal title="手动添加 JoyCode 账号" open={modalOpen} onCancel={() => { setModalOpen(false); form.resetFields(); }} onOk={() => form.submit()} okText="添加" cancelText="取消" width={560}>
        <Alert type="info" showIcon message="手动添加账号凭证" description="粘贴 JoyCode IDE 授权后获得的账号凭证。" style={{ marginBottom: 16 }} />
        <Form form={form} layout="vertical" onFinish={handleAdd}>
          <Form.Item name="pt_key" label="账号凭证" rules={[{ required: true, message: '请输入账号凭证' }]}>
            <Input.Password placeholder="粘贴 JoyCode IDE 授权凭证" />
          </Form.Item>
          <Form.Item name="user_id" label="JoyCode 用户 ID" rules={[{ required: true, message: '请输入用户 ID' }]}>
            <Input placeholder="例如：jd_xxx" />
          </Form.Item>
          <Form.Item name="default_model" label="默认模型">
            <Select placeholder="留空使用系统默认模型" options={BUILTIN_MODELS} allowClear />
          </Form.Item>
          <Form.Item name="is_default" valuePropName="checked" label="设为默认账号">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>

      <Modal title="修改账号备注" open={renameModalOpen} onCancel={() => { setRenameModalOpen(false); renameForm.resetFields(); }} onOk={() => renameForm.submit()} okText="确认" cancelText="取消">
        <Form form={renameForm} layout="vertical" onFinish={handleRename}>
          <Form.Item name="new_name" label="备注名" rules={[{ required: true, message: '请输入备注名' }]}>
            <Input placeholder="输入备注名，例如：我的主账号" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default Accounts;
