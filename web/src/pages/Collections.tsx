import { useEffect, useMemo, useRef, useState } from 'react';
import { App, Button, Checkbox, Divider, Drawer, Image, Input, InputNumber, Popconfirm, Popover, Select,
  Space, Table, Tabs, Tag, Typography, Card, Alert } from 'antd';
import type { TableColumnsType } from 'antd';
import { CloudDownloadOutlined, CopyOutlined, DeleteOutlined, EyeOutlined, PlusOutlined, ReloadOutlined, SearchOutlined, SettingOutlined, ExportOutlined } from '@ant-design/icons';
import { api, assetURL, errorText } from '../api';
import { useResource } from '../hooks';
import type { Asset, AsyncResult, CollectionDetail, List, Note, Rewrite, Segment } from '../types';
import { formatDate, number, ratio } from '../utils';
import { Failure, Field, Media, Nothing, Status } from '../components/Common';
import { platformNames, workspacePath } from '../platform';
import { useWorkspace, WorkspaceContext } from '../workspace';
import DeleteConfirmation from '../components/DeleteConfirmation';
import { deletionSuccess, type DeletionResult } from '../deletion';
import WorkspaceTableScrollbar from '../components/WorkspaceTableScrollbar';
import PlatformBadge from '../components/PlatformBadge';

const defaultColumns = ['title', 'cover', 'author', 'likes', 'collects', 'comments', 'tags', 'collected_at', 'status'];
const columnNames: Record<string, string> = { title: '标题', cover: '封面', author: '博主', likes: '点赞', collects: '收藏',
  comments: '评论', like_collect_ratio: '赞藏比', like_comment_ratio: '赞评比', tags: '分类标签',
  collected_at: '采集时间', published_at: '发布时间', status: '采集状态', platform: '来源平台', shares: '分享' };

export default function Collections({ onRewrite }: { onRewrite: (id: string) => void }) {
  const { message } = App.useApp(); const { platform, active } = useWorkspace(); const platformName = platformNames[platform];
  const [text, setText] = useState(''); const [busy, setBusy] = useState(false);
  const [query, setQuery] = useState(''); const [status, setStatus] = useState(''); const [tag, setTag] = useState('');
  const [sort, setSort] = useState('collected_at'); const [order, setOrder] = useState('desc');
  const [page, setPage] = useState(1); const [pageSize, setPageSize] = useState(20);
  const [selected, setSelected] = useState<React.Key[]>([]); const [detailID, setDetailID] = useState(''); const [columnsOpen, setColumnsOpen] = useState(false);
  const [coverRefresh, setCoverRefresh] = useState(0);
  const [tablePanel, setTablePanel] = useState<HTMLDivElement | null>(null);
  const [deleting, setDeleting] = useState<{ ids: string[]; subject: string; batch?: boolean }>();
  const columnsKey = `workbench.${platform}.columns`;
  const [columns, setColumns] = useState<string[]>(() => {
    try { const saved = JSON.parse(localStorage.getItem(columnsKey) || (platform === 'xiaohongshu' ? localStorage.getItem('xhs.columns') : null) || 'null');
      return Array.isArray(saved) ? saved.filter(x => typeof x === 'string' && columnNames[x]) : defaultColumns;
    } catch { return defaultColumns; }
  });
  const path = workspacePath(`/collections?${new URLSearchParams({ q: query, status, tag, sort, order, page: String(page), page_size: String(pageSize) })}`, platform);
  const result = useResource<List<Note>>(path, 2000);
  async function collect(input = text) {
    if (!input.trim()) { message.warning(`请先输入${platformName}链接、分享文字`); return; }
    setBusy(true);
    try { const submitted = await api<AsyncResult>(workspacePath('/collections', platform), 'POST', { text: input, platform });
      const rejected = submitted.rejected_items || [];
      const accepted = !!(submitted.job_id || submitted.job_ids?.length || submitted.existing_ids?.length);
      if (rejected.length) {
        message.warning(`${accepted ? '已处理当前平台的有效链接；' : ''}${rejected.length} 条未受理：${rejected.map(item => item.error).join('；')}`, 8);
        if (accepted) setText(rejected.map(item => item.url).join('\n'));
      } else { message.success('已提交采集任务，可在后台任务中查看进度'); setText(''); }
      setPage(1); void result.reload(true);
    } catch (e) { message.error(errorText(e)); } finally { setBusy(false); }
  }
  async function clipboard(process = false) {
    try { const input = await navigator.clipboard.readText(); setText(input); if (process) await collect(input); }
    catch (e) { message.error(e instanceof Error && e.name === 'NotAllowedError' ? '浏览器未允许读取剪贴板，请直接粘贴链接' : errorText(e)); }
  }
  async function addRewrite(ids: string[]) {
    setBusy(true);
    try {
      let first = ''; let count = 0;
      for (const note_id of ids) { const item = await api<Rewrite>(workspacePath('/rewrites', platform), 'POST', { note_id }); first ||= item.id; count++; }
      setSelected([]); message.success(ids.length === 1 ? '已打开仿写项目' : `已处理 ${count} 篇笔记`); if (first) onRewrite(first);
    } catch (e) { message.error(errorText(e)); } finally { setBusy(false); }
  }
  async function remove() {
    if (!deleting) return;
    const removed = await api<DeletionResult>(workspacePath(deleting.batch ? '/collections' : `/collections/${encodeURIComponent(deleting.ids[0])}`, platform), 'DELETE', { confirm: true, ...(deleting.batch ? { ids: deleting.ids } : {}) });
    if (deleting.ids.includes(detailID)) setDetailID('');
    setSelected(keys => keys.filter(key => !deleting.ids.includes(String(key))));
    message.success(deletionSuccess(removed, `已删除 ${deleting.ids.length} 篇采集记录及关联内容`)); void result.reload(true);
  }
  const tableColumns = useMemo<TableColumnsType<Note>>(() => {
    const all: TableColumnsType<Note> = [
    { key: 'title', title: '标题', width: 260, fixed: 'left', render: (_, n) => <button className="note-title link-button" onClick={() => setDetailID(n.id)}>{n.title || '等待采集元数据'}<span className="note-type">{n.type === 'video' ? '视频笔记' : n.type === 'normal' || n.type === 'image' ? '图文笔记' : n.type || '笔记'}</span></button> },
    { key: 'platform', title: '来源平台', width: 100, render: () => <PlatformBadge /> },
    { key: 'cover', title: '封面', width: 90, render: (_, n) => n.cover_asset_id
      ? <Image key={`${n.cover_asset_id}:${n.media_status}:${coverRefresh}`} width={52} height={65} style={{ objectFit: 'cover', borderRadius: 5 }}
          src={`${assetURL(n.cover_asset_id, platform)}&cover_refresh=${coverRefresh}`} alt={n.title || '笔记封面'} preview={false} />
      : <div className="cover-placeholder">—</div> },
    { key: 'author', title: '博主', width: 120, render: (_, n) => n.author?.name || '—' },
    { key: 'likes', title: '点赞', width: 90, render: (_, n) => number(n.likes) },
    { key: 'collects', title: '收藏', width: 90, render: (_, n) => number(n.collects) },
    { key: 'comments', title: '评论', width: 90, render: (_, n) => number(n.comments) },
    { key: 'shares', title: '分享', width: 90, render: (_, n) => number(n.shares ?? null) },
    { key: 'like_collect_ratio', title: '赞藏比', width: 90, render: (_, n) => ratio(n.likes, n.collects) },
    { key: 'like_comment_ratio', title: '赞评比', width: 90, render: (_, n) => ratio(n.likes, n.comments) },
    { key: 'tags', title: '分类标签', width: 170, render: (_, n) => <Space size={2} wrap>{n.tags?.length ? n.tags.map(t => <Tag key={t}>{t}</Tag>) : '—'}</Space> },
    { key: 'published_at', title: '发布时间', width: 170, render: (_, n) => formatDate(n.published_at) },
    { key: 'collected_at', title: '采集时间', width: 170, render: (_, n) => formatDate(n.collected_at) },
    { key: 'status', title: '采集状态', width: 120, render: (_, n) => <Status value={n.status} /> },
    { key: 'actions', title: '操作', width: 180, fixed: 'right', render: (_, n) => <Space orientation="vertical" size={2}>
      <Button type="link" size="small" icon={<PlusOutlined />} disabled={n.metadata_status !== 'succeeded'} onClick={() => void addRewrite([n.id])}>加入仿写库</Button>
      <Button type="text" size="small" icon={<EyeOutlined />} onClick={() => setDetailID(n.id)}>查看详情</Button>
      <Button danger type="text" size="small" icon={<DeleteOutlined />} disabled={busy || n.status === 'collecting'} onClick={() => setDeleting({ ids: [n.id], subject: n.title || '这篇采集记录' })}>删除采集记录</Button></Space> },
    ];
    return all.filter(c => c.key === 'actions' || columns.includes(String(c.key)));
  }, [columns, platform, busy, coverRefresh]);
  const tags = [...new Set(result.data?.items?.flatMap(n => n.tags || []) || [])];
  return <div className="page-stack">
    <Card className="import-card" title={<Space><CloudDownloadOutlined />采集{platformName}笔记</Space>}>
      <Input.TextArea aria-label={`${platformName}链接`} value={text} onChange={e => setText(e.target.value)} rows={3}
        placeholder={`粘贴${platformName}笔记链接、短链接或完整分享文字。仅接收${platformName}链接，多个链接使用空格或换行分隔，最多 50 条。`} />
      <div className="import-actions">
        <Space wrap><Button icon={<CopyOutlined />} onClick={() => void clipboard()}>读取剪贴板</Button>
          <Button onClick={() => void clipboard(true)}>读取并采集</Button><Button onClick={() => setText('')} disabled={!text}>清空</Button>
          <Button type="primary" icon={<CloudDownloadOutlined />} loading={busy} disabled={!text.trim()} onClick={() => void collect()}>开始采集</Button></Space></div>
    </Card>
    <div className="data-panel collection-table-panel workspace-table-panel" ref={setTablePanel}><div className="toolbar"><Space wrap>
      <Input.Search allowClear placeholder="搜索标题、正文或博主" prefix={<SearchOutlined />} onSearch={v => { setQuery(v); setPage(1); }} style={{ width: 265, maxWidth: '100%' }} />
      <Select value={status || undefined} allowClear placeholder="采集状态" style={{ width: 130 }} onChange={v => { setStatus(v || ''); setPage(1); }}
        options={['queued', 'collecting', 'ready', 'partial', 'failed', 'waiting_config'].map(v => ({ value: v, label: <Status value={v} /> }))} />
      <Select value={tag || undefined} allowClear showSearch placeholder="分类标签" style={{ width: 130 }} options={tags.map(v => ({ value: v, label: v }))}
        onChange={v => { setTag(v || ''); setPage(1); }} />
      <Select value={sort} style={{ width: 115 }} onChange={v => { setSort(v); setPage(1); }} options={[
        { value: 'collected_at', label: '采集时间' }, { value: 'title', label: '标题' }, { value: 'likes', label: '点赞数' }, { value: 'collects', label: '收藏数' }, { value: 'comments', label: '评论数' }]} />
      <Button onClick={() => setOrder(v => v === 'desc' ? 'asc' : 'desc')}>{order === 'desc' ? '↓ 降序' : '↑ 升序'}</Button>
    </Space><Space><Popover trigger="click" placement="bottomRight" title="显示列" open={active && columnsOpen} rootClassName={!active || !columnsOpen ? 'inactive-workspace-portal' : undefined} onOpenChange={setColumnsOpen} content={<Checkbox.Group value={columns} className="column-options"
      options={Object.entries(columnNames).map(([value, label]) => ({ value, label }))} onChange={v => { setColumns(v as string[]); localStorage.setItem(columnsKey, JSON.stringify(v)); }} />}>
      <Button icon={<SettingOutlined />}>列配置</Button></Popover><Button icon={<ReloadOutlined />} onClick={() => { setCoverRefresh(value => value + 1); void result.reload(); }} /></Space></div>
      {!!selected.length && <div className="selection-bar"><Space wrap><strong>已选择 {selected.length} 篇</strong><Button type="primary" size="small" disabled={selected.some(key => result.data?.items?.find(note => note.id === key)?.metadata_status !== 'succeeded')} onClick={() => void addRewrite(selected.map(String))} loading={busy}>批量加入仿写库</Button><Button danger size="small" icon={<DeleteOutlined />} disabled={busy} onClick={() => setDeleting({ ids: selected.map(String), subject: `选中的 ${selected.length} 篇采集记录`, batch: true })}>批量删除</Button><Button size="small" onClick={() => setSelected([])}>取消选择</Button></Space></div>}
      <Failure error={result.error} retry={() => void result.reload()} />
      <Table<Note> key={active ? 'visible' : 'hidden'} columns={tableColumns} dataSource={result.data?.items || []} rowKey="id" loading={result.loading} scroll={{ x: 1200 }}
        sticky={{ getContainer: () => tablePanel?.closest<HTMLElement>('.main-content') || window }}
        rowSelection={{ selectedRowKeys: selected, onChange: setSelected }}
        pagination={{ current: page, pageSize, total: result.data?.total || 0, showSizeChanger: true,
          showTotal: t => `共 ${t} 篇`, onChange: (p, size) => { setPage(p); setPageSize(size); } }}
        locale={{ emptyText: <Nothing description="粘贴第一条链接，开始建立你的素材库" /> }} />
    </div>
    <WorkspaceTableScrollbar panel={tablePanel} />
    <CollectionDrawer id={detailID} close={() => setDetailID('')} onRewrite={onRewrite} onDelete={note => setDeleting({ ids: [note.id], subject: note.title || '这篇采集记录' })} updated={() => void result.reload(true)} />
    <DeleteConfirmation open={!!deleting} title={deleting?.batch ? '批量删除采集记录' : '删除采集记录'} subject={deleting?.subject || ''}
      consequences={['同时删除这些采集记录关联的仿写项目、视频项目、发布稿和后台任务记录。', '删除不再被其他内容引用的本地素材文件。共享素材会保留。']}
      onCancel={() => setDeleting(undefined)} onConfirm={remove} />
  </div>;
}

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

function CollectionDrawer({ id, close, onRewrite, onDelete, updated }: { id: string; close: () => void; onRewrite: (id: string) => void; onDelete: (note: Note) => void; updated: () => void }) {
  const { message } = App.useApp(); const { platform, active } = useWorkspace(); const result = useResource<CollectionDetail>(id ? workspacePath(`/collections/${id}`, platform) : null, 2000);
  const [busy, setBusy] = useState(''); const [tagEdit, setTagEdit] = useState<string[] | undefined>();
  const [recollectOpen, setRecollectOpen] = useState(false);
  const note = result.data?.note; const assets = result.data?.assets || [];
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
  return <Drawer title={`${platformNames[platform]}笔记详情`} open={active && !!id} rootClassName={!active || !id ? 'inactive-workspace-portal' : undefined} onClose={() => { close(); setTagEdit(undefined); }} size={850}
    extra={<Space wrap><Button danger icon={<DeleteOutlined />} disabled={!note || !!busy || note.status === 'collecting'} onClick={() => note && onDelete(note)}>删除采集记录</Button><Button type="primary" disabled={note?.metadata_status !== 'succeeded'} loading={busy === 'rewrite'} icon={<PlusOutlined />} onClick={() => void rewrite()}>加入仿写库</Button></Space>}>
    <WorkspaceContext.Provider value={{ platform, active: active && !!id }}>
    <Failure error={result.error} retry={() => void result.reload()} />
    {note && <><div className="detail-heading"><Typography.Title level={4}>{note.title || '等待元数据'}</Typography.Title><Space wrap><PlatformBadge /><Status value={note.status} />
      <span>{note.author?.name || '未知博主'}</span><Typography.Text type="secondary">采集于 {formatDate(note.collected_at)}</Typography.Text>
      <Button type="link" icon={<ExportOutlined />} href={note.url} target="_blank" rel="noreferrer">查看原文</Button></Space></div>
      <Space.Compact style={{ width: '100%', marginBottom: 18 }}><Select mode="tags" aria-label="分类标签" value={tagEdit ?? note.tags ?? []}
        onChange={setTagEdit} style={{ flex: 1 }} placeholder="添加分类标签" /><Button onClick={() => void tagsSave()} loading={busy === 'tags'}>保存标签</Button></Space.Compact>
      <Tabs items={[
        { key: 'body', label: '原文与数据', children: <>
          <Typography.Paragraph className="preserve original-body">{note.body || '暂未获取正文'}</Typography.Paragraph>
          <Space wrap>{note.topics?.map(t => <Tag key={t}>#{t.replace(/^#/, '')}</Tag>)}</Space><Divider />
          <div className="metrics-grid">{[['点赞', number(note.likes)], ['收藏', number(note.collects)], ['评论', number(note.comments)],
            ['赞藏比', ratio(note.likes, note.collects)], ['赞评比', ratio(note.likes, note.comments)], ['发布时间', formatDate(note.published_at)]].map(([label, value]) => <div key={label}><span>{label}</span><strong>{value}</strong></div>)}</div>
        </> },
        { key: 'media', label: `媒体素材 (${assets.length})`, children: <div className="media-grid">{assets.map(a => <Card size="small" key={a.id} title={`素材 ${a.position + 1}`}
          extra={<Button size="small" href={assetURL(a.id, platform)} download disabled={a.status !== 'ready' && a.status !== 'succeeded'}>下载</Button>}><Media asset={a} />
          <Typography.Text type="secondary">{a.width > 0 ? `${a.width} × ${a.height}` : ''} {a.duration > 0 ? `${a.duration.toFixed(1)} 秒` : ''}</Typography.Text></Card>)}
          {!assets.length && <Nothing description="暂无媒体素材" />}</div> },
        { key: 'extraction', label: 'OCR / 视频转写', children: <><div className="row-between section-gap"><Typography.Text type="secondary">进入此页自动提取全部素材；修改内容会自动保存并用于 AI 仿写。</Typography.Text></div>
          {assets.map(a => <div key={`${a.id}-${a.extraction?.edited ? 'edited' : ''}-${a.extraction?.status}`} className="extract-wrap"><ExtractionEditor asset={a} /></div>)}
          {!assets.length && <Nothing description="采集媒体后，可以提取图片文字和视频口播" />}</> },
        { key: 'tasks', label: '处理状态', children: <><div className="stage-grid">{[['元数据', note.metadata_status], ['媒体下载', note.media_status], ['文字提取', note.extraction_status]].map(([label, value]) => <div key={label}><strong>{label}</strong><Status value={value} /></div>)}</div>
          {note.error && <Alert showIcon type="error" title="处理遇到问题" description={note.error} style={{ margin: '18px 0' }} />}
          <Popconfirm title="重新采集这篇笔记？" open={active && !!id && recollectOpen} rootClassName={!active || !id || !recollectOpen ? 'inactive-workspace-portal' : undefined} onOpenChange={setRecollectOpen} description="更新原始内容与媒体，已生成的仿写版本会继续保留。" onConfirm={() => void action('recollect')}>
            <Button icon={<ReloadOutlined />} loading={busy === 'recollect'}>重新采集</Button></Popconfirm></> },
      ]} onChange={tab => {
        if (tab !== 'extraction' || busy) return;
        const pending = assets.filter(a => a.extraction?.status !== 'succeeded' && !a.extraction?.edited && a.extraction?.status !== 'running').map(a => a.id);
        if (pending.length) void action('extract', pending);
      }} />
    </>}
    </WorkspaceContext.Provider>
  </Drawer>;
}
