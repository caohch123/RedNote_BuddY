import { useEffect, useMemo, useState } from 'react';
import { App, Button, Checkbox, Image, Input, Pagination, Popover, Select, Space, Spin, Table, Tag, Card, Tooltip } from 'antd';
import type { TableColumnsType } from 'antd';
import { CloudDownloadOutlined, CopyOutlined, DeleteOutlined, EyeOutlined, PlusOutlined, ReloadOutlined, SearchOutlined, SettingOutlined, AppstoreOutlined, UnorderedListOutlined } from '@ant-design/icons';
import { api, assetURL, errorText } from '../api';
import { useResource } from '../hooks';
import type { AsyncResult, List, Note, Rewrite } from '../types';
import { formatDate, number, ratio } from '../utils';
import { Failure, Nothing, Status } from '../components/Common';
import { platformNames, workspacePath } from '../platform';
import { useWorkspace } from '../workspace';
import DeleteConfirmation from '../components/DeleteConfirmation';
import { deletionSuccess, type DeletionResult } from '../deletion';
import WorkspaceTableScrollbar from '../components/WorkspaceTableScrollbar';
import PlatformBadge from '../components/PlatformBadge';
import CollectionFeed from '../components/CollectionFeed';
import CollectionDrawer from './CollectionDetail';
import '../styles/collection-feed.css';

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
  const [view, setView] = useState<'masonry' | 'table'>(() => {
    try { return localStorage.getItem(`workbench.${platform}.collectionView`) === 'table' ? 'table' : 'masonry'; } catch { return 'masonry'; }
  });
  const [feedFields, setFeedFields] = useState<string[]>(() => {
    try { const saved = JSON.parse(localStorage.getItem(`workbench.${platform}.feedFields`) || 'null'); return Array.isArray(saved) ? saved.filter(field => typeof field === 'string' && columnNames[field]) : ['likes', 'collects', 'comments', 'tags', 'collected_at', 'status']; }
    catch { return ['likes', 'collects', 'comments', 'tags', 'collected_at', 'status']; }
  });
  function changeView(next: 'masonry' | 'table') {
    setView(next); setColumnsOpen(false);
    try { localStorage.setItem(`workbench.${platform}.collectionView`, next); } catch { /* Keep the current view when storage is unavailable. */ }
  }
  useEffect(() => { setSelected([]); }, [query, status, tag, sort, order, page, pageSize]);
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
  const notes = result.data?.items || [];
  const allPageSelected = !!notes.length && notes.every(note => selected.includes(note.id));
  async function copyLink(note: Note) {
    try { await navigator.clipboard.writeText(note.url); message.success('笔记链接已复制'); } catch { message.error('无法复制链接，请打开原文后复制'); }
  }
  return <div className="page-stack collection-library-page">
    <Card className="import-card" title={<Space><CloudDownloadOutlined />采集{platformName}笔记</Space>}>
      <Input.TextArea aria-label={`${platformName}链接`} value={text} onChange={e => setText(e.target.value)} rows={3}
        placeholder={`粘贴${platformName}笔记链接、短链接或完整分享文字。仅接收${platformName}链接，多个链接使用空格或换行分隔，最多 50 条。`} />
      <div className="import-actions">
        <Space wrap><Button icon={<CopyOutlined />} onClick={() => void clipboard()}>读取剪贴板</Button>
          <Button onClick={() => void clipboard(true)}>读取并采集</Button><Button onClick={() => setText('')} disabled={!text}>清空</Button>
          <Button type="primary" icon={<CloudDownloadOutlined />} loading={busy} disabled={!text.trim()} onClick={() => void collect()}>开始采集</Button></Space></div>
    </Card>
    <div className={`data-panel collection-table-panel collection-library-panel ${view === 'table' ? 'workspace-table-panel is-table' : 'is-masonry'}`} ref={setTablePanel}><div className="toolbar"><Space wrap>
      <Input.Search allowClear placeholder="搜索标题、正文或博主" prefix={<SearchOutlined />} onSearch={v => { setQuery(v); setPage(1); }} style={{ width: 265, maxWidth: '100%' }} />
      <Select value={status || undefined} allowClear placeholder="采集状态" style={{ width: 130 }} onChange={v => { setStatus(v || ''); setPage(1); }}
        options={['queued', 'collecting', 'ready', 'partial', 'failed', 'waiting_config'].map(v => ({ value: v, label: <Status value={v} /> }))} />
      <Select value={tag || undefined} allowClear showSearch placeholder="分类标签" style={{ width: 130 }} options={tags.map(v => ({ value: v, label: v }))}
        onChange={v => { setTag(v || ''); setPage(1); }} />
      <Select value={sort} style={{ width: 115 }} onChange={v => { setSort(v); setPage(1); }} options={[
        { value: 'collected_at', label: '采集时间' }, { value: 'title', label: '标题' }, { value: 'likes', label: '点赞数' }, { value: 'collects', label: '收藏数' }, { value: 'comments', label: '评论数' }]} />
      <Button onClick={() => setOrder(v => v === 'desc' ? 'asc' : 'desc')}>{order === 'desc' ? '↓ 降序' : '↑ 升序'}</Button>
    </Space><Space><Popover trigger="click" placement="bottomRight" title={view === 'table' ? '显示列' : '卡片信息'} open={active && columnsOpen} rootClassName={!active || !columnsOpen ? 'inactive-workspace-portal' : undefined} onOpenChange={setColumnsOpen} content={<Checkbox.Group value={view === 'table' ? columns : feedFields} className="column-options"
      options={Object.entries(columnNames).filter(([value]) => view === 'table' || !['title', 'cover', 'author', 'platform'].includes(value)).map(([value, label]) => ({ value, label }))} onChange={v => {
        if (view === 'table') setColumns(v as string[]); else setFeedFields(v as string[]);
        try { localStorage.setItem(view === 'table' ? columnsKey : `workbench.${platform}.feedFields`, JSON.stringify(v)); } catch { /* Keep current settings without persistence. */ }
      }} />}>
      <Button icon={<SettingOutlined />}>{view === 'table' ? '列配置' : '显示信息'}</Button></Popover><Button aria-label="刷新采集库" icon={<ReloadOutlined />} onClick={() => { setCoverRefresh(value => value + 1); void result.reload(); }} /></Space></div>
      <div className="collection-library-heading"><div className="collection-library-label"><h2>采集笔记</h2><span>{result.data?.total || 0} 篇灵感</span></div>
        <div className="collection-library-view"><div className="collection-view-switch" role="group" aria-label="采集库视图">
          <Tooltip title="瀑布流视图"><Button type="text" aria-label="瀑布流视图" aria-pressed={view === 'masonry'} className={view === 'masonry' ? 'is-active' : ''} icon={<AppstoreOutlined />} onClick={() => changeView('masonry')} /></Tooltip>
          <Tooltip title="表格视图"><Button type="text" aria-label="表格视图" aria-pressed={view === 'table'} className={view === 'table' ? 'is-active' : ''} icon={<UnorderedListOutlined />} onClick={() => changeView('table')} /></Tooltip>
        </div></div>
      </div>
      {view === 'masonry' && !!notes.length && <div className="collection-library-selection"><Checkbox aria-label="选择本页" checked={allPageSelected} indeterminate={!!selected.length && !allPageSelected} onChange={event => setSelected(event.target.checked ? notes.map(note => note.id) : [])}>选择本页</Checkbox><span>按{columnNames[sort]}{order === 'desc' ? '降序' : '升序'}</span></div>}
      {!!selected.length && <div className="selection-bar"><Space wrap><strong>已选择 {selected.length} 篇</strong><Button type="primary" size="small" disabled={selected.some(key => result.data?.items?.find(note => note.id === key)?.metadata_status !== 'succeeded')} onClick={() => void addRewrite(selected.map(String))} loading={busy}>批量加入仿写库</Button><Button danger size="small" icon={<DeleteOutlined />} disabled={busy} onClick={() => setDeleting({ ids: selected.map(String), subject: `选中的 ${selected.length} 篇采集记录`, batch: true })}>批量删除</Button><Button size="small" onClick={() => setSelected([])}>取消选择</Button></Space></div>}
      <Failure error={result.error} retry={() => void result.reload()} />
      {view === 'masonry' ? <Spin spinning={result.loading}>
        {notes.length ? <CollectionFeed notes={notes} platform={platform} selected={selected} busy={busy} fields={feedFields} coverRefresh={coverRefresh}
          onSelect={(id, checked) => setSelected(keys => checked ? [...new Set([...keys, id])] : keys.filter(key => key !== id))}
          onOpen={setDetailID} onRewrite={id => void addRewrite([id])} onDelete={note => setDeleting({ ids: [note.id], subject: note.title || '这篇采集记录' })} onCopy={note => void copyLink(note)} />
          : !result.loading && <Nothing description={query || status || tag ? '没有找到匹配的笔记，试试调整筛选条件' : '粘贴第一条链接，开始建立你的素材库'} />}
        {!!result.data?.total && <div className="collection-feed-pagination"><Pagination current={page} pageSize={pageSize} total={result.data.total} showSizeChanger responsive showTotal={total => `共 ${total} 篇`}
          onChange={(next, size) => { setPage(next); setPageSize(size); }} /></div>}
      </Spin> : <Table<Note> key={active ? 'visible' : 'hidden'} columns={tableColumns} dataSource={result.data?.items || []} rowKey="id" loading={result.loading} scroll={{ x: 1200 }}
        sticky={{ getContainer: () => tablePanel?.closest<HTMLElement>('.main-content') || window }}
        rowSelection={{ selectedRowKeys: selected, onChange: setSelected }}
        pagination={{ current: page, pageSize, total: result.data?.total || 0, showSizeChanger: true,
          showTotal: t => `共 ${t} 篇`, onChange: (p, size) => { setPage(p); setPageSize(size); } }}
        locale={{ emptyText: <Nothing description="粘贴第一条链接，开始建立你的素材库" /> }} />}
    </div>
    {view === 'table' && <WorkspaceTableScrollbar panel={tablePanel} />}
    <CollectionDrawer key={detailID || 'closed'} id={detailID} close={() => setDetailID('')} onRewrite={onRewrite} onDelete={note => setDeleting({ ids: [note.id], subject: note.title || '这篇采集记录' })} updated={() => void result.reload(true)} />
    <DeleteConfirmation open={!!deleting} title={deleting?.batch ? '批量删除采集记录' : '删除采集记录'} subject={deleting?.subject || ''}
      consequences={['同时删除这些采集记录关联的仿写项目、视频项目、发布稿和后台任务记录。', '删除不再被其他内容引用的本地素材文件。共享素材会保留。']}
      onCancel={() => setDeleting(undefined)} onConfirm={remove} />
  </div>;
}
