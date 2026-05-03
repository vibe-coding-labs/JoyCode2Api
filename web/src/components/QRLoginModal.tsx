import React, { useEffect, useState, useRef, useCallback } from 'react';
import { Modal, Typography, Button, Space, Alert, Spin } from 'antd';
import { ReloadOutlined, CheckCircleOutlined, CloseCircleOutlined, SafetyCertificateOutlined, LoginOutlined } from '@ant-design/icons';
import { api } from '../api';

interface QRLoginModalProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
  onAutoLogin: () => void;
}

const QRLoginModal: React.FC<QRLoginModalProps> = ({ open, onClose, onSuccess, onAutoLogin }) => {
  const [qrImage, setQrImage] = useState('');
  const [status, setStatus] = useState<'loading' | 'waiting' | 'scanned' | 'confirmed' | 'expired' | 'error' | 'verification_required'>('loading');
  const [countdown, setCountdown] = useState(180);
  const [errorMsg, setErrorMsg] = useState('');
  const [verifyURL, setVerifyURL] = useState('');
  const [pollTrigger, setPollTrigger] = useState(0);
  const sessionIdRef = useRef('');
  const pollTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const onSuccessRef = useRef(onSuccess);
  const onCloseRef = useRef(onClose);
  const onAutoLoginRef = useRef(onAutoLogin);

  onSuccessRef.current = onSuccess;
  onCloseRef.current = onClose;
  onAutoLoginRef.current = onAutoLogin;

  const initQR = useCallback(async () => {
    setStatus('loading');
    setCountdown(180);
    setErrorMsg('');
    setVerifyURL('');
    try {
      const result = await api.qrLoginInit();
      setQrImage(result.qr_image);
      sessionIdRef.current = result.session_id;
      setStatus('waiting');
      setPollTrigger(c => c + 1);
    } catch (e: unknown) {
      setStatus('error');
      setErrorMsg(e instanceof Error ? e.message : '生成二维码失败');
    }
  }, []);

  useEffect(() => {
    if (open) {
      initQR();
    } else {
      setQrImage('');
      sessionIdRef.current = '';
      setStatus('loading');
      if (pollTimerRef.current) clearTimeout(pollTimerRef.current);
    }
  }, [open, initQR]);

  useEffect(() => {
    if (!open) return;
    if (status !== 'waiting' && status !== 'scanned') return;

    const poll = async () => {
      const sid = sessionIdRef.current;
      if (!sid) {
        pollTimerRef.current = setTimeout(poll, 1000);
        return;
      }
      try {
        const result = await api.qrLoginStatus(sid);
        if (result.status === 'confirmed') {
          setStatus('confirmed');
          setTimeout(() => {
            onSuccessRef.current();
            onCloseRef.current();
          }, 1500);
          return;
        }
        if (result.status === 'expired') {
          setStatus('expired');
          return;
        }
        if (result.status === 'verification_required') {
          setStatus('verification_required');
          setVerifyURL(result.verify_url || '');
          setErrorMsg(result.message || 'JD 风控验证');
          return;
        }
        if (result.status === 'error') {
          setStatus('error');
          setErrorMsg(result.message || '登录失败');
          return;
        }
        if (result.status === 'scanned') {
          setStatus('scanned');
        }
      } catch {
        // Continue polling on network error
      }
      pollTimerRef.current = setTimeout(poll, 3000);
    };

    pollTimerRef.current = setTimeout(poll, 2000);
    return () => { if (pollTimerRef.current) clearTimeout(pollTimerRef.current); };
  }, [open, pollTrigger]);

  useEffect(() => {
    if (!open || status === 'confirmed' || status === 'expired' || status === 'loading' || status === 'verification_required') return;
    const timer = setInterval(() => {
      setCountdown((prev) => {
        if (prev <= 1) {
          setStatus('expired');
          return 0;
        }
        return prev - 1;
      });
    }, 1000);
    return () => clearInterval(timer);
  }, [open, status]);

  const handleAutoLogin = () => {
    onCloseRef.current();
    onAutoLoginRef.current();
  };

  const autoLoginHint = (
    <Button
      type="link"
      icon={<LoginOutlined />}
      onClick={handleAutoLogin}
      style={{ padding: 0, height: 'auto' }}
    >
      推荐使用「一键登录」从本机 JoyCode 自动导入
    </Button>
  );

  const statusDisplay = () => {
    switch (status) {
      case 'loading':
        return <div style={{ textAlign: 'center', padding: 40 }}><Spin size="large" /><div style={{ marginTop: 12, color: '#666' }}>正在生成二维码...</div></div>;
      case 'waiting':
        return <Alert type="info" message="请使用京东 APP 扫描上方二维码" description={<>{`二维码有效期剩余 ${Math.floor(countdown / 60)}:${String(countdown % 60).padStart(2, '0')}`}<br />{autoLoginHint}</>} showIcon />;
      case 'scanned':
        return <Alert type="success" message="已扫描，请在手机上确认登录..." showIcon />;
      case 'confirmed':
        return <Alert type="success" message="登录成功！账号已添加" showIcon icon={<CheckCircleOutlined />} />;
      case 'expired':
        return <Space direction="vertical" align="center" style={{ width: '100%' }}>
          <Alert type="warning" message="二维码已过期" showIcon icon={<CloseCircleOutlined />} />
          <Button icon={<ReloadOutlined />} onClick={initQR}>刷新二维码</Button>
        </Space>;
      case 'verification_required':
        return <Space direction="vertical" align="center" style={{ width: '100%' }}>
          <Alert
            type="warning"
            message="京东安全验证"
            description="京东检测到登录风险，需要完成安全验证。"
            showIcon
            icon={<SafetyCertificateOutlined />}
          />
          {verifyURL && (
            <Button type="primary" icon={<SafetyCertificateOutlined />} href={verifyURL} target="_blank">
              打开安全验证页面
            </Button>
          )}
          <Button icon={<ReloadOutlined />} onClick={initQR}>重新扫码</Button>
        </Space>;
      case 'error':
        return <Space direction="vertical" align="center" style={{ width: '100%' }}>
          <Alert
            type="error"
            message={errorMsg || "登录失败"}
            description={errorMsg?.includes('pt_key') ? '京东扫码登录接口已变更，请使用一键登录自动导入凭据。' : undefined}
            showIcon
          />
          <Button type="primary" icon={<LoginOutlined />} onClick={handleAutoLogin}>
            一键登录（推荐）
          </Button>
          <Button icon={<ReloadOutlined />} onClick={initQR}>重试扫码</Button>
        </Space>;
    }
  };

  return (
    <Modal
      title="扫码登录"
      open={open}
      onCancel={onClose}
      footer={null}
      width={400}
      centered
    >
      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 16 }}>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          使用京东 APP 扫描二维码登录。如遇问题，推荐使用「一键登录」自动导入。
        </Typography.Text>
        {qrImage && status !== 'confirmed' && (
          <div style={{
            padding: 12, background: '#fff', borderRadius: 8,
            border: '1px solid #f0f0f0', boxShadow: '0 2px 8px rgba(0,0,0,0.06)',
          }}>
            <img src={qrImage} alt="QR Code" style={{ width: 200, height: 200 }} />
          </div>
        )}
        {statusDisplay()}
      </div>
    </Modal>
  );
};

export default QRLoginModal;
