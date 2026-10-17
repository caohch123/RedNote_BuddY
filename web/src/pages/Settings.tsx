import { useEffect, useRef, useState } from 'react';
import { App, Alert, Button, Card, Input, Space, Spin, Tabs, Tag, Typography } from 'antd';
import { CheckCircleOutlined, ExperimentOutlined, ReloadOutlined, SaveOutlined } from '@ant-design/icons';
import { api, errorText } from '../api';
import { useResource } from '../hooks';
import type { Capability, ProviderConfig, Settings as Config } from '../types';
import { Failure, Field } from '../components/Common';
import XHSAccountPanel from '../components/XHSAccountPanel';
import { workspacePath } from '../platform';
import { useWorkspace } from '../workspace';
import WorkspaceManager from '../components/WorkspaceManager';
import WorkspaceActionDock from '../components/WorkspaceActionDock';
import { runtimeDependencyRows, runtimePathChanged } from '../runtimeDependencies';

const capabilities: { key: Capability; title: string; detail: string }[] = [
  { key: 'text', title: '仿写文案', detail: '兼容 Chat Completions 的文字模型接口' },
  { key: 'vision', title: '图片与视频分析', detail: '支持图片输入的视觉理解模型，用于图片特征及视频关键帧分析' },
  { key: 'ocr', title: '图片 OCR', detail: '百炼多模态 OCR 接口' },
  { key: 'asr', title: '视频转写', detail: '百炼 Paraformer WebSocket 音频转写' },
  { key: 'image', title: '图片生成与仿写', detail: '百炼 Qwen-Image 3.0／2.1 支持参考图。/compatible-mode/v1 使用兼容图片协议，/api/v1 使用百炼原生协议；按控制台完整 API Host 配置' },
  { key: 'video', title: '视频生成与仿写', detail: '阿里云百炼万相 3.0 原生异步接口，支持文本、图片及视频参考，模型原生声音；模型可选 wan3.0-video / wan3.0-video-prime' },
];
const ignoreAccount = () => undefined;

export default function Settings() {
  const { platform, active } = useWorkspace();
  const scoped = (path: string) => workspacePath(path, platform);
  const { message } = App.useApp(); const result = useResource<Config>(scoped('/settings')); const health = useResource<unknown>(scoped('/health'));
  const [config, setConfig] = useState<Config>(); const [cookie, setCookie] = useState(''); const [keys, setKeys] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false); const [testing, setTesting] = useState(''); const [testMessages, setTestMessages] = useState<Record<string, { ok: boolean; message: string }>>({});
  const baseline = useRef<Config | undefined>(undefined);
  useEffect(() => {
    const next = result.data; if (!next) return; const previous = baseline.current; baseline.current = next;
    setConfig(current => {
      if (!current || !previous) return next;
      // Refresh shared settings without discarding edits kept in this workspace.
      const merged = { ...next };
      for (const field of ['collector_url', 'ffmpeg_path', 'ffprobe_path', 'node_path'] as const) {
        if (current[field] !== previous[field]) Object.assign(merged, { [field]: current[field] });
      }
      merged.providers = Object.fromEntries(capabilities.map(({ key }) => {
        const provider = { ...next.providers[key] };
        for (const field of Object.keys(current.providers[key] || {}) as (keyof ProviderConfig)[]) {
          if (current.providers[key][field] !== previous.providers[key]?.[field]) Object.assign(provider, { [field]: current.providers[key][field] });
        }
        return [key, provider];
      })) as Config['providers'];
      return merged;
    });
  }, [result.data]);
  useEffect(() => { if (active) void result.reload(true); }, [active, result.reload]);
  function patch(patch: Partial<Config>) { setConfig(c => c ? { ...c, ...patch } : c); }
  function provider(key: Capability, patch: Partial<ProviderConfig>) {
    setConfig(c => c ? { ...c, providers: { ...c.providers, [key]: { ...c.providers[key], ...patch } } } : c);
  }
  const dirty = JSON.stringify(config) !== JSON.stringify(result.data) || !!cookie || Object.values(keys).some(Boolean);
  async function save() {
    if (!config) return; setBusy(true);
    try {
      const previous = baseline.current || result.data; const latest = await api<Config>(scoped('/settings'));
      const changes: Partial<Config> = {};
      for (const field of ['ffmpeg_path', 'ffprobe_path', 'node_path', 'collector_url'] as (keyof Config)[]) {
        if (config[field] !== previous?.[field]) Object.assign(changes, { [field]: config[field] });
      }
      if (cookie) changes.cookie = cookie;
      const changedProviders: Partial<Record<Capability, Partial<ProviderConfig>>> = {};
      for (const { key } of capabilities) {
        const providerChanges: Partial<ProviderConfig> = {};
        for (const field of ['base_url', 'model', 'workspace_id'] as const) {
          if (config.providers[key]?.[field] !== previous?.providers[key]?.[field]) Object.assign(providerChanges, { [field]: config.providers[key]?.[field] });
        }
        if (keys[key]) providerChanges.api_key = keys[key];
        if (Object.keys(providerChanges).length) {
          // The backend treats omitted workspace_id as empty, so preserve its latest value.
          if (!Object.hasOwn(providerChanges, 'workspace_id')) providerChanges.workspace_id = latest.providers[key]?.workspace_id || '';
          changedProviders[key] = providerChanges;
        }
      }
      await api(scoped('/settings'), 'PATCH', { ...changes, providers: changedProviders });
      setCookie(''); setKeys({}); setTestMessages({}); message.success('配置已保存，密钥仅保存在本地后端'); await result.reload(); void health.reload();
    } catch (e) { message.error(errorText(e)); } finally { setBusy(false); }
  }
  async function test(capability: Capability | 'collector') {
    if (dirty) { message.warning('请先保存配置，再检测连接'); return; }
    setTesting(capability);
    try { const tested = await api<{ ok: boolean; message: string }>(scoped('/settings/test'), 'POST', { capability });
      setTestMessages(s => ({ ...s, [capability]: { ok: tested.ok, message: tested.message || (tested.ok ? '连接成功' : '连接失败') } }));
    } catch (e) { setTestMessages(s => ({ ...s, [capability]: { ok: false, message: errorText(e) } })); }
    finally { setTesting(''); }
  }
  const testResult = (key: string) => testMessages[key] && <Alert showIcon type={testMessages[key].ok ? 'success' : 'error'} title={testMessages[key].message} style={{ marginTop: 12 }} />;
  return <div className="page-stack settings-page">
    <Failure error={result.error} retry={() => void result.reload()} />
    <WorkspaceManager />
    <Spin spinning={result.loading}>
      {config && <Tabs tabBarExtraContent={<Button type="primary" icon={<SaveOutlined />} onClick={() => void save()} loading={busy} disabled={!dirty}>保存设置</Button>} items={[
        { key: 'collector', label: '账号与采集', children: <div className="page-stack"><XHSAccountPanel onChange={ignoreAccount} /><Card title="XHS-Downloader V2.8">
          <Field label="采集服务地址"><Input aria-label="采集服务地址" value={config.collector_url} onChange={e => patch({ collector_url: e.target.value })} placeholder="http://127.0.0.1:5556" /></Field>
          <Field label="小红书 Cookie" hint={config.cookie_configured ? `已保存 ${config.cookie_mask || 'Cookie'}；留空保留现有配置` : '未配置。可尝试无 Cookie 采集，部分内容或画质可能受限。'}>
            <Input.Password aria-label="小红书 Cookie" autoComplete="new-password" value={cookie} onChange={e => setCookie(e.target.value)} placeholder="粘贴 Cookie；无需在聊天中发送" /></Field>
          <Button icon={<ExperimentOutlined />} onClick={() => void test('collector')} loading={testing === 'collector'} disabled={dirty}>检测采集服务</Button>
          {testResult('collector')}
        </Card></div> },
        { key: 'providers', label: '共用 AI 服务', children: <div className="page-stack"><Alert type="info" showIcon title="共用 AI 服务" description="修改模型或密钥后，采集、仿写、视频和发布都会使用新的配置。平台账号、采集内容和发布稿仍分别管理。" /><div className="provider-grid">
          {capabilities.map(({ key, title, detail }) => { const p = config.providers[key] || { base_url: '', model: '', configured: false };
            const card = <Card key={key} title={<Space>{title}<Tag color={p.configured ? 'success' : 'warning'}>{p.configured ? '已配置' : '待配置'}</Tag></Space>}>
              <Typography.Paragraph type="secondary">{detail}</Typography.Paragraph>
              <Field label="API 地址" hint={key === 'ocr' ? 'qwen3.5-ocr 使用工作空间专属地址：https://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/compatible-mode/v1' : undefined}><Input aria-label={`${title} API 地址`} value={p.base_url} placeholder={key === 'image' || key === 'vision' ? 'https://dashscope.aliyuncs.com/compatible-mode/v1' : undefined} onChange={e => provider(key, { base_url: e.target.value })} /></Field>
              <Field label="模型"><Input aria-label={`${title} 模型`} value={p.model} placeholder={key === 'video' ? 'wan3.0-video' : key === 'image' ? '供应商图像模型名称' : undefined} onChange={e => provider(key, { model: e.target.value })} /></Field>
              <Field label="API Key" hint={p.configured ? `${p.key_mask || '已保存'} · 留空保留现有密钥` : '填写密钥并保存后启用此能力'}><Input.Password aria-label={`${title} API Key`} autoComplete="new-password"
                value={keys[key] || ''} onChange={e => setKeys(s => ({ ...s, [key]: e.target.value }))} placeholder="请输入 API Key" /></Field>

              {key === 'asr' && <Field label="百炼工作空间 ID（可选）"><Input value={p.workspace_id || ''} onChange={e => provider(key, { workspace_id: e.target.value })} /></Field>}
              <Button icon={<ExperimentOutlined />} loading={testing === key} disabled={dirty} onClick={() => void test(key)}>检测连接</Button>{testResult(key)}
            </Card>;
            return card;
          })}
        </div></div> },
        { key: 'runtime', label: '本地依赖', children: <Card className="runtime-dependencies" title="本地工具与服务" extra={<Button icon={<ReloadOutlined />} disabled={dirty} loading={health.loading} onClick={() => void health.reload()}>检查依赖</Button>}>
          <Typography.Paragraph type="secondary">路径留空自动检测，也可填写程序名称、完整路径或项目内相对路径。修改后先保存，再检查依赖。</Typography.Paragraph>
          <Failure error={health.error} retry={() => void health.reload()} />
          <div className="runtime-table" role="table" aria-label="本地工具与服务">
            <div className="runtime-table-header" role="row">{['工具', '用途', '状态', '可编辑路径'].map(title => <span role="columnheader" key={title}>{title}</span>)}</div>
            {runtimeDependencyRows(health.data).map(item => {
              const pending = !!item.field && runtimePathChanged(item.field, config, result.data);
              const value = item.field ? config[item.field] || '' : '';
              return <div className="runtime-table-row" role="row" key={item.name}>
                <strong className="runtime-name" role="cell">{item.label}</strong>
                <span className="runtime-usage" role="cell">{item.usage}</span>
                <div className="runtime-status" role="cell">{pending ? <Tag color="warning">待保存</Tag> : !item.checked || health.error ? <Tag>未检查</Tag> : item.ok ? <Tag color="success" icon={<CheckCircleOutlined />}>已就绪</Tag> : <Tag color="warning">未就绪</Tag>}</div>
                <div className="runtime-path" role="cell">{item.field ? <Input aria-label={`${item.label} 路径`} value={value} placeholder="自动检测" onChange={event => patch({ [item.field!]: event.target.value })} /> : <Typography.Text type="secondary">{item.name === 'database' ? '内置，无需配置' : '在账号与采集中配置'}</Typography.Text>}
                  {!pending && item.field && item.path && value.trim() !== item.path && <div className="runtime-detail">实际：{item.path}</div>}
                  {!pending && (item.message || item.version) && <div className="runtime-detail">{item.message || item.version}</div>}
                </div>
              </div>;
            })}
          </div>
        </Card> },
      ]} />}
    </Spin>
    <WorkspaceActionDock className="settings-footer"><div className="settings-note">小红书登录凭据、Cookie 和密钥由本地后端加密保存，页面只读取掩码。共用 AI 配置修改后请先保存，再检测连接。</div>
      <Button type="primary" icon={<SaveOutlined />} onClick={() => void save()} loading={busy} disabled={!config || !dirty}>保存设置</Button></WorkspaceActionDock>
  </div>;
}
