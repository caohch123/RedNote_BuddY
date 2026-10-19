import { useEffect, useState } from 'react';
import { App, Alert, Button, Card, Empty, Form, Input, InputNumber, Modal, Popconfirm, Progress, Select, Space, Spin, Switch, Table, Tag, Tooltip, Typography } from 'antd';
import { ArrowRightOutlined, AuditOutlined, BarChartOutlined, CheckCircleOutlined, CopyOutlined, DashboardOutlined, KeyOutlined, LogoutOutlined, PlusOutlined, ReloadOutlined, RobotOutlined, SafetyCertificateOutlined, SaveOutlined, TeamOutlined, UnorderedListOutlined, WalletOutlined } from '@ant-design/icons';
import { api, errorText } from '../api';
import { PasswordChangeForm, useAuth } from '../auth';
import { useResource } from '../hooks';
import { Failure, Status } from '../components/Common';
import { auditActions, canCancelManagedJob, canRetryManagedJob, jobNames, modelCapabilities, modelSettingsPatch } from '../admin';
import type { AdminJob, AdminOverview, AuditEntry, ManagedAccount, ModelSettings } from '../admin';
import type { Capability, ProviderConfig } from '../types';
import { costNumber, tokenNumber } from '../billing';
import { workbenchPalette } from '../theme';
import Billing from './Billing';
import '../styles/billing.css';
import '../styles/admin.css';

type Section = 'overview' | 'users' | 'models' | 'billing' | 'jobs' | 'audit';
const sections = [
  { key: 'overview', label: '管理总览', icon: <DashboardOutlined />, detail: '今天的工作台运行概况' },
  { key: 'users', label: '用户管理', icon: <TeamOutlined />, detail: '管理账号、访问权限和调用额度' },
  { key: 'models', label: '模型配置', icon: <RobotOutlined />, detail: '统一维护成员使用的 AI 服务' },
  { key: 'billing', label: '用量与计费', icon: <WalletOutlined />, detail: '查看各用户的模型调用与费用估算' },
  { key: 'jobs', label: '后台任务', icon: <UnorderedListOutlined />, detail: '跟进任务状态，处理明确失败的任务' },
  { key: 'audit', label: '操作记录', icon: <AuditOutlined />, detail: '管理操作留痕，变更可追溯' },
] as const;
const dateText = (value: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—';
const noItems = (description: string) => <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={description} />;

function Overview({ navigate }: { navigate: (section: Section) => void }) {
  const result = useResource<AdminOverview>('/admin/overview?timezone=Asia%2FShanghai', 15000); const data = result.data;
  return <div className="admin-content-stack">
    <Failure error={result.error} retry={() => void result.reload()} />
    {!data ? <div className="admin-loading"><Spin /></div> : <>
      <div className="admin-context"><Tag color="blue">今日 · 北京时间</Tag><span>用量和费用以已返回的服务响应为准</span><Button type="text" icon={<ReloadOutlined />} aria-label="刷新总览" onClick={() => void result.reload()} loading={result.loading} /></div>
      <div className="admin-stat-grid">
        {[{ label: '启用账号', value: String(data.enabled_users), caption: `共 ${data.users} 个账号`, icon: <TeamOutlined /> },
          { label: '模型调用', value: tokenNumber(data.requests), caption: data.unreported ? `${data.unreported} 次未返回 Token` : '已记录的调用次数', icon: <BarChartOutlined /> },
          { label: 'Token 用量', value: tokenNumber(data.tokens), caption: '服务商已报告用量', icon: <RobotOutlined /> },
          { label: '估算费用', value: costNumber(data.estimated_cny), caption: '按已配置的 Token 单价', icon: <WalletOutlined /> }].map(item => <div className="admin-stat" key={item.label}><span>{item.icon}{item.label}</span><strong>{item.value}</strong><small>{item.caption}</small></div>)}
      </div>
      <div className="admin-overview-grid">
        <Card title="用户用量" extra={<Button type="link" onClick={() => navigate('billing')}>查看明细 <ArrowRightOutlined /></Button>} className="admin-card">
          {!data.per_user.length ? noItems('今天还没有模型调用') : <div className="admin-user-usage">{data.per_user.map(row => <div className="admin-user-usage-row" key={row.user_id}>
            <div><strong>{row.display_name || row.username}</strong><span>{row.username}</span></div><div><strong>{tokenNumber(row.tokens)} <small>Token</small></strong><span>{row.requests} 次调用 · {costNumber(row.estimated_cny)}</span></div>
            <Progress percent={data.tokens ? Math.round(row.tokens / data.tokens * 100) : 0} showInfo={false} strokeColor={workbenchPalette.accent} size="small" />
          </div>)}</div>}
        </Card>
        <Card title="待办与状态" className="admin-card">
          <button type="button" className="admin-status-link" onClick={() => navigate('jobs')}><span className="admin-status-symbol"><UnorderedListOutlined /></span><span><strong>待处理任务</strong><small>查看任务进度与状态</small></span><b>{data.pending_jobs}</b><ArrowRightOutlined /></button>
          <button type="button" className="admin-status-link" onClick={() => navigate('jobs')}><span className="admin-status-symbol warning"><SafetyCertificateOutlined /></span><span><strong>失败任务</strong><small>检查原因后决定下一步</small></span><b>{data.failed_jobs}</b><ArrowRightOutlined /></button>
          <div className="admin-overview-note"><CheckCircleOutlined /><p>用户的创作与素材分别存放。管理端仅显示账号、用量与任务摘要。</p></div>
        </Card>
      </div>
    </>}
  </div>;
}

function UserManagement() {
  const { user } = useAuth(); const { message, modal } = App.useApp();
  const result = useResource<{ items: ManagedAccount[]; total: number }>('/admin/users', 15000);
  const [search, setSearch] = useState(''); const [creating, setCreating] = useState(false); const [editing, setEditing] = useState<ManagedAccount>();
  const [busy, setBusy] = useState(false); const [pending, setPending] = useState(''); const [credentials, setCredentials] = useState<{ username: string; password: string }>();
  const [createForm] = Form.useForm(); const [editForm] = Form.useForm();
  useEffect(() => { if (editing) editForm.setFieldsValue(editing); }, [editing, editForm]);
  async function create(values: Record<string, unknown>) {
    setBusy(true);
    try { const data = await api<{ user: ManagedAccount; temporary_password: string }>('/admin/users', 'POST', values);
      setCreating(false); createForm.resetFields(); setCredentials({ username: data.user.username, password: data.temporary_password }); await result.reload(true);
    } catch (e) { message.error(errorText(e)); } finally { setBusy(false); }
  }
  async function update(values: Record<string, unknown>) {
    if (!editing) return; setBusy(true);
    try { await api(`/admin/users/${encodeURIComponent(editing.id)}`, 'PATCH', values); setEditing(undefined); message.success('账号设置已更新'); await result.reload(true); }
    catch (e) { message.error(errorText(e)); } finally { setBusy(false); }
  }
  async function toggle(account: ManagedAccount, enabled: boolean) {
    setPending(account.id);
    try { await api(`/admin/users/${encodeURIComponent(account.id)}`, 'PATCH', { enabled }); message.success(enabled ? '账号已启用' : '账号已停用，现有会话已失效'); await result.reload(true); }
    catch (e) { message.error(errorText(e)); } finally { setPending(''); }
  }
  function reset(account: ManagedAccount) {
    modal.confirm({ title: `重置 ${account.username} 的密码？`, content: '现有登录会话将失效。新临时密码只在下一步显示，用户首次登录需要修改。', okText: '重置密码', cancelText: '取消',
      onOk: async () => { try { const data = await api<{ temporary_password: string }>(`/admin/users/${encodeURIComponent(account.id)}/reset-password`, 'POST');
        setCredentials({ username: account.username, password: data.temporary_password }); await result.reload(true);
      } catch (e) { message.error(errorText(e)); throw e; } } });
  }
  async function copyCredentials() {
    if (!credentials) return;
    try { await navigator.clipboard.writeText(`账号：${credentials.username}\n临时密码：${credentials.password}`); message.success('已复制账号与临时密码'); }
    catch { message.warning('复制未成功，请手动选择并复制临时密码'); }
  }
  const items = (result.data?.items || []).filter(u => `${u.username} ${u.display_name}`.toLowerCase().includes(search.trim().toLowerCase()));
  const quotaFields = <><Form.Item name="daily_request_limit" label="每日模型调用上限" extra="0 表示不设上限，按北京时间每日重置" rules={[{ required: true }]}><InputNumber min={0} max={1000000} precision={0} /></Form.Item><Form.Item name="max_concurrent" label="同时进行的模型请求数" rules={[{ required: true }]}><InputNumber min={1} max={8} precision={0} /></Form.Item></>;
  return <div className="admin-content-stack"><Failure error={result.error} retry={() => void result.reload()} />
    <Card className="admin-card" title="工作台账号" extra={<Space><Tooltip title="刷新账号"><Button icon={<ReloadOutlined />} aria-label="刷新账号" onClick={() => void result.reload()} loading={result.loading} /></Tooltip><Button type="primary" aria-label="创建账号" icon={<PlusOutlined />} onClick={() => setCreating(true)}>创建账号</Button></Space>}>
      <div className="admin-table-toolbar"><Input.Search aria-label="搜索账号" placeholder="搜索账号或姓名" allowClear value={search} onChange={e => setSearch(e.target.value)} /><span>{result.data?.total || 0} 个账号</span></div>
      <Table<ManagedAccount> rowKey="id" size="middle" dataSource={items} loading={result.loading} scroll={{ x: 930 }} pagination={{ pageSize: 10, showSizeChanger: false, hideOnSinglePage: true }} locale={{ emptyText: noItems('暂无匹配账号') }} columns={[
        { title: '用户', key: 'name', width: 190, render: (_, account) => <div className="admin-name-cell"><strong>{account.display_name || account.username}{account.id === user.id && <Tag>我</Tag>}</strong><span>{account.username}</span></div> },
        { title: '角色', dataIndex: 'role', width: 105, render: value => <Tag color={value === 'admin' ? 'purple' : 'blue'}>{value === 'admin' ? '管理员' : '用户'}</Tag> },
        { title: '今日调用 / 上限', key: 'quota', width: 155, render: (_, account) => <span>{account.daily_requests || 0} / {account.daily_request_limit || '不限'}</span> },
        { title: '并发请求', dataIndex: 'max_concurrent', width: 95 },
        { title: '登录状态', key: 'login', width: 130, render: (_, account) => account.must_change_password ? <Tag color="gold">首次需改密</Tag> : <span className="admin-muted">正常</span> },
        { title: '启用', key: 'enabled', width: 90, render: (_, account) => <Tooltip title={account.id === user.id ? '当前管理员不可停用自身' : undefined}><Switch aria-label={`${account.username} 启用状态`} checked={account.enabled} loading={pending === account.id} disabled={account.id === user.id} onChange={value => { if (value) void toggle(account, true); else modal.confirm({ title: `停用 ${account.username}？`, content: '该账号将无法登录，已有会话也会失效。', okText: '停用', okButtonProps: { danger: true }, cancelText: '取消', onOk: () => toggle(account, false) }); }} /></Tooltip> },
        { title: '操作', key: 'actions', width: 165, render: (_, account) => <Space size={4}><Button size="small" type="text" onClick={() => setEditing(account)}>编辑</Button><Button size="small" type="text" icon={<KeyOutlined />} disabled={account.id === user.id} onClick={() => reset(account)}>重置密码</Button></Space> },
      ]} />
    </Card>
    <Modal title="创建工作台账号" open={creating} onCancel={() => { setCreating(false); createForm.resetFields(); }} footer={null} destroyOnHidden><Form form={createForm} layout="vertical" initialValues={{ role: 'member', daily_request_limit: 0, max_concurrent: 2 }} onFinish={values => void create(values)}>
      <Form.Item name="username" label="登录账号" rules={[{ required: true, message: '请输入登录账号' }, { pattern: /^[a-zA-Z0-9][a-zA-Z0-9_.-]{2,31}$/, message: '使用 3–32 位字母、数字、点、下划线或短横线' }]}><Input autoComplete="off" placeholder="例如 creator01" /></Form.Item>
      <Form.Item name="display_name" label="显示名称" rules={[{ max: 80 }]}><Input placeholder="例如 内容创作员" /></Form.Item>
      <Form.Item name="role" label="账号角色"><Select options={[{ value: 'member', label: '用户 · 创作工作台' }, { value: 'admin', label: '管理员 · 管理控制台' }]} /></Form.Item>
      {quotaFields}<div className="admin-form-actions"><Button onClick={() => setCreating(false)}>取消</Button><Button type="primary" htmlType="submit" loading={busy}>创建账号</Button></div>
    </Form></Modal>
    <Modal title={`编辑账号 · ${editing?.username || ''}`} open={!!editing} onCancel={() => setEditing(undefined)} footer={null} destroyOnHidden><Form form={editForm} layout="vertical" onFinish={values => void update(values)}><Form.Item name="display_name" label="显示名称" rules={[{ max: 80 }]}><Input /></Form.Item>{quotaFields}<div className="admin-form-actions"><Button onClick={() => setEditing(undefined)}>取消</Button><Button type="primary" htmlType="submit" loading={busy}>保存设置</Button></div></Form></Modal>
    <Modal title="账号已准备就绪" open={!!credentials} onCancel={() => setCredentials(undefined)} footer={<Button type="primary" onClick={() => setCredentials(undefined)}>已妥善保存</Button>}>
      <Alert showIcon type="info" title="临时密码只在此处显示一次" description="请通过可信方式交给使用者，首次登录必须修改密码。" />
      <div className="admin-credentials"><span>登录账号</span><Typography.Text copyable>{credentials?.username}</Typography.Text><span>临时密码</span><Typography.Text code copyable>{credentials?.password}</Typography.Text></div>
      <Button icon={<CopyOutlined />} onClick={() => void copyCredentials()}>复制账号与密码</Button>
    </Modal>
  </div>;
}

function Models() {
  const { message, modal } = App.useApp(); const result = useResource<ModelSettings>('/admin/models');
  const [config, setConfig] = useState<ModelSettings>(); const [keys, setKeys] = useState<Partial<Record<Capability, string>>>({}); const [busy, setBusy] = useState(false);
  useEffect(() => { if (result.data) setConfig(result.data); }, [result.data]);
  const patch = config && result.data ? modelSettingsPatch(config, result.data, keys) : {};
  const dirty = Object.keys(patch).length > 0;
  function provider(key: Capability, changes: Partial<ProviderConfig>) { setConfig(current => current && ({ ...current, providers: { ...current.providers, [key]: { ...current.providers[key], ...changes } } })); }
  async function save() {
    if (!config || !dirty) return; setBusy(true);
    try { await api('/admin/models', 'PATCH', patch); setKeys({}); await result.reload(true); message.success('模型与运行配置已更新'); }
    catch (e) { message.error(errorText(e)); } finally { setBusy(false); }
  }
  function clearKey(key: Capability) { modal.confirm({ title: '清除已保存的 API Key？', content: '保存设置后生效。使用此模型能力的用户将需要新的密钥配置。', okText: '标记清除', okButtonProps: { danger: true }, cancelText: '取消', onOk: () => setKeys(current => ({ ...current, [key]: '__CLEAR__' })) }); }
  return <div className="admin-content-stack"><Failure error={result.error} retry={() => void result.reload()} />
    <div className="admin-section-toolbar"><p>统一配置成员使用的模型。空白密钥输入保留原配置。</p><Button type="primary" aria-label="保存配置" icon={<SaveOutlined />} loading={busy} disabled={!dirty} onClick={() => void save()}>保存配置</Button></div>
    {!config ? <div className="admin-loading"><Spin /></div> : <><div className="admin-model-grid">{modelCapabilities.map(({ key, label, detail }) => {
      const item = config.providers[key] || { base_url: '', model: '', configured: false };
      return <Card key={key} title={label} extra={<Tag color={item.configured && keys[key] !== '__CLEAR__' ? 'green' : 'default'}>{keys[key] === '__CLEAR__' ? '待清除' : item.configured ? '已配置' : '未配置'}</Tag>} className="admin-card admin-model-card">
        <p className="admin-muted">{detail}</p><label>API 地址<Input value={item.base_url} aria-label={`${label} API 地址`} placeholder="https://服务地址/…" onChange={e => provider(key, { base_url: e.target.value })} /></label>
        <label>模型名称<Input value={item.model} aria-label={`${label} 模型名称`} onChange={e => provider(key, { model: e.target.value })} /></label>
        <label>API Key<Input.Password value={keys[key] === '__CLEAR__' ? '' : keys[key] || ''} autoComplete="new-password" aria-label={`${label} API Key`} placeholder={keys[key] === '__CLEAR__' ? '保存后清除原密钥' : item.configured ? `${item.key_mask || '已保存'} · 留空保留` : '填写服务密钥'} onChange={e => setKeys(current => ({ ...current, [key]: e.target.value }))} /></label>
        <div className="admin-key-actions">{keys[key] === '__CLEAR__' ? <Button type="text" size="small" onClick={() => setKeys(current => ({ ...current, [key]: '' }))}>撤销清除</Button> : <Button type="text" size="small" danger disabled={!item.configured} onClick={() => clearKey(key)}>清除密钥</Button>}</div>
        <label>工作空间 ID <span className="admin-muted">（可选）</span><Input value={item.workspace_id || ''} aria-label={`${label} 工作空间 ID`} onChange={e => provider(key, { workspace_id: e.target.value })} /></label>
      </Card>;
    })}</div>
    <Card title="运行环境" className="admin-card"><div className="admin-runtime-grid">{([{ key: 'collector_url', label: '采集服务地址', placeholder: 'http://127.0.0.1:5556' }, { key: 'ffmpeg_path', label: 'FFmpeg 路径', placeholder: '留空自动检测' }, { key: 'ffprobe_path', label: 'FFprobe 路径', placeholder: '留空自动检测' }, { key: 'node_path', label: 'Node.js 路径', placeholder: '留空自动检测' }] as const).map(field => <label key={field.key}>{field.label}<Input value={config[field.key]} placeholder={field.placeholder} aria-label={field.label} onChange={e => setConfig(current => current && ({ ...current, [field.key]: e.target.value }))} /></label>)}</div></Card></>}
  </div>;
}

function Jobs() {
  const { message } = App.useApp(); const [userID, setUserID] = useState(''); const [state, setState] = useState(''); const [pending, setPending] = useState('');
  const users = useResource<{ items: ManagedAccount[] }>('/admin/users'); const query = new URLSearchParams(); if (userID) query.set('user_id', userID); if (state) query.set('state', state);
  const result = useResource<{ items: AdminJob[]; total: number }>(`/admin/jobs?${query.toString()}`, 5000);
  async function action(job: AdminJob, verb: 'retry' | 'cancel') { setPending(job.id); try { await api(`/admin/users/${encodeURIComponent(job.user_id)}/jobs/${encodeURIComponent(job.id)}/${verb}`, 'POST'); message.success(verb === 'retry' ? '已提交重试' : '已取消排队任务'); await result.reload(true); } catch (e) { message.error(errorText(e)); } finally { setPending(''); } }
  return <div className="admin-content-stack"><Failure error={result.error || users.error} retry={() => { void result.reload(); void users.reload(); }} />
    <Card title="后台任务" className="admin-card" extra={<Button aria-label="刷新任务" icon={<ReloadOutlined />} loading={result.loading} onClick={() => void result.reload()} />}>
      <div className="admin-table-toolbar"><Space wrap><Select allowClear showSearch optionFilterProp="label" aria-label="按用户筛选任务" placeholder="全部用户" value={userID || undefined} onChange={value => setUserID(value || '')} options={(users.data?.items || []).map(u => ({ value: u.id, label: `${u.display_name || u.username} · ${u.username}` }))} /><Select allowClear aria-label="筛选任务状态" placeholder="全部状态" value={state || undefined} onChange={value => setState(value || '')} options={[{ value: 'queued', label: '排队中' }, { value: 'running', label: '进行中' }, { value: 'failed', label: '失败' }, { value: 'waiting_config', label: '待配置' }, { value: 'submission_unknown', label: '结果待核验' }, { value: 'succeeded', label: '已完成' }, { value: 'cancelled', label: '已取消' }]} /></Space><span>{result.data?.total || 0} 个任务</span></div>
      <Table<AdminJob> rowKey={row => `${row.user_id}:${row.id}`} size="middle" dataSource={result.data?.items || []} loading={result.loading} scroll={{ x: 950 }} pagination={{ pageSize: 15, showSizeChanger: false, hideOnSinglePage: true }} locale={{ emptyText: noItems('暂无匹配任务') }} columns={[
        { title: '任务', key: 'kind', width: 190, render: (_, job) => <div className="admin-name-cell"><strong>{jobNames[job.kind] || job.kind}</strong><span>{job.username}</span></div> },
        { title: '状态', dataIndex: 'state', width: 125, render: value => <Status value={value} /> },
        { title: '进度', key: 'progress', width: 135, render: (_, job) => <Progress percent={Math.min(100, Math.max(0, job.progress || 0))} status={job.state === 'failed' ? 'exception' : undefined} size="small" /> },
        { title: '阶段 / 摘要', key: 'message', render: (_, job) => <div className="admin-task-summary"><span>{job.phase}</span><span className={job.error ? 'admin-error' : 'admin-muted'}>{job.error || job.message || '—'}</span></div> },
        { title: '更新时间', dataIndex: 'updated_at', width: 166, render: dateText },
        { title: '操作', key: 'action', width: 110, render: (_, job) => canRetryManagedJob(job) ? <Popconfirm title="重试此任务？" description="该任务将重新调用服务，可能产生费用。" okText="重试" cancelText="取消" onConfirm={() => action(job, 'retry')}><Button size="small" aria-label="重试" loading={pending === job.id}>重试</Button></Popconfirm> : canCancelManagedJob(job) ? <Popconfirm title="取消此排队任务？" okText="取消任务" cancelText="返回" onConfirm={() => action(job, 'cancel')}><Button size="small" aria-label="取消" loading={pending === job.id}>取消</Button></Popconfirm> : <span className="admin-muted">—</span> },
      ]} expandable={{ expandedRowRender: job => <div className="admin-task-details"><span>任务编号：{job.id}</span><span>创建：{dateText(job.created_at)}</span><span>尝试：{job.attempts} 次</span></div> }} />
    </Card>
  </div>;
}

function Audit() {
  const result = useResource<{ items: AuditEntry[]; total: number }>('/admin/audit', 15000); const [search, setSearch] = useState('');
  const rows = (result.data?.items || []).filter(row => `${row.actor_name} ${row.action} ${auditActions[row.action] || ''} ${row.target}`.toLowerCase().includes(search.toLowerCase()));
  return <div className="admin-content-stack"><Failure error={result.error} retry={() => void result.reload()} /><Card title="操作记录" className="admin-card" extra={<Button icon={<ReloadOutlined />} aria-label="刷新操作记录" loading={result.loading} onClick={() => void result.reload()} />}>
    <div className="admin-table-toolbar"><Input.Search aria-label="搜索操作记录" placeholder="搜索操作者、操作或对象" allowClear value={search} onChange={e => setSearch(e.target.value)} /><span>最近 {result.data?.total || 0} 条</span></div>
    <Table<AuditEntry> rowKey="id" dataSource={rows} size="middle" loading={result.loading} scroll={{ x: 800 }} pagination={{ pageSize: 15, showSizeChanger: false, hideOnSinglePage: true }} locale={{ emptyText: noItems('暂无操作记录') }} columns={[
      { title: '时间', dataIndex: 'created_at', width: 180, render: dateText }, { title: '操作者', dataIndex: 'actor_name', width: 150 },
      { title: '操作', dataIndex: 'action', width: 180, render: value => auditActions[value] || value }, { title: '对象', dataIndex: 'target', render: value => <span className="admin-audit-target">{value || '—'}</span> },
      { title: '结果', dataIndex: 'result', width: 105, render: value => <Tag color={['success', 'ok', 'succeeded'].includes(value) ? 'green' : 'red'}>{['success', 'ok', 'succeeded'].includes(value) ? '成功' : value === 'failed' ? '失败' : ['rejected', 'denied'].includes(value) ? '已拒绝' : value}</Tag> },
    ]} />
  </Card></div>;
}

function readSection(): Section { const key = location.hash.replace('#', ''); return sections.some(item => item.key === key) ? key as Section : 'overview'; }
export default function Admin() {
  const { user, logout, changePassword } = useAuth(); const { message } = App.useApp(); const [passwordOpen, setPasswordOpen] = useState(false); const [section, setSection] = useState<Section>(readSection); const [leaving, setLeaving] = useState(false);
  useEffect(() => { const changed = () => setSection(readSection()); window.addEventListener('hashchange', changed); return () => window.removeEventListener('hashchange', changed); }, []);
  const current = sections.find(item => item.key === section)!;
  function navigate(key: Section) { location.hash = key; setSection(key); }
  async function leave() { setLeaving(true); try { await logout(); } catch (e) { message.error(errorText(e)); } finally { setLeaving(false); } }
  return <div className="admin-shell">
    <aside className="admin-sidebar"><a className="admin-brand" href="/admin"><span><SafetyCertificateOutlined /></span><div><strong>RedNote BuddY</strong><small>管理控制台</small></div></a>
      <nav className="admin-navigation" aria-label="管理导航">{sections.map(item => <button type="button" key={item.key} aria-label={item.label} className={item.key === section ? 'active' : ''} aria-current={item.key === section ? 'page' : undefined} onClick={() => navigate(item.key)}>{item.icon}<span>{item.label}</span></button>)}</nav>
      <div className="admin-sidebar-foot"><a href="/" target="_blank" rel="noreferrer">打开用户端 <ArrowRightOutlined /></a><small>用户与管理员分别登录</small></div>
    </aside>
    <div className="admin-main"><header className="admin-header"><div><span className="admin-eyebrow">ADMINISTRATION</span><h1>{current.label}</h1><p>{current.detail}</p></div><div className="admin-account"><span className="admin-avatar">{(user.display_name || user.username).slice(0, 1).toUpperCase()}</span><div><strong>{user.display_name || user.username}</strong><small>管理员</small></div><Tooltip title="修改密码"><Button icon={<KeyOutlined />} aria-label="修改管理员密码" onClick={() => setPasswordOpen(true)} /></Tooltip><Tooltip title="退出登录"><Button icon={<LogoutOutlined />} aria-label="退出管理员账号" loading={leaving} onClick={() => void leave()} /></Tooltip></div></header>
      <main className={`admin-body ${section === 'billing' ? 'admin-body-billing' : ''}`}>{section === 'overview' ? <Overview navigate={navigate} /> : section === 'users' ? <UserManagement /> : section === 'models' ? <Models /> : section === 'billing' ? <Billing admin /> : section === 'jobs' ? <Jobs /> : <Audit />}</main>
    </div>
    <Modal title="修改管理员密码" open={passwordOpen} onCancel={() => setPasswordOpen(false)} footer={null} destroyOnHidden><PasswordChangeForm onSubmit={changePassword} /></Modal>
  </div>;
}
