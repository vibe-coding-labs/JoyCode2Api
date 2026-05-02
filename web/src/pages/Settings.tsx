import React, { useEffect, useState } from 'react';
import {
  Card, Form, Input, Button, InputNumber, Select, Switch, message, Modal,
  Spin, Typography, Space, Row, Col, Tag, Tooltip,
} from 'antd';
import {
  SaveOutlined, ReloadOutlined, QuestionCircleOutlined,
  SettingOutlined, CheckCircleOutlined, InfoCircleOutlined, LockOutlined,
} from '@ant-design/icons';
import { api, authApi, clearToken } from '../api';
import type { Settings } from '../api';

const { Text } = Typography;

interface FieldConfig {
  key: string;
  label: string;
  tooltip: string;
  placeholder: string;
  type: 'input' | 'number' | 'select' | 'switch';
  options?: { label: string; value: string }[];
  suffix?: string;
  readOnly?: boolean;
  tag?: string;
}

const FIELD_GROUPS = [
  {
    title: '模型配置',
    fields: [
      {
        key: 'default_model',
        label: '默认模型',
        tag: '已生效',
        tooltip: '当客户端未指定模型，且账号未配置默认模型时使用的 JoyCode 模型',
        placeholder: 'JoyAI-Code',
        type: 'select' as const,
        options: [
          { label: 'JoyAI-Code — 主力代码模型（推荐）', value: 'JoyAI-Code' },
          { label: 'GLM-5.1 — 智谱 GLM 5.1', value: 'GLM-5.1' },
          { label: 'GLM-5 — 智谱 GLM 5', value: 'GLM-5' },
          { label: 'GLM-4.7 — 智谱 GLM 4.7', value: 'GLM-4.7' },
          { label: 'Kimi-K2.6 — Moonshot Kimi K2.6', value: 'Kimi-K2.6' },
          { label: 'Kimi-K2.5 — Moonshot Kimi K2.5', value: 'Kimi-K2.5' },
          { label: 'MiniMax-M2.7 — MiniMax M2.7', value: 'MiniMax-M2.7' },
          { label: 'Doubao-Seed-2.0-pro — 豆包 Seed 2.0 Pro', value: 'Doubao-Seed-2.0-pro' },
        ],
      },
      {
        key: 'default_max_tokens',
        label: '默认最大输出 Token',
        tooltip: '客户端未指定 max_tokens 时的默认值。更大值允许更长回复，但消耗更多配额',
        placeholder: '8192',
        type: 'number' as const,
        tag: '已生效',
      },
    ],
  },
  {
    title: '连接优化',
    fields: [
      {
        key: 'max_retries',
        label: '最大重试次数',
        tooltip: '请求失败时的自动重试次数。网络不稳定时可适当增加',
        placeholder: '3',
        type: 'number' as const,
        tag: '已生效',
      },
      {
        key: 'request_timeout',
        label: '请求超时（秒）',
        tooltip: '与 JoyCode 后端通信的读取超时时间，低于 60 秒会自动调整为 60 秒',
        placeholder: '120',
        type: 'number' as const,
        suffix: '秒',
        tag: '已生效',
      },
      {
        key: 'max_connections',
        label: '最大连接数',
        tooltip: '与 JoyCode 后端的最大并发 HTTP 连接数，修改后 10 秒内自动生效',
        placeholder: '20',
        type: 'number' as const,
        tag: '已生效',
      },
    ],
  },
  {
    title: '日志与监控',
    fields: [
      {
        key: 'enable_request_logging',
        label: '启用请求日志',
        tooltip: '记录每个 API 请求的详细信息（模型、延迟、状态码）。关闭后「数据概览」页面将无数据',
        placeholder: 'true',
        type: 'switch' as const,
        tag: '已生效',
      },
      {
        key: 'log_retention_days',
        label: '日志保留天数',
        tooltip: '请求日志的自动清理周期。超过此天数的日志将每小时自动清理，0 表示永久保留',
        placeholder: '30',
        type: 'number' as const,
        suffix: '天',
        tag: '已生效',
      },
    ],
  },
];

const SettingsPage: React.FC = () => {
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [changePwLoading, setChangePwLoading] = useState(false);
  const [form] = Form.useForm();
  const [pwForm] = Form.useForm();

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
      message.success('设置已保存');
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '保存设置失败');
    } finally {
      setSaving(false);
    }
  };

  const handleChangePassword = async (values: { old_password: string; new_password: string }) => {
    Modal.confirm({
      title: '确认修改密码',
      content: '修改密码后需要重新登录，确定要继续吗？',
      okText: '确认修改',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: async () => {
        setChangePwLoading(true);
        try {
          await authApi.changePassword(values.old_password, values.new_password);
          message.success('密码修改成功，请重新登录');
          pwForm.resetFields();
          clearToken();
          setTimeout(() => { window.location.href = '/login'; }, 1000);
        } catch (e: unknown) {
          message.error(e instanceof Error ? e.message : '密码修改失败');
        } finally {
          setChangePwLoading(false);
        }
      },
    });
  };

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;

  const renderField = (field: FieldConfig) => {
    const label = (
      <Space size={4}>
        {field.label}
        <Tooltip title={field.tooltip}><QuestionCircleOutlined style={{ color: '#bbb' }} /></Tooltip>
        {field.tag && (
          <Tag color={field.tag === '已生效' ? 'success' : 'default'} style={{ marginLeft: 4, fontSize: 11 }}>
            {field.tag === '已生效' ? <CheckCircleOutlined /> : <InfoCircleOutlined />} {field.tag}
          </Tag>
        )}
      </Space>
    );

    switch (field.type) {
      case 'number':
        return (
          <Form.Item key={field.key} name={field.key} label={label}>
            <InputNumber
              style={{ width: '100%' }}
              placeholder={field.placeholder}
              addonAfter={field.suffix}
              disabled={field.readOnly}
            />
          </Form.Item>
        );
      case 'select':
        return (
          <Form.Item key={field.key} name={field.key} label={label}>
            <Select placeholder={field.placeholder} options={field.options} allowClear disabled={field.readOnly} />
          </Form.Item>
        );
      case 'switch':
        return (
          <Form.Item key={field.key} name={field.key} valuePropName="checked" label={label}>
            <Switch />
          </Form.Item>
        );
      default:
        return (
          <Form.Item key={field.key} name={field.key} label={label}>
            <Input placeholder={field.placeholder} disabled={field.readOnly} />
          </Form.Item>
        );
    }
  };

  return (
    <div>
      <Card
        style={{
          marginBottom: 16,
          background: 'linear-gradient(135deg, #00b578 0%, #009a63 100%)',
          border: 'none',
          borderRadius: 12,
        }}
        styles={{ body: { padding: '20px 24px' } }}
      >
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <div>
            <Text style={{ color: 'rgba(255,255,255,0.85)', fontSize: 13 }}>
              JoyCode API 代理服务 · 系统设置
            </Text>
            <div style={{ color: '#fff', fontSize: 22, fontWeight: 700, marginTop: 4 }}>
              代理配置管理
            </div>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <Button
              ghost
              style={{ color: '#fff', borderColor: 'rgba(255,255,255,0.4)' }}
              icon={<ReloadOutlined />}
              onClick={fetchSettings}
            >
              刷新
            </Button>
          </div>
        </div>
      </Card>

      <Form form={form} layout="vertical" onFinish={handleSave}>
        {FIELD_GROUPS.map((group) => (
          <Card
            key={group.title}
            title={<Text strong style={{ fontSize: 15 }}>{group.title}</Text>}
            style={{ marginBottom: 16, borderRadius: 8, border: '1px solid #f0f0f0' }}
            styles={{ body: { padding: '20px 24px' } }}
            extra={
              <SettingOutlined style={{ color: '#00b578' }} />
            }
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

        <Card
          title={<Text strong style={{ fontSize: 15 }}>安全设置</Text>}
          style={{ marginBottom: 16, borderRadius: 8, border: '1px solid #f0f0f0' }}
          styles={{ body: { padding: '20px 24px' } }}
          extra={<SettingOutlined style={{ color: '#00b578' }} />}
        >
          <Form form={pwForm} layout="vertical" onFinish={handleChangePassword}>
            <Row gutter={[24, 0]}>
              <Col xs={24} md={8}>
                <Form.Item name="old_password" label="当前密码" rules={[{ required: true, message: '请输入当前密码' }]}>
                  <Input.Password placeholder="输入当前密码" />
                </Form.Item>
              </Col>
              <Col xs={24} md={8}>
                <Form.Item name="new_password" label="新密码" rules={[
                  { required: true, message: '请输入新密码' },
                  { min: 6, message: '密码长度不能少于 6 位' },
                ]}>
                  <Input.Password placeholder="输入新密码（至少 6 位）" />
                </Form.Item>
              </Col>
              <Col xs={24} md={8}>
                <Form.Item label="确认新密码" dependencies={['new_password']} rules={[
                  { required: true, message: '请确认新密码' },
                  ({ getFieldValue }) => ({
                    validator(_, value) {
                      if (!value || getFieldValue('new_password') === value) {
                        return Promise.resolve();
                      }
                      return Promise.reject(new Error('两次输入的密码不一致'));
                    },
                  }),
                ]} name="confirm_password">
                  <Input.Password placeholder="再次输入新密码" />
                </Form.Item>
              </Col>
            </Row>
            <Button
              type="primary"
              htmlType="submit"
              loading={changePwLoading}
              icon={<LockOutlined />}
              style={{ borderRadius: 6 }}
            >
              修改密码
            </Button>
          </Form>
        </Card>

        <div style={{ display: 'flex', gap: 12, marginTop: 8 }}>
          <Button
            type="primary"
            htmlType="submit"
            loading={saving}
            icon={<SaveOutlined />}
            size="large"
            style={{ borderRadius: 6 }}
          >
            保存设置
          </Button>
          <Button onClick={fetchSettings} icon={<ReloadOutlined />} size="large">
            恢复当前值
          </Button>
        </div>
      </Form>
    </div>
  );
};

export default SettingsPage;
