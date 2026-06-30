import React from 'react';
import { Button, Typography } from 'antd';
import { CloseCircleOutlined, LoginOutlined, HomeOutlined } from '@ant-design/icons';
import { useNavigate, useSearchParams } from 'react-router-dom';

const { Title, Text, Paragraph } = Typography;

const OAuthError: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const error = searchParams.get('error') || '未知错误';

  return (
    <div className="jc-auth-bg">
      <div className="jc-auth-card" style={{ width: 500, textAlign: 'center' }}>
        <div className="jc-auth-logo" style={{ background: 'linear-gradient(135deg, #EF4444, #DC2626)', boxShadow: '0 8px 20px rgba(239, 68, 68, 0.25)' }}>
          <CloseCircleOutlined style={{ fontSize: 26, color: '#fff' }} />
        </div>
        <Title level={3}>OAuth 授权失败</Title>
        <Paragraph type="secondary" style={{ fontSize: 14 }}>
          授权过程中发生错误，账号未能添加成功。
        </Paragraph>
        <div style={{
          background: 'rgba(239, 68, 68, 0.08)',
          border: '1px solid rgba(239, 68, 68, 0.3)',
          borderRadius: 8,
          padding: '12px 16px',
          marginBottom: 24,
          textAlign: 'left',
        }}>
          <Text type="danger" style={{ fontSize: 13, wordBreak: 'break-all' }}>
            {error}
          </Text>
        </div>
        <div style={{ display: 'flex', gap: 12, justifyContent: 'center' }}>
          <Button
            icon={<LoginOutlined />}
            onClick={() => navigate('/accounts')}
          >
            返回账号管理
          </Button>
          <Button
            type="primary"
            icon={<HomeOutlined />}
            onClick={() => navigate('/dashboard')}
          >
            返回首页
          </Button>
        </div>
      </div>
    </div>
  );
};

export default OAuthError;
