import React, { useEffect, useState } from 'react';
import { Suspense, lazy } from 'react';
import { BrowserRouter, Routes, Route, Navigate, useNavigate, useSearchParams } from 'react-router-dom';
import { ConfigProvider, Spin, message } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import MainLayout from './layouts/MainLayout';
import Login from './pages/Login';
import Setup from './pages/Setup';
import ForgotPassword from './pages/ForgotPassword';
import OAuthError from './pages/OAuthError';
import { authApi, isAuthenticated, setToken } from './api';

const Dashboard = lazy(() => import('./pages/Dashboard'));
const Accounts = lazy(() => import('./pages/Accounts'));
const AccountDetail = lazy(() => import('./pages/AccountDetail'));
const Settings = lazy(() => import('./pages/Settings'));

const pageLoading = <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;

const OAuthCallback: React.FC = () => {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();

  useEffect(() => {
    const loginSuccess = searchParams.get('login_success');
    const loginError = searchParams.get('login_error');

    if (loginSuccess) {
      const jwtCookie = document.cookie.split('; ').find(c => c.startsWith('joycode_auto_jwt='));
      if (jwtCookie) {
        const token = jwtCookie.split('=')[1];
        if (token) setToken(token);
        document.cookie = 'joycode_auto_jwt=; path=/; max-age=0';
      }
      message.success(`登录成功！账号「${loginSuccess}」已添加`);
      navigate('/accounts', { replace: true });
    } else if (loginError) {
      navigate(`/oauth-error?error=${encodeURIComponent(loginError)}`, { replace: true });
    } else {
      navigate('/dashboard', { replace: true });
    }
  }, [searchParams, navigate]);

  return pageLoading;
};

const AuthGuard: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [checking, setChecking] = useState(true);
  const [initialized, setInitialized] = useState(true);
  const [authed, setAuthed] = useState(false);

  useEffect(() => {
    authApi.status().then((res) => {
      setInitialized(res.initialized);
      if (res.initialized) {
        setAuthed(isAuthenticated());
      }
      setChecking(false);
    }).catch(() => {
      setChecking(false);
    });
  }, []);

  if (checking) return pageLoading;

  if (!initialized) {
    return <Navigate to="/setup" replace />;
  }

  if (!authed) {
    return <Navigate to="/login" replace />;
  }

  return <>{children}</>;
};

const App: React.FC = () => (
  <ConfigProvider locale={zhCN} theme={{ token: { colorPrimary: '#00b578' } }}>
    <BrowserRouter>
      <Routes>
        <Route path="/setup" element={<Setup />} />
        <Route path="/login" element={<Login />} />
        <Route path="/forgot-password" element={<ForgotPassword />} />
        <Route path="/oauth-error" element={<OAuthError />} />
        <Route element={<AuthGuard><MainLayout /></AuthGuard>}>
          <Route path="/dashboard" element={<Suspense fallback={pageLoading}><Dashboard /></Suspense>} />
          <Route path="/accounts" element={<Suspense fallback={pageLoading}><Accounts /></Suspense>} />
          <Route path="/accounts/:apiKey" element={<Suspense fallback={pageLoading}><AccountDetail /></Suspense>} />
          <Route path="/settings" element={<Suspense fallback={pageLoading}><Settings /></Suspense>} />
        </Route>
        <Route path="/" element={<OAuthCallback />} />
      </Routes>
    </BrowserRouter>
  </ConfigProvider>
);

export default App;
