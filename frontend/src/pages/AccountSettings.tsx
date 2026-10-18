import { useState } from 'react';
import { Alert, App, Avatar, Button, Card, Descriptions, Input, Popconfirm, Space, Spin, Tag, Typography } from 'antd';
import { LockOutlined, SaveOutlined, UserOutlined } from '@ant-design/icons';
import { api, errorText } from '../api';
import { PasswordChangeForm } from '../auth';
import { useAuth } from '../auth-context';
import { Failure, Field } from '../components/Common';
import XHSAccountPanel from '../components/XHSAccountPanel';
import { useResource } from '../hooks';
import { workspacePath } from '../platform';
import type { Capability, Settings } from '../types';
import { useWorkspace } from '../workspace';

const ignoreAccount = () => undefined;
const capabilities: { key: Capability; label: string }[] = [
  { key: 'text', label: '文案创作' }, { key: 'vision', label: '视觉分析' },
  { key: 'image', label: '图片生成' }, { key: 'video', label: '视频生成' },
  { key: 'ocr', label: '图片文字提取' }, { key: 'asr', label: '视频转写' },
];

export default function AccountSettings() {
  const { user, changePassword } = useAuth();
  const { platform } = useWorkspace();
  const { message } = App.useApp();
  const scoped = workspacePath('/settings', platform);
  const settings = useResource<Settings>(scoped);
  const [cookie, setCookie] = useState('');
  const [saving, setSaving] = useState(false);
  const [clearing, setClearing] = useState(false);
  async function saveCookie(clear = false) {
    if (!clear && !cookie.trim()) return;
    clear ? setClearing(true) : setSaving(true);
    try {
      await api(scoped, 'PATCH', { cookie: clear ? '' : cookie.trim() });
      setCookie(''); await settings.reload(); message.success(clear ? '采集 Cookie 已清除' : '采集 Cookie 已保存');
    } catch (reason) { message.error(errorText(reason)); }
    finally { setSaving(false); setClearing(false); }
  }
  return <div className="page-stack account-settings-page">
    <div className="account-settings-heading"><Avatar icon={<UserOutlined />} size={42} /><div><Typography.Title level={3}>账户设置</Typography.Title><Typography.Text type="secondary">管理你的登录密码与小红书连接。</Typography.Text></div></div>
    <Card title="工作台账户">
      <Descriptions column={{ xs: 1, sm: 2, md: 2 }} items={[
        { key: 'name', label: '显示名称', children: user.display_name || user.username },
        { key: 'username', label: '用户名', children: user.username },
        { key: 'role', label: '角色', children: <Tag color="blue">普通用户</Tag> },
        { key: 'created', label: '创建时间', children: user.created_at ? new Date(user.created_at).toLocaleString('zh-CN') : '—' },
        { key: 'daily', label: '每日调用额度', children: user.daily_request_limit > 0 ? `${user.daily_request_limit.toLocaleString()} 次` : '不限' },
        { key: 'concurrent', label: '模型请求并发上限', children: user.max_concurrent > 0 ? `${user.max_concurrent} 次` : '不限' },
      ]} />
    </Card>
    <Card title={<Space><LockOutlined />修改登录密码</Space>}><PasswordChangeForm onSubmit={changePassword} /></Card>
    <Card title="小红书发布账号"><XHSAccountPanel onChange={ignoreAccount} /></Card>
    <Card title="采集 Cookie">
      <Failure error={settings.error} retry={() => void settings.reload()} />
      <Spin spinning={settings.loading}>
        <Field label="小红书 Cookie" hint={settings.data?.cookie_configured ? '已保存。留空保留现有 Cookie，填写新值后可替换。' : '尚未配置。部分公开笔记支持直接采集。'}>
          <Input.Password autoComplete="new-password" aria-label="小红书 Cookie" value={cookie} onChange={event => setCookie(event.target.value)} placeholder="粘贴你的小红书 Cookie" disabled={saving || clearing} />
        </Field>
        <div className="account-cookie-actions"><Button type="primary" icon={<SaveOutlined />} onClick={() => void saveCookie()} disabled={!cookie.trim() || clearing} loading={saving}>保存 Cookie</Button>
          <Popconfirm title="清除已保存的采集 Cookie？" okText="清除" okButtonProps={{ 'aria-label': '确认清除采集 Cookie' }} cancelText="取消" onConfirm={() => void saveCookie(true)}>
            <Button danger disabled={!settings.data?.cookie_configured || saving} loading={clearing}>清除 Cookie</Button>
          </Popconfirm>
        </div>
      </Spin>
    </Card>
    <Card title="平台模型服务">
      <Alert type="info" showIcon title="由管理员统一配置" description="你可以直接使用已配置的模型。模型地址、密钥和运行环境由管理员维护。" />
      {settings.data && <div className="account-config-models">{capabilities.map(({ key, label }) => {
        const provider = settings.data!.providers[key];
        return <div className="account-config-model" key={key}><div><strong>{label}</strong><Tag color={provider?.configured ? 'success' : 'default'}>{provider?.configured ? '可用' : '待配置'}</Tag></div>
          <Typography.Text type="secondary">{provider?.model || '尚未设置模型'}</Typography.Text></div>;
      })}</div>}
    </Card>
  </div>;
}
