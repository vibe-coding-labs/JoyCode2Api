import React, { useEffect, useState } from 'react';
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

const getBaseURL = () => `${window.location.protocol}//${window.location.host}`;

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
  `NODE_TLS_REJECT_UNAUTHORIZED=0 \\`,
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
  const { attributes, setNodeRef, transform, transition, isDragging } = useSortable({
    id: props['data-row-key'],
  });

  const style: React.CSSProperties = {
    ...props.style,
    transform: CSS.Transform.toString(transform && { ...transform, scaleY: 1 }),
    transition,
    ...(isDragging ? { position: 'relative', zIndex: 9999 } : {}),
  };

  return (
    <tr
      {...props}
      ref={setNodeRef}
      style={style}
      {...attributes}
    />
  );
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
      render: (_: unknown, record: Account) => <DragHandle id={record.user_id} />,
    },
    {
      title: '账户名',
      dataIndex: 'user_id',
      key: 'user_id',
      width: 120,
      render: (_: unknown, record: Account) => (
        <Typography.Text strong>{accountDisplayName(record)}</Typography.Text>
      ),
    },
    {
      title: 'API Token',
      dataIndex: 'api_token',
      key: 'api_token',
      width: 180,
      render: (token: string) => (
        <Typography.Text code copyable style={{ fontSize: 12 }}>
          {token.slice(0, 12)}...{token.slice(-4)}
        </Typography.Text>
      ),
    },
    {
      title: '用户 ID',
      dataIndex: 'user_id',
      key: 'user_id_uid',
      width: 120,
      render: (text: string) => (
        <Typography.Text type="secondary" style={{ fontSize: 13 }}>{maskUserId(text)}</Typography.Text>
      ),
    },
    {
      title: '活跃会话',
      dataIndex: 'active_sessions',
      key: 'active_sessions',
      width: 100,
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
      width: 100,
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
      width: 100,
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
      width: 100,
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
      width: 100,
      render: (val: boolean) => val ? <Tag color="blue"><StarOutlined /> 默认账号</Tag> : null,
    },
    {
      title: '默认模型',
      dataIndex: 'default_model',
      key: 'default_model',
      width: 150,
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
        description="每个账号对应一个 JoyCode 后端凭证。客户端通过 API Token 来指定使用哪个账号。配置 Claude Code 时，将 API Token 填入 ANTHROPIC_API_KEY 环境变量即可。拖动行左侧手柄可调整排序。"
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
              style: { cursor: 'pointer' },
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
