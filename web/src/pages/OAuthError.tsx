import React from 'react';
import { Button, Card, Typography } from 'antd';
import { CloseCircleOutlined, LoginOutlined, HomeOutlined } from '@ant-design/icons';
import { useNavigate, useSearchParams } from 'react-router-dom';

const { Title, Text, Paragraph } = Typography;

const OAuthError: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const error = searchParams.get('error') || '未知错误';

  return (
    <div style={{
      minHeight: '100vh',
      display: 'flex',
      justifyContent: 'center',
      alignItems: 'center',
      background: '#f0f2f5',
    }}>
      <Card style={{ width: 480, textAlign: 'center', borderRadius: 12 }}>
        <CloseCircleOutlined style={{ fontSize: 64, color: '#ff4d4f', marginBottom: 16 }} />
        <Title level={3}>OAuth 授权失败</Title>
        <Paragraph type="secondary" style={{ fontSize: 14 }}>
          授权过程中发生错误，账号未能添加成功。
        </Paragraph>
        <Card
          size="small"
          style={{
            background: '#fff2f0',
            border: '1px solid #ffccc7',
            marginBottom: 24,
            textAlign: 'left',
          }}
        >
          <Text type="danger" style={{ fontSize: 13, wordBreak: 'break-all' }}>
            {error}
          </Text>
        </Card>
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
      </Card>
    </div>
  );
};

export default OAuthError;
