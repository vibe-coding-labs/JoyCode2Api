import React, { useState } from 'react';
import { Form, Input, Button, Card, message, Typography } from 'antd';
import { LockOutlined, UserOutlined, QuestionCircleOutlined } from '@ant-design/icons';
import { useNavigate, Link } from 'react-router-dom';
import { authApi, setToken } from '../api';

const { Title, Text } = Typography;

const LoginPage: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();

  const handleSubmit = async (values: { password: string }) => {
    setLoading(true);
    try {
      const result = await authApi.login(values.password);
      setToken(result.token);
      message.success('登录成功');
      navigate('/dashboard');
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '登录失败');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div style={{
      minHeight: '100vh',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      background: 'linear-gradient(135deg, #00b578 0%, #009a63 100%)',
    }}>
      <Card
        style={{ width: 400, borderRadius: 12, boxShadow: '0 8px 24px rgba(0,0,0,0.12)' }}
        styles={{ body: { padding: 32 } }}
      >
        <div style={{ textAlign: 'center', marginBottom: 24 }}>
          <Title level={3} style={{ marginBottom: 4 }}>JoyCode 代理</Title>
          <Text type="secondary">请输入 root 密码登录</Text>
        </div>
        <Form onFinish={handleSubmit} size="large">
          <Form.Item name="username" initialValue="root">
            <Input prefix={<UserOutlined />} disabled />
          </Form.Item>
          <Form.Item
            name="password"
            rules={[{ required: true, message: '请输入密码' }]}
          >
            <Input.Password prefix={<LockOutlined />} placeholder="root 密码" autoFocus />
          </Form.Item>
          <Form.Item style={{ marginBottom: 0 }}>
            <Button
              type="primary"
              htmlType="submit"
              loading={loading}
              block
              style={{ borderRadius: 6, height: 44 }}
            >
              登录
            </Button>
          </Form.Item>
        </Form>
        <div style={{ textAlign: 'center', marginTop: 16 }}>
          <Link
            to="/forgot-password"
            style={{ color: '#999', fontSize: 13, display: 'inline-flex', alignItems: 'center', gap: 4 }}
          >
            <QuestionCircleOutlined />
            忘记密码？
          </Link>
        </div>
      </Card>
    </div>
  );
};

export default LoginPage;
