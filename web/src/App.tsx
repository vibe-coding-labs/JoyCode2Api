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

const antTheme = {
  token: {
    colorPrimary: '#22C55E',
    colorBgContainer: '#0E1223',
    colorBgElevated: '#0E1223',
    colorBgLayout: '#020617',
    colorBgSpotlight: '#1A1E2F',
    colorText: '#F8FAFC',
    colorTextSecondary: '#94A3B8',
    colorTextTertiary: '#64748B',
    colorTextQuaternary: '#475569',
    colorBorder: '#334155',
    colorBorderSecondary: '#1F2937',
    colorSplit: '#1F2937',
    colorSuccess: '#22C55E',
    colorWarning: '#F59E0B',
    colorError: '#EF4444',
    colorInfo: '#3B82F6',
    colorLink: '#22C55E',
    colorLinkHover: '#16A34A',
    colorLinkActive: '#15803D',
    colorBgBlur: '#0E1223',
    colorFill: '#1A1E2F',
    colorFillSecondary: '#1A1E2F',
    colorFillTertiary: '#0E1223',
    colorFillQuaternary: '#0E1223',
    colorFillContent: '#1A1E2F',
    colorFillContentHover: '#334155',
    colorFillAlter: '#1A1E2F',
    borderRadius: 8,
    borderRadiusLG: 10,
    borderRadiusSM: 6,
    fontFamily: "'Fira Sans', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
    fontFamilyCode: "'Fira Code', 'SF Mono', Menlo, monospace",
    fontSize: 14,
    controlHeight: 36,
    controlHeightLG: 44,
    controlHeightSM: 28,
    wireframe: false,
  },
  components: {
    Layout: {
      headerBg: '#0E1223',
      headerHeight: 56,
      headerPadding: '0 24px',
      bodyBg: '#020617',
      siderBg: '#0E1223',
      triggerBg: '#0E1223',
      triggerColor: '#94A3B8',
    },
    Menu: {
      itemBg: 'transparent',
      itemColor: '#94A3B8',
      itemHoverBg: '#1A1E2F',
      itemHoverColor: '#F8FAFC',
      itemSelectedBg: 'rgba(34, 197, 94, 0.12)',
      itemSelectedColor: '#22C55E',
      itemActiveBg: 'rgba(34, 197, 94, 0.08)',
      activeBarBorderWidth: 0,
      itemBorderRadius: 8,
      itemMarginInline: 8,
      itemMarginBlock: 4,
    },
    Card: {
      headerBg: 'transparent',
      headerFontSize: 14,
      headerHeight: 44,
      paddingLG: 16,
    },
    Table: {
      headerBg: '#1A1E2F',
      headerColor: '#94A3B8',
      headerSplitColor: '#1F2937',
      rowHoverBg: '#1A1E2F',
      rowSelectedBg: 'rgba(34, 197, 94, 0.08)',
      rowSelectedHoverBg: 'rgba(34, 197, 94, 0.12)',
      borderColor: '#1F2937',
      headerSortActiveBg: '#1A1E2F',
      headerSortHoverBg: '#334155',
      cellPaddingBlock: 12,
      cellPaddingInline: 16,
    },
    Statistic: {
      titleFontSize: 11,
      contentFontSize: 20,
    },
    Button: {
      borderRadius: 6,
      borderRadiusLG: 6,
      borderRadiusSM: 4,
      controlHeight: 36,
      controlHeightLG: 44,
      controlHeightSM: 28,
      primaryShadow: '0 2px 6px rgba(34, 197, 94, 0.2)',
      defaultShadow: 'none',
      dashShadow: 'none',
    },
    Input: {
      activeBorderColor: '#22C55E',
      hoverBorderColor: '#22C55E',
      activeShadow: '0 0 0 2px rgba(34, 197, 94, 0.15)',
    },
    InputNumber: {
      activeBorderColor: '#22C55E',
      hoverBorderColor: '#22C55E',
      activeShadow: '0 0 0 2px rgba(34, 197, 94, 0.15)',
    },
    Select: {
      optionSelectedBg: 'rgba(34, 197, 94, 0.15)',
      optionSelectedColor: '#22C55E',
      optionSelectedFontWeight: 500,
      optionActiveBg: '#1A1E2F',
    },
    Tag: {
      defaultBg: '#1A1E2F',
      defaultColor: '#94A3B8',
    },
    Modal: {
      contentBg: '#0E1223',
      headerBg: '#0E1223',
      titleColor: '#F8FAFC',
    },
    Tooltip: {
      colorBgSpotlight: '#0E1223',
      colorTextLightSolid: '#F8FAFC',
    },
    Popover: {
      colorBgElevated: '#0E1223',
      colorText: '#F8FAFC',
    },
    Alert: {
      defaultPadding: '12px 16px',
    },
    Segmented: {
      itemSelectedBg: '#0E1223',
      itemSelectedColor: '#F8FAFC',
      trackBg: '#1A1E2F',
      itemColor: '#94A3B8',
    },
    Form: {
      labelColor: '#94A3B8',
      labelFontSize: 13,
    },
    Divider: {
      colorSplit: '#1F2937',
    },
    Spin: {
      colorPrimary: '#22C55E',
    },
    Progress: {
      defaultColor: '#22C55E',
    },
  },
};

const App: React.FC = () => (
  <ConfigProvider locale={zhCN} theme={antTheme}>
    <BrowserRouter>
      <Routes>
        <Route path="/setup" element={<Setup />} />
        <Route path="/login" element={<Login />} />
        <Route path="/forgot-password" element={<ForgotPassword />} />
        <Route path="/oauth-error" element={<OAuthError />} />
        <Route element={<AuthGuard><MainLayout /></AuthGuard>}>
          <Route path="/dashboard" element={<Suspense fallback={pageLoading}><Dashboard /></Suspense>} />
          <Route path="/accounts" element={<Suspense fallback={pageLoading}><Accounts /></Suspense>} />
          <Route path="/accounts/:userId" element={<Suspense fallback={pageLoading}><AccountDetail /></Suspense>} />
          <Route path="/settings" element={<Suspense fallback={pageLoading}><Settings /></Suspense>} />
        </Route>
        <Route path="/" element={<OAuthCallback />} />
      </Routes>
    </BrowserRouter>
  </ConfigProvider>
);

export default App;
