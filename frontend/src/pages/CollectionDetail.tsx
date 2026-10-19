import { useEffect, useRef, useState } from 'react';
import { App, Button, Checkbox, Collapse, Drawer, Input, InputNumber, Popconfirm, Select, Skeleton, Space, Tag, Typography, Card, Alert } from 'antd';
import { DeleteOutlined, PlusOutlined, ReloadOutlined, ExportOutlined, ReadOutlined } from '@ant-design/icons';
import { api, assetURL, errorText } from '../api';
import { useResource } from '../hooks';
import type { Asset, AsyncResult, CollectionDetail, Note, Rewrite, Segment } from '../types';
import { ratio } from '../utils';
import { Failure, Media, Nothing, Status } from '../components/Common';
import CollectionNotePreview from '../components/CollectionNotePreview';
import { workspacePath } from '../platform';
import { useWorkspace, WorkspaceContext } from '../workspace';
import '../styles/collection-detail.css';

function ExtractionEditor({ asset }: { asset: Asset }) {
  const { message } = App.useApp(); const { platform } = useWorkspace(); const [text, setText] = useState(asset.extraction?.text || '');
  const [segments, setSegments] = useState<Segment[]>(asset.extraction?.segments || []); const [busy, setBusy] = useState(false);
  const [timed, setTimed] = useState((asset.extraction?.segments || []).length > 0);
  const saved = useRef(JSON.stringify({ text: asset.extraction?.text || '', segments: asset.extraction?.segments || [] }));
  useEffect(() => {
    const next = JSON.stringify({ text, segments: timed ? segments : [] });
    if (next === saved.current || (timed && segments.some(s => s.end <= s.start || s.start < 0))) return;
    const timer = window.setTimeout(() => {
      setBusy(true);
      void api(workspacePath(`/assets/${asset.id}/text`, platform), 'PATCH', { text, segments: timed ? segments : [] })
        .then(() => { saved.current = next; })
        .catch(e => message.error(errorText(e)))
        .finally(() => setBusy(false));
    }, 500);
    return () => window.clearTimeout(timer);
  }, [asset.id, message, platform, segments, text, timed]);
  const update = (index: number, patch: Partial<Segment>) => setSegments(s => s.map((v, i) => i === index ? { ...v, ...patch } : v));
  return <Card size="small" className="extract-card" title={<Space>素材 {asset.position + 1}<Status value={asset.extraction?.status} />{asset.extraction?.edited && <Tag>已人工修正</Tag>}</Space>}
    extra={busy ? <Typography.Text type="secondary">保存中</Typography.Text> : null}>
    {asset.extraction?.error && <Alert type="warning" showIcon title={asset.extraction.error} style={{ marginBottom: 12 }} />}
    <Input.TextArea aria-label={`素材 ${asset.position + 1} 提取文字`} value={text} onChange={e => setText(e.target.value)} rows={5} placeholder="提取文字或人工补充内容" />
    {(asset.kind === 'video' || segments.length > 0) && <><Checkbox style={{ margin: '14px 0' }} checked={timed} onChange={e => setTimed(e.target.checked)}>保存带时间的转写段落</Checkbox>
      {timed && <><div className="segment-list">{segments.map((s, i) => <div className="segment-row" key={i}>
        <InputNumber min={0} value={s.start} onChange={v => update(i, { start: v || 0 })} addonAfter="秒" />
        <span>→</span><InputNumber min={0} value={s.end} onChange={v => update(i, { end: v || 0 })} addonAfter="秒" />
        <Input value={s.text} onChange={e => update(i, { text: e.target.value })} placeholder="这一段的内容" />
        <Button size="small" danger onClick={() => setSegments(all => all.filter((_, k) => k !== i))}>删除</Button></div>)}</div>
        <Button size="small" icon={<PlusOutlined />} onClick={() => setSegments(all => [...all, { start: all.at(-1)?.end || 0, end: (all.at(-1)?.end || 0) + 1, text: '' }])}>添加时间段</Button></>}
    </>}
  </Card>;
}

export default function CollectionDrawer({ id, close, onRewrite, onDelete, updated }: { id: string; close: () => void; onRewrite: (id: string) => void; onDelete: (note: Note) => void; updated: () => void }) {
  const { message } = App.useApp(); const { platform, active } = useWorkspace();
  const result = useResource<CollectionDetail>(id ? workspacePath(`/collections/${id}`, platform) : null, 2000);
  const [busy, setBusy] = useState(''); const [tagEdit, setTagEdit] = useState<string[] | undefined>();
  const [recollectOpen, setRecollectOpen] = useState(false); const [toolKeys, setToolKeys] = useState<string[]>([]);
  const note = result.data?.note.id === id ? result.data.note : undefined;
  const assets = note ? [...(result.data?.assets || [])].sort((a, b) => a.position - b.position) : [];
  const open = active && !!id;
  useEffect(() => { setToolKeys([]); setTagEdit(undefined); setRecollectOpen(false); }, [id]);
  useEffect(() => { if (!open) setRecollectOpen(false); }, [open]);
  async function action(kind: 'recollect' | 'extract', ids?: string[]) {
    setBusy(kind);
    try { await api<AsyncResult>(workspacePath(`/collections/${id}/${kind}`, platform), 'POST', ids ? { asset_ids: ids } : {}); message.success('任务已提交'); void result.reload(true); }
    catch (e) { message.error(errorText(e)); } finally { setBusy(''); }
  }
  async function tagsSave() {
    setBusy('tags'); try { await api(workspacePath(`/collections/${id}`, platform), 'PATCH', { tags: tagEdit || note?.tags || [] });
      setTagEdit(undefined); message.success('标签已保存'); void result.reload(true); updated();
    } catch (e) { message.error(errorText(e)); } finally { setBusy(''); }
  }
  async function rewrite() {
    setBusy('rewrite'); try { const item = await api<Rewrite>(workspacePath('/rewrites', platform), 'POST', { note_id: id }); close(); onRewrite(item.id); }
    catch (e) { message.error(errorText(e)); } finally { setBusy(''); }
  }
  function toolsChange(keys: string | string[]) {
    const next = Array.isArray(keys) ? keys : [keys]; setToolKeys(next);
    if (!next.includes('extraction') || toolKeys.includes('extraction') || busy) return;
    const pending = assets.filter(a => a.extraction?.status !== 'succeeded' && !a.extraction?.edited && a.extraction?.status !== 'running').map(a => a.id);
    if (pending.length) void action('extract', pending);
  }
  return <Drawer title={<span className="collection-detail-title"><ReadOutlined aria-hidden="true" />笔记阅读</span>} open={open}
    rootClassName={`collection-detail-drawer${!open ? ' inactive-workspace-portal' : ''}`} onClose={close} size={1120}
    extra={<Space wrap><Button icon={<ExportOutlined />} href={note?.url} disabled={!note?.url} target="_blank" rel="noreferrer">查看原文</Button>
      <Button type="primary" disabled={note?.metadata_status !== 'succeeded'} loading={busy === 'rewrite'} icon={<PlusOutlined />} onClick={() => void rewrite()}>加入仿写库</Button></Space>}>
    <WorkspaceContext.Provider value={{ platform, active: open }}>
      <div className="collection-detail">
        <Failure error={result.error} retry={() => void result.reload()} />
        {!note && result.loading && <Skeleton active paragraph={{ rows: 8 }} />}
        {note && <>
          <div className="collection-reader"><CollectionNotePreview key={id} note={note} assets={assets} platform={platform} active={open} /></div>
          <div className="collection-tool-heading"><strong>内容工具</strong>
            <Button danger type="text" icon={<DeleteOutlined />} disabled={!!busy || note.status === 'collecting'} onClick={() => onDelete(note)}>删除记录</Button></div>
          <Collapse className="collection-tools" activeKey={toolKeys} onChange={toolsChange} items={[
            { key: 'media', label: `媒体素材 (${assets.length})`, children: <div className="media-grid">{assets.map(a => <Card size="small" key={a.id} title={`素材 ${a.position + 1}`}
              extra={<Button size="small" href={assetURL(a.id, platform)} download disabled={!['ready', 'succeeded', 'completed', 'success'].includes(a.status)}>下载</Button>}>
              <WorkspaceContext.Provider value={{ platform, active: open && toolKeys.includes('media') }}><Media asset={a} /></WorkspaceContext.Provider>
              <Typography.Text type="secondary">{a.width > 0 ? `${a.width} × ${a.height}` : ''} {a.duration > 0 ? `${a.duration.toFixed(1)} 秒` : ''}</Typography.Text></Card>)}
              {!assets.length && <Nothing description="暂无媒体素材" />}</div> },
            { key: 'extraction', label: 'OCR / 视频转写', children: <><div className="row-between section-gap"><Typography.Text type="secondary">展开后自动提取全部素材；修改内容会自动保存并用于 AI 仿写。</Typography.Text>
              <Button size="small" icon={<ReloadOutlined />} disabled={!assets.length || !!busy} onClick={() => void action('extract', assets.map(a => a.id))}>重新提取</Button></div>
              {assets.map(a => <div key={`${a.id}-${a.extraction?.edited ? 'edited' : ''}-${a.extraction?.status}`} className="extract-wrap"><ExtractionEditor asset={a} /></div>)}
              {!assets.length && <Nothing description="采集媒体后，可以提取图片文字和视频口播" />}</> },
            { key: 'tags', label: '分类标签', children: <Space.Compact style={{ width: '100%' }}><Select mode="tags" aria-label="分类标签" value={tagEdit ?? note.tags ?? []}
              onChange={setTagEdit} style={{ flex: 1, minWidth: 0 }} placeholder="添加分类标签" /><Button onClick={() => void tagsSave()} loading={busy === 'tags'}>保存标签</Button></Space.Compact> },
            { key: 'tasks', label: '处理状态', children: <><div className="stage-grid">{[['元数据', note.metadata_status], ['媒体下载', note.media_status], ['文字提取', note.extraction_status]].map(([label, value]) => <div key={label}><strong>{label}</strong><Status value={value} /></div>)}</div>
              <div className="collection-extra-metrics"><span>赞藏比 <strong>{ratio(note.likes, note.collects)}</strong></span><span>赞评比 <strong>{ratio(note.likes, note.comments)}</strong></span></div>
              {note.error && <Alert showIcon type="error" title="处理遇到问题" description={note.error} style={{ margin: '18px 0' }} />}
              <Popconfirm title="重新采集这篇笔记？" open={open && recollectOpen} rootClassName={!open || !recollectOpen ? 'inactive-workspace-portal' : undefined} onOpenChange={setRecollectOpen} description="更新原始内容与媒体，已生成的仿写版本会继续保留。" onConfirm={() => void action('recollect')}>
                <Button icon={<ReloadOutlined />} loading={busy === 'recollect'}>重新采集</Button></Popconfirm></> },
          ]} />
        </>}
      </div>
    </WorkspaceContext.Provider>
  </Drawer>;
}
