import React, { useEffect, useState } from 'react';
import { Button, Card, Typography, Alert } from 'antd';
import { ArrowLeftOutlined } from '@ant-design/icons';
import { Link } from 'react-router-dom';
import { authApi } from '../api';

const { Title, Text, Paragraph } = Typography;

type TokenType = 'prompt' | 'path' | 'subcommand' | 'flag' | 'value';

const TOKEN_COLORS: Record<TokenType, string> = {
  prompt: '#a6e3a1',
  path: '#89b4fa',
  subcommand: '#94e2d5',
  flag: '#f9e2af',
  value: '#fab387',
};

interface Token {
  text: string;
  type: TokenType;
}

function tokenizeCommand(cmd: string): Token[] {
  const tokens: Token[] = [];
  const parts = cmd.split(/\s+/);

  for (let i = 0; i < parts.length; i++) {
    const part = parts[i];
    if (i === 0) {
      tokens.push({ text: part, type: 'path' });
    } else if (i === 1) {
      tokens.push({ text: part, type: 'subcommand' });
    } else if (part.startsWith('-')) {
      tokens.push({ text: part, type: 'flag' });
      if (i + 1 < parts.length) {
        tokens.push({ text: parts.slice(i + 1).join(' '), type: 'value' });
        break;
      }
    } else {
      tokens.push({ text: parts.slice(i).join(' '), type: 'value' });
      break;
    }
  }

  return tokens;
}

const codeBlockStyle: React.CSSProperties = {
  background: '#1e1e2e',
  color: '#cdd6f4',
  padding: '14px 18px',
  borderRadius: 8,
  marginTop: 8,
  marginBottom: 20,
  fontSize: 14,
  fontFamily: "'SF Mono', 'Fira Code', 'Cascadia Code', Menlo, monospace",
  lineHeight: 1.6,
  overflow: 'auto',
  whiteSpace: 'pre-wrap' as const,
  wordBreak: 'break-all' as const,
};

function BashCode({ children }: { children: string }) {
  const tokens = tokenizeCommand(children);
  return (
    <pre style={codeBlockStyle}>
      <span style={{ color: TOKEN_COLORS.prompt, userSelect: 'none' }}>$ </span>
      {tokens.map((t, i) => (
        <React.Fragment key={i}>
          {i > 0 && ' '}
          <span style={{ color: TOKEN_COLORS[t.type] }}>{t.text}</span>
        </React.Fragment>
      ))}
    </pre>
  );
}

const ForgotPasswordPage: React.FC = () => {
  const [exePath, setExePath] = useState('./joycode_proxy_bin');

  useEffect(() => {
    authApi.status().then((res) => {
      if (res.exe_path) {
        setExePath(res.exe_path);
      }
    }).catch(() => {});
  }, []);

  return (
    <div style={{
      minHeight: '100vh',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      background: 'linear-gradient(135deg, #00b578 0%, #009a63 100%)',
    }}>
      <Card
        style={{ width: 640, borderRadius: 12, boxShadow: '0 8px 24px rgba(0,0,0,0.12)' }}
        styles={{ body: { padding: 32 } }}
      >
        <Title level={3} style={{ marginBottom: 8 }}>忘记密码</Title>
        <Paragraph type="secondary" style={{ marginBottom: 24 }}>
          Dashboard 的 root 密码需要通过服务器命令行重置。
        </Paragraph>

        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 20 }}
          message="在服务器终端执行以下命令"
        />

        <Text strong>交互式重置（会提示你输入新密码）：</Text>
        <BashCode>{`${exePath} reset-password`}</BashCode>

        <Text strong>直接指定新密码：</Text>
        <BashCode>{`${exePath} reset-password -p 你的新密码`}</BashCode>

        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 24 }}
          message={
            <span>
              密码至少 <Text strong>6 位</Text>，以 bcrypt 哈希加密存储在 SQLite 数据库中。
              重置后所有已登录的会话需要重新登录。
            </span>
          }
        />

        <Link to="/login">
          <Button icon={<ArrowLeftOutlined />} type="primary" ghost style={{ borderRadius: 6 }}>
            返回登录
          </Button>
        </Link>
      </Card>
    </div>
  );
};

export default ForgotPasswordPage;
