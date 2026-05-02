export interface Account {
  api_key: string;
  api_token: string;
  user_id: string;
  is_default: boolean;
  default_model: string;
  created_at?: string;
}

export interface ModelInfo {
  id: string;
  name: string;
}

export interface Stats {
  total_requests: number;
  total_input_tokens: number;
  total_output_tokens: number;
  accounts_count: number;
  avg_latency_ms: number;
  error_count: number;
  stream_count: number;
  success_count: number;
  by_model: { model: string; count: number }[];
  by_account: { api_key: string; count: number }[];
  all_time: {
    total_requests: number;
    total_input_tokens: number;
    total_output_tokens: number;
    error_count: number;
  };
  hourly: {
    hour: string;
    count: number;
    input_tokens: number;
    output_tokens: number;
    errors: number;
  }[];
}

export interface Settings {
  [key: string]: string;
}

export interface AccountStats {
  api_key: string;
  total_requests: number;
  total_input_tokens: number;
  total_output_tokens: number;
  success_count: number;
  stream_count: number;
  by_model: { model: string; count: number }[];
  by_endpoint: { endpoint: string; count: number }[];
  avg_latency_ms: number;
  error_count: number;
  all_time?: {
    total_requests: number;
    total_input_tokens: number;
    total_output_tokens: number;
    error_count: number;
  };
  hourly: {
    hour: string;
    count: number;
    input_tokens: number;
    output_tokens: number;
    errors: number;
  }[];
}

export interface RequestLog {
  id: number;
  api_key: string;
  model: string;
  endpoint: string;
  stream: boolean;
  status_code: number;
  latency_ms: number;
  error_message: string;
  input_tokens: number;
  output_tokens: number;
  created_at: string;
}

const TOKEN_KEY = 'joycode_jwt';

function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}

export function isAuthenticated(): boolean {
  return !!getToken();
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = getToken();
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  const resp = await fetch(path, {
    headers,
    ...options,
  });
  if (resp.status === 401) {
    clearToken();
    window.location.href = '/login';
    throw new Error('Unauthorized');
  }
  if (!resp.ok) {
    const err = await resp.json().catch(() => ({ detail: resp.statusText }));
    throw new Error(err.detail || `HTTP ${resp.status}`);
  }
  return resp.json();
}

async function authRequest<T>(path: string, options?: RequestInit): Promise<T> {
  const resp = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  if (!resp.ok) {
    const err = await resp.json().catch(() => ({ detail: resp.statusText }));
    throw new Error(err.detail || `HTTP ${resp.status}`);
  }
  return resp.json();
}

export const authApi = {
  status: () => authRequest<{ initialized: boolean; exe_path?: string }>('/api/auth/status'),
  setup: (password: string) =>
    authRequest<{ ok: boolean; token: string }>('/api/auth/setup', {
      method: 'POST',
      body: JSON.stringify({ password }),
    }),
  login: (password: string) =>
    authRequest<{ ok: boolean; token: string }>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ password }),
    }),
  changePassword: (oldPassword: string, newPassword: string) =>
    request<{ ok: boolean }>('/api/auth/change-password', {
      method: 'POST',
      body: JSON.stringify({ old_password: oldPassword, new_password: newPassword }),
    }),
};

export const api = {
  listAccounts: () => request<{ accounts: Account[] }>('/api/accounts').then(r => r.accounts),
  addAccount: (data: { api_key: string; pt_key: string; user_id: string; is_default?: boolean; default_model?: string }) =>
    request<{ ok: boolean }>('/api/accounts', { method: 'POST', body: JSON.stringify(data) }),
  removeAccount: (apiKey: string) =>
    request<{ ok: boolean }>(`/api/accounts/${encodeURIComponent(apiKey)}`, { method: 'DELETE' }),
  setDefault: (apiKey: string) =>
    request<{ ok: boolean }>(`/api/accounts/${encodeURIComponent(apiKey)}/default`, { method: 'PUT' }),
  validateAccount: (apiKey: string) =>
    request<{ valid: boolean }>(`/api/accounts/${encodeURIComponent(apiKey)}/validate`, { method: 'POST' }),
  listModels: () => request<{ models: ModelInfo[] }>('/api/models').then(r => r.models),
  listAccountModels: (apiKey: string) =>
    request<{ models: ModelInfo[] }>(`/api/accounts/${encodeURIComponent(apiKey)}/models`).then(r => r.models),
  getStats: () => request<Stats>('/api/stats'),
  getSettings: () => request<{ settings: Settings }>('/api/settings').then(r => r.settings),
  updateSettings: (data: Settings) =>
    request<{ ok: boolean }>('/api/settings', { method: 'PUT', body: JSON.stringify(data) }),
  getHealth: () => request<{ status: string; accounts: number }>('/api/health'),
  updateAccountModel: (apiKey: string, defaultModel: string) =>
    request<{ ok: boolean }>(`/api/accounts/${encodeURIComponent(apiKey)}/model`, {
      method: 'PUT',
      body: JSON.stringify({ default_model: defaultModel }),
    }),
  getAccountStats: (apiKey: string) =>
    request<AccountStats>(`/api/accounts/${encodeURIComponent(apiKey)}/stats`),
  getAccountLogs: (apiKey: string, limit = 200) =>
    request<{ logs: RequestLog[]; total: number }>(`/api/accounts/${encodeURIComponent(apiKey)}/logs?limit=${limit}`),
  renewToken: (apiKey: string) =>
    request<{ ok: boolean; api_token: string }>(`/api/accounts/${encodeURIComponent(apiKey)}/renew-token`, { method: 'POST' }),
  autoLogin: () =>
    request<{ ok: boolean; api_key: string; user_id: string; real_name: string; is_default: boolean }>('/api/accounts-auto-login', { method: 'POST' }),
  qrLoginInit: () =>
    request<{ ok: boolean; session_id: string; qr_image: string }>('/api/qr-login/init', { method: 'POST' }),
  qrLoginStatus: (sessionId: string) =>
    request<{ status: string; ok?: boolean; api_key?: string; user_id?: string; real_name?: string; message?: string }>(`/api/qr-login/status?session=${encodeURIComponent(sessionId)}`),
  getRecentErrors: (limit = 50) =>
    request<{ errors: RequestLog[]; total: number }>(`/api/errors?limit=${limit}`),
  getGitHubStars: () =>
    request<{ stars: number }>('/api/github-stars').then(r => r.stars),
};
