import { useEffect, useRef, useState } from 'react';
import { Alert, App, Avatar, Button, Modal, Popconfirm, Space, Spin, Tag, Typography } from 'antd';
import { LogoutOutlined, QrcodeOutlined, ReloadOutlined, UserOutlined } from '@ant-design/icons';
import { api, errorText } from '../api';
import { useResource } from '../hooks';
import type { XHSAccount, XHSAccountState, XHSLogin } from '../types';
import { useWorkspace } from '../workspace';

const terminal = new Set<XHSLogin['status']>(['logged_in', 'expired', 'failed']);
const loginLabels: Record<XHSLogin['status'], string> = {
  initializing: '正在准备二维码', waiting_scan: '请使用小红书 App 扫码', scanned: '已扫码，请在手机上确认登录',
  logged_in: '登录成功', expired: '二维码已过期', failed: '登录失败',
};

export function AccountIdentity({ account }: { account: XHSAccount }) {
  return <Space align="center"><Avatar src={account.avatar} icon={<UserOutlined />} size={40} />
    <div><strong>{account.nickname || '小红书账号'}</strong><div className="field-hint">账号 ID：{account.id}{account.red_id ? ` · 小红书号：${account.red_id}` : ''}</div></div></Space>;
}

export default function XHSAccountPanel({ onChange }: { onChange: (account: XHSAccountState | undefined) => void }) {
  const { active: workspaceActive } = useWorkspace();
  const portalClassName = workspaceActive ? '' : 'inactive-workspace-portal';
  const { message } = App.useApp();
  const account = useResource<XHSAccountState>('/xhs/account', 10000);
  const [open, setOpen] = useState(false); const [login, setLogin] = useState<XHSLogin>();
  const [starting, setStarting] = useState(false); const [loginError, setLoginError] = useState('');
  const [loggingOut, setLoggingOut] = useState(false); const [now, setNow] = useState(Date.now());
  const [logoutOpen, setLogoutOpen] = useState(false);
  const active = useRef(false); const generation = useRef(0);
  useEffect(() => { onChange(account.data); }, [account.data, onChange]);
  useEffect(() => () => { active.current = false; generation.current++; }, []);

  function close() { active.current = false; generation.current++; setOpen(false); setStarting(false); }
  async function startLogin() {
    active.current = true; const current = ++generation.current;
    setOpen(true); setStarting(true); setLogin(undefined); setLoginError(''); setNow(Date.now());
    try {
      const result = await api<{ login_id: string }>('/xhs/login', 'POST');
      if (!active.current || current !== generation.current) return;
      if (!result.login_id) throw new Error('登录服务没有返回二维码会话');
      setLogin({ login_id: result.login_id, status: 'initializing' });
    } catch (e) { if (active.current && current === generation.current) setLoginError(errorText(e)); }
    finally { if (current === generation.current) setStarting(false); }
  }
  useEffect(() => {
    if (!open || !login?.login_id || terminal.has(login.status)) return;
    let cancelled = false; let inFlight = false; const current = generation.current;
    async function poll() {
      if (inFlight || cancelled) return; inFlight = true;
      try {
        const value = await api<XHSLogin>(`/xhs/login/${encodeURIComponent(login!.login_id)}`);
        if (cancelled || !active.current || current !== generation.current) return;
        setLogin(value); setLoginError('');
        if (value.status === 'logged_in') { void account.reload(true); message.success('小红书账号已登录'); }
      } catch (e) { if (!cancelled && current === generation.current) setLoginError(errorText(e)); }
      finally { inFlight = false; }
    }
    void poll(); const timer = setInterval(() => void poll(), 2000);
    return () => { cancelled = true; clearInterval(timer); };
  }, [open, login?.login_id, !!login && terminal.has(login.status), account.reload, message]);
  useEffect(() => { if (!open) return; const timer = setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(timer); }, [open]);
  const expires = login?.expires_at ? Date.parse(login.expires_at) : NaN;
  const seconds = Number.isFinite(expires) ? Math.max(0, Math.ceil((expires - now) / 1000)) : undefined;
  const expired = login?.status === 'expired' || (seconds === 0 && login?.status !== 'logged_in' && login?.status !== 'scanned');
  const qr = login?.qr_image && /^data:image\/png;base64,[A-Za-z0-9+/=]+$/.test(login.qr_image) ? login.qr_image : undefined;
  async function logout() {
    setLoggingOut(true);
    try { await api('/xhs/account', 'DELETE'); await account.reload(true); message.success('已清除本机的小红书登录状态'); }
    catch (e) { message.error(errorText(e)); } finally { setLoggingOut(false); }
  }
  return <>
    <div className="xhs-account-panel"><div className="xhs-account-info">
      {account.data?.status === 'logged_in' && account.data.account ? <><AccountIdentity account={account.data.account} /><Tag color="success">已登录</Tag></>
        : <><Avatar icon={<UserOutlined />} size={40} /><div><strong>小红书发布账号</strong><div className="field-hint">{account.error || account.data?.message || (account.data?.status === 'expired' ? '登录已失效，请重新扫码' : '使用本人小红书 App 扫码登录')}</div></div></>}
    </div><Space wrap>
      <Button aria-label="刷新小红书账号状态" icon={<ReloadOutlined />} loading={account.loading} onClick={() => void account.reload()} />
      {account.data?.status === 'logged_in' ? <Popconfirm classNames={{ root: portalClassName }} title="退出本机小红书账号？" description="清除本机保存的登录状态，已发布笔记不受影响。" open={workspaceActive && logoutOpen} onOpenChange={value => { if (workspaceActive) setLogoutOpen(value); }} okText="退出登录" cancelText="取消" onConfirm={() => void logout()}>
        <Button icon={<LogoutOutlined />} loading={loggingOut}>退出登录</Button></Popconfirm>
        : <Button type="primary" icon={<QrcodeOutlined />} disabled={!account.data?.dependency_ready} onClick={() => void startLogin()}>扫码登录小红书</Button>}
    </Space></div>
    <Modal rootClassName={portalClassName} title="扫码登录本人小红书账号" open={workspaceActive && open} onCancel={close} footer={<Space>
      <Button aria-label={login?.status === 'logged_in' ? '完成登录' : '关闭登录'} onClick={close}>{login?.status === 'logged_in' ? '完成' : '关闭'}</Button>
      {login?.status !== 'logged_in' && <Button icon={<ReloadOutlined />} loading={starting} onClick={() => void startLogin()}>重新获取二维码</Button>}
    </Space>} width={480}>
      <Typography.Paragraph type="secondary">打开小红书 App 扫描下方二维码，并在手机上确认。登录凭据仅由本机服务保存。</Typography.Paragraph>
      {loginError && <Alert type="error" showIcon title={loginError} />}
      <div className="xhs-qr-box">
        {starting || login?.status === 'initializing' ? <Spin description="正在准备二维码" />
          : login?.status === 'logged_in' && login.account ? <AccountIdentity account={login.account} />
            : qr ? <img src={qr} alt="小红书扫码登录二维码" className={expired ? 'expired-qr' : ''} />
              : <Typography.Text type="secondary">{login?.message || '二维码暂不可用，请重新获取'}</Typography.Text>}
      </div>
      {login && <Alert type={login.status === 'logged_in' ? 'success' : expired || login.status === 'failed' ? 'warning' : 'info'} showIcon
        title={expired ? '二维码已过期，请重新获取' : loginLabels[login.status]} description={login.message || undefined} />}
      {seconds !== undefined && !expired && login?.status !== 'logged_in' && <div className="field-hint" style={{ marginTop: 12 }}>二维码剩余有效时间：{seconds} 秒</div>}
    </Modal>
  </>;
}
