import React, { useState } from 'react';
import { Form, Input, Button, message, Typography, Progress } from 'antd';
import { LockOutlined, CheckCircleOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { authApi, setToken } from '../api';

const { Title, Text } = Typography;

const getPasswordStrength = (pw: string): number => {
  if (!pw) return 0;
  let score = 0;
  if (pw.length >= 6) score += 25;
  if (pw.length >= 10) score += 25;
  if (/[A-Z]/.test(pw) && /[a-z]/.test(pw)) score += 25;
  if (/[0-9]/.test(pw) && /[^a-zA-Z0-9]/.test(pw)) score += 25;
  return score;
};

const SetupPage: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();

  const handleSubmit = async (values: { password: string }) => {
    setLoading(true);
    try {
      const result = await authApi.setup(values.password);
      setToken(result.token);
      message.success('密码设置成功');
      navigate('/dashboard');
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '设置失败');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="jc-auth-bg">
      <div className="jc-auth-card" style={{ width: 460 }}>
        <div className="jc-auth-logo">
          <CheckCircleOutlined style={{ fontSize: 26, color: '#052e16' }} />
        </div>
        <div style={{ textAlign: 'center', marginBottom: 24 }}>
          <Title level={3} style={{ marginBottom: 4 }}>初始化 JoyCode 代理</Title>
          <Text type="secondary">首次使用，请设置 root 管理员密码</Text>
        </div>
        <Form onFinish={handleSubmit} size="large" autoComplete="off">
          <Form.Item shouldUpdate>
            {({ getFieldValue }) => {
              const pw = getFieldValue('password') || '';
              const strength = getPasswordStrength(pw);
              const color = strength <= 25 ? '#EF4444' : strength <= 50 ? '#F59E0B' : strength <= 75 ? '#22C55E' : '#16A34A';
              const label = strength <= 25 ? '弱' : strength <= 50 ? '中' : strength <= 75 ? '强' : '很强';
              return pw ? (
                <div style={{ marginTop: -8, marginBottom: 16 }}>
                  <Progress percent={strength} strokeColor={color} format={() => label} size="small" />
                </div>
              ) : null;
            }}
          </Form.Item>
          <Form.Item
            name="password"
            rules={[
              { required: true, message: '请输入密码' },
              { min: 6, message: '密码长度不能少于 6 位' },
            ]}
          >
            <Input.Password prefix={<LockOutlined />} placeholder="设置密码（至少 6 位）" autoFocus />
          </Form.Item>
          <Form.Item
            name="confirm"
            dependencies={['password']}
            rules={[
              { required: true, message: '请确认密码' },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || getFieldValue('password') === value) {
                    return Promise.resolve();
                  }
                  return Promise.reject(new Error('两次输入的密码不一致'));
                },
              }),
            ]}
          >
            <Input.Password prefix={<LockOutlined />} placeholder="确认密码" />
          </Form.Item>
          <Form.Item style={{ marginBottom: 0 }}>
            <Button
              type="primary"
              htmlType="submit"
              loading={loading}
              block
              style={{ height: 44 }}
            >
              设置密码并登录
            </Button>
          </Form.Item>
        </Form>
      </div>
    </div>
  );
};

export default SetupPage;
