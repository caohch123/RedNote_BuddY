import { lazy, Suspense, useCallback, useEffect, useState } from 'react';
import { Alert, Button, Form, Input, Space, Spin, Tag, Typography } from 'antd';
import { ArrowRightOutlined, LockOutlined, LogoutOutlined, SafetyCertificateOutlined, UserOutlined } from '@ant-design/icons';
import { api, ApiError, AUTH_EXPIRED_EVENT, authRealm, errorText, markSessionAuthenticated, resetCSRF, setCSRF } from './api';
import { AuthContext, type Account, type Realm } from './auth-context';
export { useAuth, type Account } from './auth-context';
import './styles/auth.css';

const WorkspaceApp = lazy(() => import('./App'));
const AdminApp = lazy(() => import('./pages/Admin'));

type AuthResult = { user: Account; csrf_token?: string };

export function passwordProblem(value: string): string | undefined {
  if ([...value].length < 12) return '请使用至少 12 个字符的新密码';
  if (new TextEncoder().encode(value).length > 72) return '密码最多 72 个 UTF-8 字节，请减少字符数量';
  return undefined;
}

export function PasswordChangeForm({ onSubmit, required = false }: {
  onSubmit: (currentPassword: string, newPassword: string) => Promise<void>;
  required?: boolean;
}) {
  const [form] = Form.useForm();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);
  async function submit(values: { current: string; password: string }) {
    setBusy(true); setError(''); setSaved(false);
    try { await onSubmit(values.current, values.password); form.resetFields(); setSaved(true); }
    catch (reason) { setError(errorText(reason)); }
    finally { setBusy(false); }
  }
  return <Form className="password-change-form" form={form} layout="vertical" onFinish={values => void submit(values)} requiredMark={false}>
    {error && <Alert type="error" showIcon title={error} role="alert" />}
    {saved && <Alert type="success" showIcon title="密码已更新，当前会话已刷新" role="status" />}
    <Form.Item label="当前密码" name="current" rules={[{ required: true, message: '请输入当前密码' }]}>
      <Input.Password autoComplete="current-password" placeholder="输入当前密码" disabled={busy} />
    </Form.Item>
    <Form.Item label="新密码" name="password" dependencies={['current']} rules={[
      { required: true, message: '请输入新密码' },
      { validator: (_, value: string | undefined) => {
        if (!value) return Promise.resolve();
        const problem = passwordProblem(value);
        if (problem) return Promise.reject(new Error(problem));
        if (value === form.getFieldValue('current')) return Promise.reject(new Error('新密码需要与当前密码不同'));
        return Promise.resolve();
      } },
    ]} extra="至少 12 个字符，最多 72 个 UTF-8 字节。">
      <Input.Password autoComplete="new-password" placeholder="设置新密码" disabled={busy} />
    </Form.Item>
    <Form.Item label="确认新密码" name="confirm" dependencies={['password']} rules={[
      { required: true, message: '请再次输入新密码' },
      ({ getFieldValue }) => ({ validator: (_, value) => !value || value === getFieldValue('password')
        ? Promise.resolve() : Promise.reject(new Error('两次输入的密码不一致')) }),
    ]}>
      <Input.Password autoComplete="new-password" placeholder="再次输入新密码" disabled={busy} />
    </Form.Item>
    <Button type="primary" htmlType="submit" aria-label={required ? '保存密码并进入' : '更新密码'} icon={<LockOutlined />} loading={busy}>{required ? '保存密码并进入' : '更新密码'}</Button>
  </Form>;
}

function Login({ realm, notice, onSuccess }: { realm: Realm; notice: string; onSuccess: (result: AuthResult) => void }) {
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const admin = realm === 'admin';
  const prefix = admin ? '/admin/auth' : '/auth';
  async function submit(values: { username: string; password: string }) {
    setBusy(true); setError('');
    try { onSuccess(await api<AuthResult>(`${prefix}/login`, 'POST', values)); }
    catch (reason) { setError(errorText(reason)); }
    finally { setBusy(false); }
  }
  return <main className={`portal-login ${admin ? 'portal-login-admin' : ''}`}>
    <section className="portal-intro" aria-label="RedNote 工作台">
      <a href="/" className="portal-brand"><img src="/workbench-logo.png" alt="" /><span>RedNote<span className="portal-brand-sub">内容工作台</span></span></a>
      <div className="portal-intro-copy"><div className="portal-eyebrow">{admin ? 'WORKSPACE ADMINISTRATION' : 'YOUR CREATIVE WORKSPACE'}</div>
        <h1>{admin ? <>管理有序，<br />创作更从容。</> : <>让灵感，<br />成为好内容。</>}</h1>
        <p>{admin ? '统一管理成员、模型服务与用量，掌握工作台的运行状态。' : '采集、创作、整理与发布，在你的专属工作区继续。'}</p>
      </div>
      <div className="portal-intro-footer"><SafetyCertificateOutlined /> 独立账户 · 安全会话</div>
    </section>
    <section className="portal-login-panel">
      <div className="portal-login-card"><Tag className="portal-realm-tag" icon={admin ? <SafetyCertificateOutlined /> : <UserOutlined />}>{admin ? '管理员端' : '用户端'}</Tag>
        <Typography.Title level={2}>{admin ? '登录管理控制台' : '登录你的工作台'}</Typography.Title>
        <Typography.Paragraph type="secondary">使用分配给你的{admin ? '管理员' : '用户'}账户登录。</Typography.Paragraph>
        {notice && <Alert showIcon type="info" title={notice} role="status" />}
        {error && <Alert showIcon type="error" title={error} role="alert" />}
        <Form layout="vertical" onFinish={values => void submit(values)} requiredMark={false}>
          <Form.Item label="用户名" name="username" rules={[{ required: true, whitespace: true, message: '请输入用户名' }]}>
            <Input prefix={<UserOutlined />} size="large" autoComplete="username" autoFocus placeholder="输入用户名" disabled={busy} />
          </Form.Item>
          <Form.Item label="密码" name="password" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password prefix={<LockOutlined />} size="large" autoComplete="current-password" placeholder="输入密码" disabled={busy} />
          </Form.Item>
          <Button type="primary" size="large" block htmlType="submit" aria-label="登录" loading={busy} icon={<ArrowRightOutlined />} iconPlacement="end">登录</Button>
        </Form>
        <div className="portal-entry-switch">{admin ? '前往创作工作台' : '需要管理工作台？'}<a href={admin ? '/' : '/admin'}>{admin ? '用户端登录' : '管理员端登录'} <ArrowRightOutlined /></a></div>
      </div>
      <div className="portal-login-footnote">RedNote · 小红书内容工作台</div>
    </section>
  </main>;
}

export function PortalApp() {
  const realm = authRealm();
  const prefix = realm === 'admin' ? '/admin/auth' : '/auth';
  const [user, setUser] = useState<Account>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [revision, setRevision] = useState(0);
  const accept = useCallback((result: AuthResult) => {
    const validRole = realm === 'admin' ? result.user.role === 'admin' : result.user.role === 'member';
    if (!validRole) { markSessionAuthenticated(false); resetCSRF(); throw new Error(`请使用${realm === 'admin' ? '管理员' : '普通用户'}账户登录此入口`); }
    setCSRF(result.csrf_token); markSessionAuthenticated(true); setUser(result.user); setNotice(''); setError('');
  }, [realm]);
  useEffect(() => {
    let cancelled = false;
    setLoading(true); setError(''); markSessionAuthenticated(false);
    void api<AuthResult>(`${prefix}/me`).then(result => { if (!cancelled) accept(result); })
      .catch(reason => { if (!cancelled && (!(reason instanceof ApiError) || reason.status !== 401)) setError(errorText(reason)); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [prefix, accept, revision]);
  useEffect(() => {
    function expired() { setUser(undefined); setNotice('会话已失效，请重新登录'); }
    window.addEventListener(AUTH_EXPIRED_EVENT, expired);
    return () => window.removeEventListener(AUTH_EXPIRED_EVENT, expired);
  }, []);
  const logout = useCallback(async () => {
    await api(`${prefix}/logout`, 'POST', {});
    markSessionAuthenticated(false); resetCSRF(); setUser(undefined); setNotice('已安全退出登录');
  }, [prefix]);
  const changePassword = useCallback(async (currentPassword: string, newPassword: string) => {
    accept(await api<AuthResult>(`${prefix}/password`, 'POST', { current_password: currentPassword, new_password: newPassword }));
  }, [prefix, accept]);
  if (loading) return <div className="portal-loading" role="status"><Spin size="large" /><span>正在加载账户…</span></div>;
  if (!user && error) return <div className="portal-loading"><Alert type="error" showIcon title="暂时无法加载登录服务" description={error} /><Button onClick={() => setRevision(value => value + 1)}>重新连接</Button></div>;
  if (!user) return <Login realm={realm} notice={notice} onSuccess={accept} />;
  return <AuthContext.Provider value={{ user, realm, logout, changePassword }}>
    {user.must_change_password ? <main className="portal-password-page"><div className="portal-password-card">
      <Tag icon={<SafetyCertificateOutlined />}>{realm === 'admin' ? '管理员端' : '用户端'}</Tag>
      <Typography.Title level={2}>设置你的新密码</Typography.Title>
      <Typography.Paragraph type="secondary">你好，{user.display_name || user.username}。首次登录需要先更换初始密码，再进入工作台。</Typography.Paragraph>
      <PasswordChangeForm onSubmit={changePassword} required />
      <Space className="portal-password-exit"><Button type="text" icon={<LogoutOutlined />} onClick={() => void logout().catch(reason => setError(errorText(reason)))}>退出此账户</Button></Space>
      {error && <Alert type="error" title={error} showIcon />}
    </div></main> : <Suspense fallback={<div className="portal-loading" role="status"><Spin size="large" /><span>正在打开工作台…</span></div>}>
      {realm === 'admin' ? <AdminApp /> : <WorkspaceApp />}
    </Suspense>}
  </AuthContext.Provider>;
}
