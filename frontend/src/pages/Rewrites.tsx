import { useEffect, useRef, useState } from 'react';
import { App, Alert, Button, Card, Dropdown, Input, Modal, Select,
  Space, Spin, Tabs, Tag, Typography, Upload } from 'antd';
import { CheckCircleOutlined, DeleteOutlined, EditOutlined, EllipsisOutlined, FileDoneOutlined, FormOutlined,
  PushpinFilled, PushpinOutlined, ReloadOutlined, UploadOutlined } from '@ant-design/icons';
import { api, errorText, upload } from '../api';
import { useResource } from '../hooks';
import type { Asset, AsyncResult, Draft, Job, LibraryMedia, List, Publication, Rewrite, RewriteDetail, RewriteOptions, Version } from '../types';
import { applyImageToDraft, characterCount, formatDate, toDraft, truncateCharacters } from '../utils';
import { Failure, Field, Nothing } from '../components/Common';
import VideoRewriteWorkspace from '../components/VideoRewriteWorkspace';
import { applyVideoToDraft } from '../videoRewrite';
import ImageRewriteWorkspace from '../components/ImageRewriteWorkspace';
import ProjectMediaLibrary from '../components/ProjectMediaLibrary';
import MediaSelectionGrid from '../components/MediaSelectionGrid';
import RewriteCopyComposer from '../components/RewriteCopyComposer';
import { platformNames, workspacePath } from '../platform';
import { useWorkspace } from '../workspace';
import DeleteConfirmation from '../components/DeleteConfirmation';
import { deletionSuccess, type DeletionResult } from '../deletion';
import { jobWasCleared, useJobsCleared } from '../jobEvents';

const defaults: RewriteOptions = { audience: '普通读者', tone: '自然分享', length: '中等（约 500 字）', instructions: '', include_body: true, extraction_asset_ids: [] };
const REWRITE_LIST_MIN_WIDTH = 180;
const REWRITE_LIST_MAX_WIDTH = 480;
const REWRITE_WORKSPACE_MIN_WIDTH = 320;
const REWRITE_RESIZER_WIDTH = 8;

function clampRewriteListWidth(width: number, layoutWidth: number) {
  const maxWidth = Math.min(
    REWRITE_LIST_MAX_WIDTH,
    layoutWidth - REWRITE_WORKSPACE_MIN_WIDTH - REWRITE_RESIZER_WIDTH,
  );
  return Math.max(REWRITE_LIST_MIN_WIDTH, Math.min(maxWidth, width));
}

export default function Rewrites({ initialID, navigation, jobs, onPublication }: { initialID: string; navigation: number; jobs: Job[]; onPublication: (id: string) => void }) {
  const { platform, active: workspaceActive } = useWorkspace();
  const { message } = App.useApp();
  const list = useResource<List<Rewrite>>(workspacePath('/rewrites', platform), 2000);
  const [active, setActive] = useState(initialID);
  const [query, setQuery] = useState('');
  const [creatingBlank, setCreatingBlank] = useState(false); const blankInFlight = useRef(false);
  const [listWidth, setListWidth] = useState(() => {
    try {
      const saved = Number(localStorage.getItem(`workbench.${platform}.rewriteListWidth`));
      return Number.isFinite(saved) && saved >= REWRITE_LIST_MIN_WIDTH
        ? Math.min(REWRITE_LIST_MAX_WIDTH, saved)
        : 280;
    } catch {
      return 280;
    }
  });
  const [resizingList, setResizingList] = useState(false);
  const layout = useRef<HTMLDivElement>(null);
  const listDrag = useRef<{ pointer: number; offset: number } | null>(null);
  const [deleting, setDeleting] = useState<Rewrite>();
  const [renaming, setRenaming] = useState<Rewrite>(); const [projectName, setProjectName] = useState(''); const [renameBusy, setRenameBusy] = useState(false);
  const renameInFlight = useRef(false); const [projectMenu, setProjectMenu] = useState('');
  const [pinChanges, setPinChanges] = useState<Record<string, boolean>>({});
  const [pinning, setPinning] = useState<string[]>([]); const pinInFlight = useRef(new Set<string>());
  const previousIDs = useRef(new Set<string>());
  const deletedIDs = useRef(new Set<string>());
  useEffect(() => { if (!workspaceActive) setProjectMenu(''); }, [workspaceActive]);
  useEffect(() => { if (initialID) setActive(initialID); }, [initialID, navigation]);
  useEffect(() => { const first = list.data?.items?.find(item => !deletedIDs.current.has(item.id)); if (!active && first) setActive(first.id); }, [active, list.data]);
  useEffect(() => {
    if (!list.data) return;
    const next = new Set(list.data.items.map(item => item.id));
    if (active && previousIDs.current.has(active) && !next.has(active)) setActive(list.data.items.find(item => !deletedIDs.current.has(item.id))?.id || '');
    previousIDs.current = next;
  }, [list.data, active]);
  useEffect(() => {
    if (!list.data) return;
    setPinChanges(previous => Object.fromEntries(Object.entries(previous).filter(([id, pinned]) => !list.data!.items.some(item => item.id === id && !!item.pinned === pinned))));
  }, [list.data]);

  function updateListWidth(width: number) {
    const availableWidth = layout.current?.clientWidth ?? window.innerWidth - 224;
    setListWidth(clampRewriteListWidth(width, availableWidth));
  }

  function startListResize(event: React.PointerEvent<HTMLDivElement>) {
    if (event.button !== 0 || window.innerWidth <= 900 || !layout.current) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    listDrag.current = {
      pointer: event.pointerId,
      offset: event.clientX - event.currentTarget.getBoundingClientRect().left,
    };
    setResizingList(true);
  }

  function moveListResize(event: React.PointerEvent<HTMLDivElement>) {
    const drag = listDrag.current;
    if (drag?.pointer !== event.pointerId || !layout.current) return;
    const bounds = layout.current.getBoundingClientRect();
    updateListWidth(event.clientX - bounds.left - drag.offset);
  }

  function endListResize(event: React.PointerEvent<HTMLDivElement>) {
    if (listDrag.current?.pointer !== event.pointerId) return;
    listDrag.current = null;
    setResizingList(false);
    try {
      localStorage.setItem(`workbench.${platform}.rewriteListWidth`, String(listWidth));
    } catch {
      /* Keep the resized width for this session if browser storage is unavailable. */
    }
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
  }

  function resizeListWithKeyboard(event: React.KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
    event.preventDefault();
    const availableWidth = layout.current?.clientWidth ?? window.innerWidth - 224;
    const direction = event.key === 'ArrowRight' ? 1 : -1;
    const nextWidth = clampRewriteListWidth(listWidth + direction * 16, availableWidth);
    setListWidth(nextWidth);
    try {
      localStorage.setItem(`workbench.${platform}.rewriteListWidth`, String(nextWidth));
    } catch {
      /* Keep the resized width for this session if browser storage is unavailable. */
    }
  }

  useEffect(() => {
    const constrainListWidth = () => updateListWidth(listWidth);
    window.addEventListener('resize', constrainListWidth);
    return () => window.removeEventListener('resize', constrainListWidth);
  }, [listWidth]);

  async function togglePin(rewrite: Rewrite) {
    if (pinInFlight.current.has(rewrite.id)) return;
    pinInFlight.current.add(rewrite.id); setPinning(ids => [...ids, rewrite.id]);
    try {
      const saved = await api<Rewrite>(workspacePath(`/rewrites/${encodeURIComponent(rewrite.id)}`, platform), 'PATCH', { pinned: !rewrite.pinned });
      setPinChanges(previous => ({ ...previous, [saved.id]: !!saved.pinned }));
      await list.reload(true);
    } catch (e) { message.error(errorText(e)); }
    finally { pinInFlight.current.delete(rewrite.id); setPinning(ids => ids.filter(id => id !== rewrite.id)); }
  }
  async function rename() {
    if (!renaming || renameInFlight.current) return;
    const name = projectName.trim();
    if (!name || characterCount(name) > 100) { message.warning('项目名称不能为空，且不能超过 100 个字符'); return; }
    renameInFlight.current = true; setRenameBusy(true);
    try {
      await api<Rewrite>(workspacePath(`/rewrites/${encodeURIComponent(renaming.id)}`, platform), 'PATCH', { title: name });
      setRenaming(undefined); message.success('项目已重命名'); await list.reload(true);
    } catch (e) { message.error(errorText(e)); }
    finally { renameInFlight.current = false; setRenameBusy(false); }
  }
  async function createBlank() {
    if (blankInFlight.current) return;
    blankInFlight.current = true; setCreatingBlank(true);
    try {
      const created = await api<Rewrite>(workspacePath('/rewrites', platform), 'POST', { blank: true });
      setQuery(''); setActive(created.id); await list.reload(true);
    } catch (e) { message.error(errorText(e)); }
    finally { blankInFlight.current = false; setCreatingBlank(false); }
  }
  const deleteBusy = (id: string) => jobs.some(job => job.entity_id === id && ['running', 'submission_unknown'].includes(job.state));
  const items = (list.data?.items || []).filter(r => !deletedIDs.current.has(r.id) && (r.title || '').includes(query))
    .map(r => r.id in pinChanges ? { ...r, pinned: pinChanges[r.id] } : r)
    .sort((a, b) => Number(!!b.pinned) - Number(!!a.pinned) || (b.updated_at || '').localeCompare(a.updated_at || ''));
  return <div className="page-stack">
    <Failure error={list.error} retry={() => void list.reload()} />
    <div
      ref={layout}
      className={`rewrite-layout${resizingList ? ' resizing-rewrite-list' : ''}`}
      style={{ '--rewrite-list-width': `${listWidth}px` } as React.CSSProperties}
    ><aside className="rewrite-list">
      <div className="rewrite-list-toolbar"><Input.Search allowClear value={query} onChange={e => setQuery(e.target.value)} placeholder="搜索仿写项目" />
        <Button type="text" className="rewrite-create-blank" aria-label="新增空白项目" title="新增空白项目" icon={<FormOutlined />} loading={creatingBlank} onClick={() => void createBlank()} /></div>
      <Spin spinning={list.loading}><div role="list" className="rewrite-items">
        {items.map(r => <div key={r.id} className={`rewrite-list-row${active === r.id ? ' selected' : ''}`} data-rewrite-id={r.id}><button title={r.title || '未命名项目'} className={`rewrite-list-item ${active === r.id ? 'selected' : ''}`} onClick={() => setActive(r.id)}>
          <div><strong>{r.title || '未命名项目'}</strong><div className="rewrite-meta"><span>{r.versions?.length || 0} 个版本</span>
            {r.confirmed_version_id ? <Tag color="success">已确认</Tag> : <Tag>草稿</Tag>}</div>
            <small>{formatDate(r.updated_at)}</small></div></button><div className="rewrite-row-actions">
              <Dropdown trigger={['click']} placement="bottomRight" open={workspaceActive && projectMenu === r.id} onOpenChange={open => setProjectMenu(open ? r.id : '')}
                classNames={{ root: workspaceActive ? 'rewrite-project-menu' : 'rewrite-project-menu inactive-workspace-portal' }}
                menu={{ items: [
                  { key: 'rename', label: '重命名', icon: <EditOutlined aria-hidden /> },
                  { key: 'delete', label: '删除', icon: <DeleteOutlined aria-hidden />, disabled: deleteBusy(r.id) },
                ], onClick: ({ key }) => { setProjectMenu(''); if (key === 'rename') { setProjectName(r.title || ''); setRenaming(r); } else if (key === 'delete') setDeleting(r); } }}>
                <Button type="text" size="small" className="rewrite-menu-button" aria-label={`项目菜单 ${r.title || '未命名项目'}`} title="项目菜单" aria-haspopup="menu" aria-expanded={workspaceActive && projectMenu === r.id} icon={<EllipsisOutlined />} />
              </Dropdown>
              <Button type="text" size="small" className={`rewrite-pin${r.pinned ? ' is-pinned' : ''}`} aria-label={`${r.pinned ? '取消置顶' : '置顶'}仿写项目 ${r.title || '未命名项目'}`} title={r.pinned ? '取消置顶' : '置顶项目'} aria-pressed={!!r.pinned} icon={r.pinned ? <PushpinFilled /> : <PushpinOutlined />} loading={pinning.includes(r.id)} onClick={() => void togglePin(r)} />
            </div></div>)}
        {!items.length && <Nothing description="从采集库加入笔记" />}
      </div></Spin>
    </aside><div
      className="rewrite-resizer"
      role="separator"
      tabIndex={0}
      aria-label="调整项目列表与工作区宽度"
      aria-orientation="vertical"
      aria-valuemin={REWRITE_LIST_MIN_WIDTH}
      aria-valuemax={Math.max(REWRITE_LIST_MIN_WIDTH, (layout.current?.clientWidth ?? window.innerWidth - 224) - REWRITE_WORKSPACE_MIN_WIDTH - REWRITE_RESIZER_WIDTH)}
      aria-valuenow={listWidth}
      onPointerDown={startListResize}
      onPointerMove={moveListResize}
      onPointerUp={endListResize}
      onPointerCancel={endListResize}
      onLostPointerCapture={() => {
        listDrag.current = null;
        setResizingList(false);
      }}
      onKeyDown={resizeListWithKeyboard}
    /><div className="rewrite-main">
      {active ? <RewriteEditor key={active} id={active} jobs={jobs} refreshList={() => void list.reload(true)} onPublication={onPublication} />
        : <Nothing description="先从采集库选择笔记，加入仿写库" />}
    </div></div>
    <Modal title="重命名项目" open={workspaceActive && !!renaming} rootClassName={!workspaceActive || !renaming ? 'inactive-workspace-portal' : undefined} destroyOnHidden
      onCancel={() => { if (!renameBusy) setRenaming(undefined); }} onOk={() => void rename()} confirmLoading={renameBusy} okText="保存" cancelText="取消"
      okButtonProps={{ 'aria-label': '保存项目名称' }} cancelButtonProps={{ disabled: renameBusy }} closable={!renameBusy} keyboard={!renameBusy} maskClosable={!renameBusy}>
      <Input autoFocus aria-label="项目名称" value={projectName} disabled={renameBusy}
        count={{ max: 100, strategy: characterCount, exceedFormatter: (value, { max }) => truncateCharacters(value, max) }}
        onChange={e => setProjectName(e.target.value)} onPressEnter={() => void rename()} />
    </Modal>
    <DeleteConfirmation open={workspaceActive && !!deleting} title="删除仿写项目" subject={deleting?.title || '这个仿写项目'}
      consequences={['删除项目的全部仿写版本、关联视频项目、发布稿和后台任务记录。', '原始采集记录会保留；不再被引用的项目素材文件会删除。']}
      onCancel={() => setDeleting(undefined)} onConfirm={async () => {
        if (!deleting) return;
        const removed = await api<DeletionResult>(workspacePath(`/rewrites/${encodeURIComponent(deleting.id)}`, platform), 'DELETE', { confirm: true });
        deletedIDs.current.add(deleting.id);
        if (active === deleting.id) setActive('');
        message.success(deletionSuccess(removed, '仿写项目及关联内容已删除')); await list.reload(true);
      }} />
  </div>;
}

function RewriteEditor({ id, jobs, refreshList, onPublication }: { id: string; jobs: Job[]; refreshList: () => void; onPublication: (id: string) => void }) {
  const { message } = App.useApp(); const { platform, active } = useWorkspace();
  const result = useResource<RewriteDetail>(workspacePath(`/rewrites/${id}`, platform), 2000);
  const [activeVersion, setActiveVersion] = useState('');
  const [localVersion, setLocalVersion] = useState<Version>();
  const [editorTab, setEditorTab] = useState('editor');
  const publicationLibrary = useResource<List<LibraryMedia>>(active && editorTab === 'media' ? workspacePath(`/media-library?rewrite_id=${encodeURIComponent(id)}`, platform) : null, 5000);
  const [displayModel, setDisplayModel] = useState('默认模型');
  const [draft, setDraft] = useState<Draft>(toDraft()); const [options, setOptions] = useState<RewriteOptions>(defaults);
  const [libraryAssets, setLibraryAssets] = useState<Asset[]>([]);
  const [busy, setBusy] = useState('');
  const [publishOpen, setPublishOpen] = useState(false);
  const [pendingVersion, setPendingVersion] = useState('');
  const [awaitingCount, setAwaitingCount] = useState<number>(); const [awaitingID, setAwaitingID] = useState(''); const [generationError, setGenerationError] = useState(''); const initialized = useRef(false);
  const detail = result.data; const rewrite = detail?.rewrite;
  const savedVersions = rewrite?.versions || [];
  const versions = localVersion && !savedVersions.some(v => v.id === localVersion.id) ? [...savedVersions, localVersion] : savedVersions;
  const current = versions.find(v => v.id === activeVersion);
  const sourceAssetIDs = new Set([...(detail?.note.asset_ids || []), ...(detail?.note.cover_asset_id ? [detail.note.cover_asset_id] : [])]);
  const sourceAssets = detail?.assets.filter(asset => sourceAssetIDs.has(asset.id)) || [];
  const extractionAssetIDs = sourceAssets.filter(asset => (asset.extraction?.status === 'succeeded' || asset.extraction?.edited)
    && (asset.extraction?.text?.trim() || asset.extraction?.segments?.some(segment => segment.text.trim()))).map(asset => asset.id);
  const pendingJob = jobs.find(j => j.id === awaitingID);
  useJobsCleared(platform, ids => {
    if (ids.includes(awaitingID)) { setAwaitingCount(undefined); setAwaitingID(''); }
    void result.reload(true);
  });
  const baseline = toDraft(current);
  const textDirty = draft.title !== baseline.title || draft.body !== baseline.body || JSON.stringify(draft.topics) !== JSON.stringify(baseline.topics);
  const mediaDirty = JSON.stringify(draft.asset_ids) !== JSON.stringify(baseline.asset_ids);
  const dirty = textDirty || mediaDirty;
  const confirmed = !!current && rewrite?.confirmed_version_id === current.id;
  const titleLimit = platform === 'xiaohongshu' ? 20 : 100;
  const titleTooLong = characterCount(draft.title) > titleLimit;
  const titleError = `标题不能超过 ${titleLimit} 个字符，请缩短后保存`;
  useEffect(() => {
    if (!detail || initialized.current) return;
    initialized.current = true; setOptions({ ...defaults, ...detail.rewrite.options, include_body: true, extraction_asset_ids: [] });
    const last = detail.rewrite.versions?.at(-1);
    if (last) { setActiveVersion(last.id); setDraft(toDraft(last)); }
    else setDraft({ title: detail.note.title || '', body: detail.note.body || '', script: '', topics: detail.note.topics || [], asset_ids: detail.note.asset_ids || [] });
  }, [detail]);
  useEffect(() => {
    if (awaitingCount !== undefined && versions.length > awaitingCount && (!awaitingID || pendingJob?.state === 'succeeded')) {
      const last = versions.at(-1)!; setActiveVersion(last.id); setDraft(d => ({ ...toDraft(last), asset_ids: d.asset_ids })); setAwaitingCount(undefined); message.success('新仿写版本已生成');
    }
  }, [versions, awaitingCount, awaitingID, pendingJob, message]);
  useEffect(() => {
    if (awaitingCount !== undefined && pendingJob && ['failed', 'waiting_config', 'blocked', 'paused', 'submission_unknown'].includes(pendingJob.state)) {
      setAwaitingCount(undefined); setAwaitingID(''); setGenerationError(pendingJob.error || pendingJob.message || '仿写任务未完成，请在后台任务中处理');
    }
  }, [awaitingCount, pendingJob]);
  async function save() {
    if (!draft.title.trim() || !draft.body.trim()) { message.warning('请填写标题与正文'); return; }
    if (titleTooLong) { message.warning(titleError); return; }
    setBusy('save');
    try { const saved = await api<Version>(workspacePath(`/rewrites/${id}/versions`, platform), 'POST', { ...draft, parent_version_id: activeVersion || undefined });
      setLocalVersion(saved); setActiveVersion(saved.id); setDraft(toDraft(saved)); message.success('已保存新版本'); await result.reload(true); refreshList();
    } catch (e) { message.error(errorText(e)); } finally { setBusy(''); }
  }
  async function generate() {
    setBusy('generate');
    try { const job = await api<AsyncResult>(workspacePath(`/rewrites/${id}/generate`, platform), 'POST', { ...options, include_body: true, extraction_asset_ids: extractionAssetIDs }); if (job.job_id && jobWasCleared(platform, job.job_id)) return; setAwaitingCount(versions.length); setAwaitingID(job.job_id || ''); setGenerationError('');
      message.success('仿写任务已提交，生成后将显示新版本');
    } catch (e) { message.error(errorText(e)); } finally { setBusy(''); }
  }
  function switchVersion(value: string) {
    const next = versions.find(v => v.id === value); if (!next) return;
    const choose = () => { setActiveVersion(value); setDraft(toDraft(next)); };
    if (dirty) setPendingVersion(value);
    else choose();
  }
  async function confirm() {
    if (titleTooLong) { message.warning(titleError); return; }
    if (!activeVersion || dirty) { message.warning('请先保存当前内容为新版本'); return; }
    setBusy('confirm');
    try { await api(workspacePath(`/rewrites/${id}/confirm`, platform), 'POST', { version_id: activeVersion }); message.success('已确认此版本'); await result.reload(true); refreshList(); }
    catch (e) { message.error(errorText(e)); } finally { setBusy(''); }
  }
  async function addFile(file: File) {
    setBusy('upload');
    try { const asset = await upload(file, id, workspacePath('/assets', platform)) as Asset; setLibraryAssets(items => [...items, asset]); setDraft(d => ({ ...d, asset_ids: [...d.asset_ids, asset.id] }));
      message.success('素材已上传，保存版本后生效'); await result.reload(true);
    } catch (e) { message.error(errorText(e)); } finally { setBusy(''); }
    return false;
  }
  async function publish() {
    if (titleTooLong) { message.warning(titleError); return; }
    if (!current || dirty || !confirmed) { message.warning('请先保存并确认当前素材选择'); return; }
    setBusy('publish');
    try {
      const item = await api<Publication>(workspacePath('/publications', platform), 'POST', { rewrite_id: id, version_id: activeVersion, target_platform: platform,
        asset_ids: draft.asset_ids });
      setPublishOpen(false); message.success('发布稿已准备好'); onPublication(item.id);
    } catch (e) { message.error(errorText(e)); } finally { setBusy(''); }
  }
  // Generated results and reference uploads stay in the library until explicitly
  // adopted or added. Saved versions retain their choices, including deselections.
  const projectLibrary = publicationLibrary.data?.items.filter(item => item.rewrite_id === id && !item.asset.library_deleted_at).map(item => item.asset) || [];
  const publicationIDs = new Set([...(detail?.note.asset_ids || []), ...versions.flatMap(v => v.asset_ids || []), ...libraryAssets.map(a => a.id), ...projectLibrary.map(a => a.id), ...draft.asset_ids]);
  const media = [...new Map([...(detail?.assets || []), ...libraryAssets, ...projectLibrary].map(asset => [asset.id, asset])).values()]
    .filter(a => publicationIDs.has(a.id) && a.kind !== 'style_reference' && (a.mime?.startsWith('image/') || a.mime?.startsWith('video/')));
  return <Spin spinning={result.loading}>
    <Failure error={result.error} retry={() => void result.reload()} />
    {detail && <>
      {awaitingCount !== undefined && <Alert type="info" showIcon title="正在生成新版本" description="可在后台任务中查看进度。若任务失败，当前编辑内容仍会保留。" closable onClose={() => setAwaitingCount(undefined)} style={{ marginBottom: 16 }} />}
      {generationError && <Alert type="warning" showIcon title={generationError} description="配置完成后，可在后台任务中重试；也可继续手动编辑与保存。" closable onClose={() => setGenerationError('')} style={{ marginBottom: 16 }} />}
      <section className="draft-pane">
        <Tabs className="rewrite-editor-tabs" activeKey={editorTab} onChange={setEditorTab} tabBarGutter={24}
          tabBarExtraContent={<div className="rewrite-tab-actions">{confirmed && !dirty && <Tag color="success">已确认</Tag>}{editorTab === 'editor' && textDirty && <Tag color="warning">文案有未保存编辑</Tag>}{editorTab === 'media' && mediaDirty && <Tag color="warning">素材有未保存编辑</Tag>}
            <Button aria-label="刷新仿写项目" icon={<ReloadOutlined />} onClick={() => void result.reload()} /></div>} items={[
          { key: 'editor', label: '文案编辑', children: <div className="rewrite-editing-surface">
            <section className="copy-note-card" aria-label="笔记文案">
            <header className="copy-note-heading"><h2>笔记文案</h2></header>
            <div className="copy-note-fields">
            <Field label="标题"><Input aria-label="仿写标题" variant="borderless" value={draft.title} onChange={e => setDraft(d => ({ ...d, title: e.target.value }))} status={titleTooLong ? 'error' : undefined}
              count={{ show: true, max: titleLimit, strategy: characterCount, exceedFormatter: (value, { max }) => truncateCharacters(value, max) }} />
              {titleTooLong && <Typography.Text type="danger">{titleError}</Typography.Text>}</Field>
            <Field label="正文"><Input.TextArea aria-label="仿写正文" variant="borderless" value={draft.body} onChange={e => setDraft(d => ({ ...d, body: e.target.value }))} autoSize={{ minRows: 8, maxRows: 24 }} showCount /></Field>
            <Field label="话题"><Select mode="tags" aria-label="仿写话题" style={{ width: '100%' }} value={draft.topics} onChange={v => setDraft(d => ({ ...d, topics: v }))} tokenSeparators={[' ', '，', ',']} placeholder="输入话题并回车" /></Field>
            </div>
            {editorTab === 'editor' && <div className="editor-footer copy-note-footer"><Typography.Text type="secondary">{current ? `当前版本 ${current.number} · ${formatDate(current.created_at)}` : '编辑后保存你的第一个版本'}</Typography.Text>
              <Space wrap><Button aria-label="保存新版本" icon={<EditOutlined />} loading={busy === 'save'} onClick={() => void save()}>保存新版本</Button>
                <Button aria-label="确认此版本" icon={<CheckCircleOutlined />} loading={busy === 'confirm'} disabled={!current || dirty || confirmed || titleTooLong} onClick={() => void confirm()}>确认此版本</Button></Space></div>}
            </section>
            {editorTab === 'editor' && <RewriteCopyComposer options={options} onOptionsChange={setOptions} displayModel={displayModel} onModelChange={setDisplayModel}
              versions={versions} activeVersion={activeVersion} confirmedVersion={rewrite?.confirmed_version_id} onVersionChange={switchVersion}
              onGenerate={() => void generate()} generating={busy === 'generate'} pending={awaitingCount !== undefined} disabled={!!busy} />}
          </div> },
          ...(platform === 'xiaohongshu' && current ? [{ key: 'images', label: '图片仿写', children: <div className="rewrite-editing-surface"><ImageRewriteWorkspace rewrite={detail.rewrite} version={current} assets={detail.assets} jobs={jobs} onUpdated={() => { void result.reload(true); refreshList(); }} onApplied={(v, sources, assetID) => { setDraft(d => applyImageToDraft(d, current, v, sources, assetID)); setLocalVersion(v); setActiveVersion(v.id); }} /></div> }] : []),
          { key: 'video', label: '视频仿写', children: <div className="page-stack rewrite-editing-surface">
            <VideoRewriteWorkspace rewrite={detail.rewrite} version={current} assets={detail.assets} projects={detail.videos || []} jobs={jobs} visible={editorTab === 'video'} mediaStatus={detail.note.media_status}
              onUpdated={() => { void result.reload(true); refreshList(); }} onApplied={(next, sources, assetID) => { if (current) setDraft(previous => applyVideoToDraft(previous, current, next, sources, assetID)); setLocalVersion(next); setActiveVersion(next.id); }} />

          </div> },
          { key: 'library', label: '素材库', children: <ProjectMediaLibrary rewriteID={id} visible={editorTab === 'library'} selectedIDs={draft.asset_ids}
            onUpdated={() => void result.reload(true)} onAdd={asset => { setLibraryAssets(items => items.some(item => item.id === asset.id) ? items : [...items, asset]); setDraft(d => ({ ...d, asset_ids: [...new Set([...d.asset_ids, asset.id])] })); }} /> },
          { key: 'media', label: `发布素材 (${draft.asset_ids.length})`, children: <>
            <Card size="small" className="publication-copy-preview" title="发布文案">
              <Typography.Title level={5}>{draft.title || '尚未填写标题'}</Typography.Title>
              <Typography.Paragraph className="preserve publication-copy-body">{draft.body || '尚未填写正文'}</Typography.Paragraph>
              <Space wrap>{draft.topics.map(topic => <Tag key={topic}>#{topic.replace(/^#/, '')}</Tag>)}</Space>
            </Card>
            <div className="row-between publication-media-toolbar"><Typography.Text type="secondary">勾选需要发布的素材，直接拖拽已选卡片调整发布顺序。</Typography.Text>
              <Upload accept="image/*,video/*" beforeUpload={file => addFile(file)} showUploadList={false}><Button icon={<UploadOutlined />} loading={busy === 'upload'}>上传素材</Button></Upload></div>
            <Failure error={publicationLibrary.error} retry={() => void publicationLibrary.reload()} />
            <MediaSelectionGrid key={activeVersion} assets={media} selectedIDs={draft.asset_ids}
              disabled={!active || editorTab !== 'media' || !!busy} onChange={update => setDraft(d => ({ ...d, asset_ids: update(d.asset_ids) }))} />
            {!media.length && <Nothing description="上传或采集媒体素材" />}
            <div className="publication-material-actions"><Button aria-label="创建发布稿" type="primary" icon={<FileDoneOutlined />} disabled={!confirmed || dirty || titleTooLong || !!busy} onClick={() => setPublishOpen(true)}>创建发布稿</Button></div>
          </> },
        ]} />
      </section>
      {editorTab === 'media' && <div className="editor-footer"><Typography.Text type="secondary">{current ? `当前版本 ${current.number} · ${formatDate(current.created_at)}` : '编辑后保存你的第一个版本'}</Typography.Text>
        <Space wrap><Button aria-label="保存新版本" icon={<EditOutlined />} loading={busy === 'save'} onClick={() => void save()}>保存新版本</Button>
          <Button aria-label="确认此版本" icon={<CheckCircleOutlined />} loading={busy === 'confirm'} disabled={!current || dirty || confirmed || titleTooLong} onClick={() => void confirm()}>确认此版本</Button></Space></div>}
      <Modal title={`创建${platformNames[platform]}发布稿`} open={active && publishOpen} rootClassName={!active || !publishOpen ? 'inactive-workspace-portal' : undefined} onCancel={() => setPublishOpen(false)} onOk={() => void publish()} confirmLoading={busy === 'publish'} okText="创建发布稿">
        <Typography.Paragraph>发布稿会保存确认版本的内容快照，后续仿写编辑不会改变它。</Typography.Paragraph>
        <Typography.Paragraph type="secondary">目标平台：{platformNames[platform]}。发布稿保存在当前工作区。</Typography.Paragraph>
        <Typography.Paragraph>当前选定素材：{draft.asset_ids.length} 个</Typography.Paragraph>
        {!draft.asset_ids.length && <Alert type="info" title="当前没有选择媒体，发布稿将只有文案，可先返回上传素材。" style={{ marginTop: 12 }} />}
      </Modal>
      <Modal title="放弃尚未保存的编辑？" open={active && !!pendingVersion} rootClassName={!active || !pendingVersion ? 'inactive-workspace-portal' : undefined} onCancel={() => setPendingVersion('')} okText="切换版本"
        onOk={() => { const next = versions.find(v => v.id === pendingVersion); if (next) { setActiveVersion(next.id); setDraft(toDraft(next)); } setPendingVersion(''); }}>
        切换版本会显示已保存的内容。
      </Modal>
    </>}
  </Spin>;
}
