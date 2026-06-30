import React, { useEffect, useState } from 'react';
import { Form, Input, Button, message, Typography, Tooltip } from 'antd';
import { LockOutlined, UserOutlined, QuestionCircleOutlined, GithubOutlined, StarFilled } from '@ant-design/icons';
import { useNavigate, Link } from 'react-router-dom';
import { authApi, setToken, api } from '../api';

const { Title, Text } = Typography;

const LoginPage: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const [stars, setStars] = useState<number | null>(null);
  const navigate = useNavigate();

  useEffect(() => {
    api.getGitHubStars().then(setStars).catch(() => {});
  }, []);

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
    <div className="jc-auth-bg">
      <Tooltip title="去 GitHub Star 支持我们">
        <a
          href="https://github.com/vibe-coding-labs/JoyCode2Api"
          target="_blank"
          rel="noopener noreferrer"
          style={{
            position: 'absolute',
            top: 20,
            right: 24,
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            color: 'rgba(248, 250, 252, 0.7)',
            fontSize: 13,
            textDecoration: 'none',
            transition: 'color 200ms ease',
          }}
          onMouseEnter={e => (e.currentTarget.style.color = '#F8FAFC')}
          onMouseLeave={e => (e.currentTarget.style.color = 'rgba(248, 250, 252, 0.7)')}
        >
          <GithubOutlined style={{ fontSize: 18 }} />
          GitHub
          {stars !== null && (
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 3, marginLeft: 2 }}>
              <StarFilled style={{ fontSize: 13, color: '#F59E0B' }} />
              <span style={{ fontSize: 12 }}>{stars.toLocaleString()}</span>
            </span>
          )}
        </a>
      </Tooltip>
      <div className="jc-auth-card">
        <div className="jc-auth-logo">
          <img src="/favicon.ico" alt="JoyCode" style={{ width: 28, height: 28, filter: 'brightness(0) invert(1)' }} />
        </div>
        <div style={{ textAlign: 'center', marginBottom: 28 }}>
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
              style={{ height: 44 }}
            >
              登录
            </Button>
          </Form.Item>
        </Form>
        <div style={{ textAlign: 'center', marginTop: 16 }}>
          <Link
            to="/forgot-password"
            style={{ color: '#94A3B8', fontSize: 13, display: 'inline-flex', alignItems: 'center', gap: 4, transition: 'color 200ms ease' }}
          >
            <QuestionCircleOutlined />
            忘记密码？
          </Link>
        </div>
      </div>
    </div>
  );
};

export default LoginPage;
