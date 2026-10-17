import { useEffect, useRef, useState } from 'react';
import { App, Alert, Button, Card, Checkbox, Drawer, Image, Input, Modal, Select, Space, Spin, Table, Tag, Tooltip, Typography } from 'antd';
import type { TableColumnsType } from 'antd';
import { CheckCircleOutlined, CopyOutlined, DeleteOutlined, DownloadOutlined, EditOutlined, ExportOutlined, ReloadOutlined, SendOutlined } from '@ant-design/icons';
import { api, assetURL, download, errorText } from '../api';
import { useResource } from '../hooks';
import type { Asset, AsyncResult, List, Publication, PublishPreview, XHSAccountState, XHSVisibility } from '../types';
import { formatDate, fullCopy } from '../utils';
import { Failure, Field, Media, Nothing, Status } from '../components/Common';
import XHSAccountPanel, { AccountIdentity } from '../components/XHSAccountPanel';
import { creatorURLs, platformNames, validPlatformWorkURL, workspacePath } from '../platform';
import { useWorkspace, WorkspaceContext } from '../workspace';
import DeleteConfirmation from '../components/DeleteConfirmation';
import { deletionSuccess, type DeletionResult } from '../deletion';
import WorkspaceTableScrollbar from '../components/WorkspaceTableScrollbar';

const publicationStates = ['preparing', 'ready', 'exported', 'preparing_publish', 'awaiting_confirmation', 'publishing', 'submitted', 'reviewing', 'rejected', 'publish_failed', 'submission_unknown', 'published'];
const cannotPublish = new Set(['preparing', 'preparing_publish', 'publishing', 'submitted', 'reviewing', 'submission_unknown', 'published', 'rejected']);
const activePublishing = new Set(['preparing_publish', 'publishing', 'submitted', 'reviewing']);
function localDate(value = new Date().toISOString()) {
  return new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })
    .format(new Date(value)).replace(' ', 'T');
}

export default function Publications({ initialID }: { initialID: string }) {
  const [tablePanel, setTablePanel] = useState<HTMLDivElement | null>(null);
  const [tablePage, setTablePage] = useState(1);
  const [tablePageSize, setTablePageSize] = useState(15);
  const { platform: workspacePlatform, active } = useWorkspace();
  const scoped = (path: string) => workspacePath(path, workspacePlatform);
  const { message } = App.useApp(); const result = useResource<List<Publication>>(scoped('/publications'), 2000);
  const [id, setID] = useState(initialID); const [query, setQuery] = useState(''); const [status, setStatus] = useState('');
  const [exporting, setExporting] = useState(''); const [mark, setMark] = useState<Publication>();
  const [deleting, setDeleting] = useState<Publication>();
  const [publishedURL, setPublishedURL] = useState(''); const [publishedAt, setPublishedAt] = useState(localDate()); const [saving, setSaving] = useState(false);
  const [account, setAccount] = useState<XHSAccountState>();
  const [publishItem, setPublishItem] = useState<Publication>(); const [visibility, setVisibility] = useState<XHSVisibility>('public');
  const [preview, setPreview] = useState<PublishPreview>(); const [previewLoading, setPreviewLoading] = useState(false); const [previewError, setPreviewError] = useState('');
  const [confirmed, setConfirmed] = useState(false); const [publishing, setPublishing] = useState(false); const [now, setNow] = useState(Date.now());
  const previewGeneration = useRef(0); const publishInFlight = useRef(false);
  const [imageOpen, setImageOpen] = useState('');
  const previousPublicationIDs = useRef(new Set<string>());
  const imagePreview = (key: string) => ({ open: active && imageOpen === key, onOpenChange: (open: boolean) => { if (active) setImageOpen(open ? key : ''); } });
  const imagePortalClassName = (key: string) => active && imageOpen === key ? '' : 'inactive-workspace-portal';
  const detail = useResource<Publication>(id ? scoped(`/publications/${id}`) : null, 2000);
  const assets = useResource<List<Asset>>(scoped('/assets'), 10000);
  const platformName = platformNames[workspacePlatform];
  useEffect(() => {
    if (!result.data) return;
    const next = new Set(result.data.items.map(item => item.id));
    const removed = new Set([...previousPublicationIDs.current].filter(itemID => !next.has(itemID)));
    previousPublicationIDs.current = next;
    if (id && removed.has(id)) { setID(''); setImageOpen(''); }
    if (mark && removed.has(mark.id)) setMark(undefined);
    if (publishItem && removed.has(publishItem.id)) {
      previewGeneration.current++; setPublishItem(undefined); setPreview(undefined); setConfirmed(false);
      setPreviewLoading(false); setPreviewError(''); setImageOpen('');
    }
    // Only IDs present in the previous complete list are removed. A newly
    // created initialID may be opened before its first list refresh finishes.
  }, [result.data]);
  useEffect(() => { if (initialID) setID(initialID); }, [initialID]);
  useEffect(() => () => { previewGeneration.current++; }, []);
  useEffect(() => { if (!publishItem) return; const timer = setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(timer); }, [publishItem]);
  async function loadPreview(item: Publication, selectedVisibility: XHSVisibility) {
    const current = ++previewGeneration.current; setPreview(undefined); setConfirmed(false); setPreviewError(''); setPreviewLoading(true);
    try {
      const value = await api<PublishPreview>(scoped(`/publications/${item.id}/publish-preview`), 'POST', { visibility: selectedVisibility });
      if (current === previewGeneration.current) { setPreview(value); setNow(Date.now()); }
    } catch (e) { if (current === previewGeneration.current) { setPreview(undefined); setPreviewError(errorText(e)); } }
    finally { if (current === previewGeneration.current) setPreviewLoading(false); }
  }
  function openPublish(item: Publication) {
    if (!active) return;
    setPublishItem(item); const selectedVisibility = item.visibility || 'public'; setVisibility(selectedVisibility);
    void loadPreview(item, selectedVisibility);
  }
  function closePublish() { if (publishInFlight.current) return; previewGeneration.current++; setPublishItem(undefined); setPreview(undefined); setConfirmed(false); }
  const previewExpired = !preview || !Number.isFinite(Date.parse(preview.expires_at)) || Date.parse(preview.expires_at) <= now;
  const previewMatchesAccount = !!preview && account?.status === 'logged_in' && account.account?.id === preview.account.id;
  const canConfirmPublish = active && !!publishItem && !!preview && confirmed && !previewExpired && previewMatchesAccount && !previewLoading && !publishing;
  function canPublish(item: Publication): boolean {
    if (cannotPublish.has(item.status)) return false;
    return account?.status === 'logged_in';
  }
  function publishReason(item: Publication): string | undefined {
    if (canPublish(item)) return undefined;
    if (cannotPublish.has(item.status)) return item.status === 'submission_unknown' ? '先到目标平台核对结果，避免重复发布' : item.status === 'rejected' ? '作品审核未通过，请修改内容并创建新的发布稿' : '当前稿件状态暂不能重新发布';
    return '请先扫码登录本人小红书账号';
  }
  function publishLabel(): string { return `发布到${platformName}`; }
  async function publishConfirmed() {
    if (!canConfirmPublish || !preview || !publishItem || publishInFlight.current) return;
    publishInFlight.current = true; setPublishing(true);
    try {
      const value = await api<AsyncResult>(scoped(`/publications/${publishItem.id}/publish`), 'POST', { confirmation_token: preview.confirmation_token });
      if (!value.job_id) throw new Error('服务未返回发布任务，请刷新发布状态后核对结果');
      message.success(`发布任务已提交，正在等待${platformName}结果`); setPublishItem(undefined); setPreview(undefined); setConfirmed(false);
      void result.reload(true); void detail.reload(true);
    } catch (e) { setPreviewError(errorText(e)); setConfirmed(false); setPreview(undefined); void result.reload(true); void detail.reload(true); }
    finally { publishInFlight.current = false; setPublishing(false); }
  }
  async function copy(text: string) {
    try { await navigator.clipboard.writeText(text); message.success('已复制到剪贴板'); }
    catch { message.error('浏览器未允许复制，请从文案详情中手动复制'); }
  }
  async function exportPackage(item: Publication) {
    setExporting(item.id);
    try { await download(scoped(`/publications/${item.id}/export`), `发布包-${item.title || item.id}.zip`, true);
      message.success(`发布包已导出，请在${platformName}完成实际发布`); void result.reload(true); void detail.reload(true);
    } catch (e) { message.error(errorText(e)); } finally { setExporting(''); }
  }
  function markOpen(item: Publication) { setMark(item); setPublishedURL(item.published_url || ''); setPublishedAt(localDate(item.published_at || undefined)); }
  async function markPublished() {
    if (!active || !mark) return;
    let url: URL;
    try { url = new URL(publishedURL.trim()); if (!validPlatformWorkURL(url.href)) throw new Error(); }
    catch { message.warning(`请填写有效的${platformName}已发布作品链接（HTTPS）`); return; }
    if (!publishedAt || Number.isNaN(new Date(`${publishedAt}:00+08:00`).getTime())) { message.warning('请填写发布时间'); return; }
    setSaving(true);
    try { await api(scoped(`/publications/${mark.id}`), 'PATCH', { status: 'published', published_url: url.href,
      published_at: new Date(`${publishedAt}:00+08:00`).toISOString() });
      message.success('已记录发布结果'); setMark(undefined); void result.reload(true); void detail.reload(true);
    } catch (e) { message.error(errorText(e)); } finally { setSaving(false); }
  }
  async function removePublication() {
    if (!deleting) return;
    const removed = await api<DeletionResult>(scoped(`/publications/${encodeURIComponent(deleting.id)}`), 'DELETE', { confirm: true });
    if (id === deleting.id) { setID(''); setImageOpen(''); }
    if (mark?.id === deleting.id) setMark(undefined);
    if (publishItem?.id === deleting.id) closePublish();
    message.success(deletionSuccess(removed, '本地发布稿已删除')); void result.reload(true); void assets.reload(true);
  }
  const deletionBusy = (item: Publication) => ['preparing_publish', 'publishing', 'submission_unknown'].includes(item.status) || exporting === item.id || saving && mark?.id === item.id;
  const columns: TableColumnsType<Publication> = [
    { title: '发布稿', key: 'title', width: 300, render: (_, p) => <button className="note-title link-button" onClick={() => setID(p.id)}>{p.title}<span className="note-type">{p.video_project_id ? '视频成片' : '图文 / 素材'} · {p.asset_ids?.length || 0} 个素材</span></button> },
    { title: '状态', key: 'status', width: 170, render: (_, p) => <><Status value={p.status} label={p.status === 'ready' ? '可发布' : p.status === 'submission_unknown' ? '结果待核实' : undefined} />
      {p.publish_account_name && <div className="field-hint">{p.publish_account_name}</div>}
      {p.publish_error && <div className="publish-error-text">{p.publish_error}</div>}</> },
    { title: '创建时间', key: 'created_at', width: 170, render: (_, p) => formatDate(p.created_at) },
    { title: '发布时间', key: 'published_at', width: 170, render: (_, p) => formatDate(p.published_at) },
    { title: '操作', key: 'actions', width: 220, render: (_, p) => <Space wrap size={4}>
      <Tooltip title={!canPublish(p) ? publishReason(p) : publishLabel()}><span><Button aria-label={publishLabel()} size="small" type="text" icon={<SendOutlined />} disabled={!canPublish(p)} onClick={() => openPublish(p)} /></span></Tooltip>
      <Tooltip title="复制文案"><Button aria-label="复制文案" size="small" type="text" icon={<CopyOutlined />} onClick={() => void copy(fullCopy(p))} /></Tooltip>
      <Tooltip title="导出发布包"><span><Button aria-label="导出发布包" size="small" type="text" icon={<DownloadOutlined />} loading={exporting === p.id} disabled={activePublishing.has(p.status)} onClick={() => void exportPackage(p)} /></span></Tooltip>
      <Tooltip title={p.status === 'published' ? '编辑发布记录' : '记录已发布'}><span><Button aria-label={p.status === 'published' ? '编辑发布记录' : '记录已发布'} size="small" type="text" icon={p.status === 'published' ? <EditOutlined /> : <CheckCircleOutlined />} disabled={activePublishing.has(p.status)} onClick={() => markOpen(p)} /></span></Tooltip>
      <Tooltip title="删除发布稿"><span><Button aria-label="删除发布稿" size="small" type="text" icon={<DeleteOutlined />} disabled={deletionBusy(p)} onClick={() => setDeleting(p)} /></span></Tooltip></Space> },
  ];
  const items = (result.data?.items || []).filter(p => (!status || p.status === status) && [p.title, p.body].join(' ').includes(query));
  const detailState = detail.data?.status;
  const detailDescription = detailState === 'submission_unknown' ? '提交后未能确认平台结果，已停止重复发布。请先到小红书创作平台查看笔记；确认成功后手动记录作品链接。'
    : detailState === 'publish_failed' || detailState === 'rejected' ? detail.data?.publish_error || '发布没有成功，请检查账号和素材后重新确认。'
      : detailState === 'preparing_publish' ? '正在上传素材并填写官方编辑器；准备完成后仍须核对并确认，才会正式提交。'
        : detailState === 'awaiting_confirmation' ? '官方编辑器已准备好，请核对账号、内容、素材顺序和可见性后确认发布。'
          : detailState === 'submitted' || detailState === 'reviewing' ? '平台已接收作品，正在等待审核结果；此状态不会计为已发布。'
            : detailState === 'publishing' ? '等待平台返回结果，关闭页面不会中断后台任务。'
              : '可使用本人账号扫码登录，核对账号、文案、素材和可见性后确认发布；也可继续导出发布包并手动发布。';
  return <div className="page-stack publication-page">
    <XHSAccountPanel onChange={setAccount} />
    <div className="publication-overview">{[['preparing', '待准备'], ['ready', '可发布'], ['exported', '已导出'], ['published', '已发布']].map(([value, label]) => <button key={value}
      onClick={() => setStatus(status === value ? '' : value)} className={`overview-card ${status === value ? 'active' : ''}`}>
      <span>{label}</span><strong>{(result.data?.items || []).filter(p => p.status === value).length}</strong></button>)}</div>
    <div className="data-panel publication-table-panel workspace-table-panel" ref={setTablePanel}><div className="toolbar"><Space wrap>
      <Input.Search allowClear placeholder="搜索发布标题或正文" onSearch={setQuery} style={{ width: 285, maxWidth: '100%' }} />
      <Select allowClear value={status || undefined} onChange={v => setStatus(v || '')} placeholder="发布状态" style={{ width: 150 }}
        options={publicationStates.map(v => ({ value: v, label: <Status value={v} label={v === 'ready' ? '可发布' : undefined} /> }))} />
    </Space><Space wrap><Button icon={<ExportOutlined />} href={creatorURLs[workspacePlatform]} target="_blank" rel="noreferrer">打开{platformName}创作平台</Button>
      <Button icon={<ReloadOutlined />} onClick={() => void result.reload()} /></Space></div>
      <Failure error={result.error} retry={() => void result.reload()} />
      <Table key={active ? 'visible' : 'hidden'} rowKey="id" columns={columns} dataSource={items} loading={result.loading} scroll={{ x: 1200 }}
        pagination={{ current: tablePage, pageSize: tablePageSize, showSizeChanger: true, onChange: (page, size) => { setTablePage(page); setTablePageSize(size); } }}
        sticky={{ getContainer: () => tablePanel?.closest<HTMLElement>('.main-content') || window }}
        locale={{ emptyText: <Nothing description="确认一个仿写版本，创建你的第一篇发布稿" /> }} />
    </div>
    <WorkspaceTableScrollbar panel={tablePanel} label="发布表格横向滚动条" />
    <Drawer rootClassName={active && id ? '' : 'inactive-workspace-portal'} open={active && !!id} onClose={() => setID('')} title="发布稿详情" size={850}
      extra={detail.data && <Space wrap><Button danger icon={<DeleteOutlined />} disabled={deletionBusy(detail.data)} onClick={() => setDeleting(detail.data)}>删除发布稿</Button><Button type="primary" icon={<DownloadOutlined />} loading={exporting === id} disabled={activePublishing.has(detail.data.status)} onClick={() => void exportPackage(detail.data!)}>导出发布包</Button></Space>}>
      <WorkspaceContext.Provider value={{ platform: workspacePlatform, active: active && !!id }}>
      <Failure error={detail.error} retry={() => void detail.reload()} />
      {detail.data && <><Alert type={detailState === 'submission_unknown' ? 'warning' : ['publish_failed', 'rejected'].includes(detailState || '') ? 'error' : 'info'} showIcon
        title={detailState === 'submission_unknown' ? '发布结果待核实' : detailState === 'rejected' ? '审核未通过' : detailState === 'publish_failed' ? '发布失败' : detailState === 'preparing_publish' ? '正在准备官方编辑器' : detailState === 'awaiting_confirmation' ? '等待本人确认发布' : detailState === 'submitted' || detailState === 'reviewing' ? '作品已提交，等待审核' : detailState === 'publishing' ? `正在发布到${platformName}` : '扫码登录后发布'}
        description={detailDescription} style={{ marginBottom: 18 }} />
        <Tag>{platformName}发布稿</Tag>
        <div className="row-between"><Status value={detail.data.status} label={detail.data.status === 'ready' ? '可发布' : detail.data.status === 'submission_unknown' ? '结果待核实' : undefined} /><Space><Button icon={<CopyOutlined />} onClick={() => void copy(detail.data!.title)}>复制标题</Button>
          <Button icon={<CopyOutlined />} onClick={() => void copy(fullCopy(detail.data!))}>复制完整文案</Button></Space></div>
        <Typography.Title level={4}>{detail.data.title}</Typography.Title><Typography.Paragraph className="preserve original-body" copyable>{detail.data.body}</Typography.Paragraph>
        <Space wrap>{detail.data.topics?.map(t => <Tag key={t}>#{t.replace(/^#/, '')}</Tag>)}</Space>
        <Typography.Title level={5} style={{ marginTop: 24 }}>发布素材</Typography.Title>
        <div className="publication-media">{detail.data.asset_ids?.map((assetID, i) => <Card size="small" key={assetID} title={`素材 ${i + 1}`}
          extra={<Button size="small" href={assetURL(assetID, workspacePlatform)} download icon={<DownloadOutlined />}>下载</Button>}>
          {assets.data?.items?.find(a => a.id === assetID) ? <Media asset={assets.data.items.find(a => a.id === assetID)!} />
            : detail.data?.video_project_id && i === 0 ? <video controls preload="metadata" src={assetURL(assetID, workspacePlatform)} className="media-video" />
            : <Image classNames={{ popup: { root: imagePortalClassName(`detail-${assetID}`) } }} preview={imagePreview(`detail-${assetID}`)} src={assetURL(assetID, workspacePlatform)} alt={`发布素材 ${i + 1}`} style={{ maxHeight: 300, objectFit: 'contain' }} />}
        </Card>)}</div>
        {!detail.data.asset_ids?.length && <Nothing description="这篇发布稿只有文案" />}
        <Space wrap style={{ marginTop: 24 }}><Button aria-label={publishLabel()} title={publishReason(detail.data)} type="primary" icon={<SendOutlined />} disabled={!canPublish(detail.data)} onClick={() => openPublish(detail.data!)}>{publishLabel()}</Button>
          <Button href={creatorURLs[workspacePlatform]} target="_blank" rel="noreferrer" icon={<ExportOutlined />}>前往创作平台</Button>
          <Button icon={<CheckCircleOutlined />} disabled={activePublishing.has(detail.data.status)} onClick={() => markOpen(detail.data!)}>记录已发布</Button>
          {detail.data.published_url && <Button type="link" href={detail.data.published_url} target="_blank" rel="noreferrer">{detail.data.status === 'published' ? '查看已发布作品' : '查看已提交作品'} ↗</Button>}</Space>
        {detail.data.submitted_at && <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>提交时间：{formatDate(detail.data.submitted_at)}</Typography.Paragraph>}
        {detail.data.published_at && <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>发布时间：{formatDate(detail.data.published_at)}</Typography.Paragraph>}
        {detail.data.publish_account_name && <Typography.Paragraph type="secondary">发布账号：{detail.data.publish_account_name}（{detail.data.publish_account_id}）{detail.data.visibility === 'private' ? ' · 仅自己可见' : detail.data.visibility === 'public' ? ' · 公开' : ''}</Typography.Paragraph>}
        {detail.data.publish_job_id && <Typography.Paragraph type="secondary">发布任务：{detail.data.publish_job_id}</Typography.Paragraph>}
      </>}
      </WorkspaceContext.Provider>
    </Drawer>
    <Modal rootClassName={active && mark ? '' : 'inactive-workspace-portal'} title="记录发布结果" open={active && !!mark} onCancel={() => setMark(undefined)} onOk={() => void markPublished()} confirmLoading={saving} okText="保存发布记录">
      <Typography.Paragraph>完成{platformName}实际发布后，在这里保存作品链接。</Typography.Paragraph>
      <Field label="作品链接"><Input aria-label="已发布作品链接" value={publishedURL} onChange={e => setPublishedURL(e.target.value)} placeholder="https://www.xiaohongshu.com/explore/..." /></Field>
      <Field label="发布时间（北京时间）"><Input aria-label="发布时间" type="datetime-local" value={publishedAt} onChange={e => setPublishedAt(e.target.value)} /></Field>
    </Modal>
    <Modal rootClassName={active && publishItem ? '' : 'inactive-workspace-portal'} title={`确认发布到${platformName}`} open={active && !!publishItem} onCancel={closePublish} onOk={() => void publishConfirmed()} width={850}
      confirmLoading={publishing} okButtonProps={{ disabled: !canConfirmPublish }} okText={`确认发布到${platformName}`} cancelText="取消" closable={!publishing} mask={{ closable: !publishing }} cancelButtonProps={{ disabled: publishing }}>
      <Field label="可见性"><Select aria-label={`${platformName}发布可见性`} value={visibility} disabled={publishing} options={[{ value: 'public', label: '公开' }, { value: 'private', label: '仅自己可见' }]}
        onChange={value => { setVisibility(value); if (publishItem) void loadPreview(publishItem, value); }} style={{ width: 200 }} /></Field>
      {previewLoading && <div className="empty-panel"><Spin description="正在检查发布账号和素材" /></div>}
      {previewError && <Alert type="error" showIcon title={previewError} />}
      {preview && <><div className="publish-preview-account"><AccountIdentity account={preview.account} /><Tag color={preview.visibility === 'private' ? 'default' : 'blue'}>{preview.visibility === 'private' ? '仅自己可见' : '公开发布'}</Tag></div>
        <Typography.Title level={4}>{preview.title}</Typography.Title><Typography.Paragraph className="preserve original-body">{preview.body}</Typography.Paragraph>
        <Space wrap>{preview.topics?.map(topic => <Tag key={topic}>#{topic.replace(/^#/, '')}</Tag>)}</Space>
        <Typography.Title level={5} style={{ marginTop: 20 }}>将按以下顺序发布 {preview.media_type === 'video' ? '视频' : '图片'}</Typography.Title>
        <div className="publication-media publish-preview-media">{preview.assets.map((asset, index) => <Card size="small" key={asset.id} title={`素材 ${index + 1}`}>
          {asset.kind === 'video' || asset.mime.startsWith('video/') ? <video src={assetURL(asset.id, workspacePlatform)} controls preload="metadata" className="media-video" />
            : <Image classNames={{ popup: { root: imagePortalClassName(`publish-${asset.id}`) } }} preview={imagePreview(`publish-${asset.id}`)} src={assetURL(asset.id, workspacePlatform)} alt={`待发布素材 ${index + 1}`} style={{ maxHeight: 250, objectFit: 'contain' }} />}
        </Card>)}</div>
        {previewExpired && <Alert type="warning" showIcon title="发布确认已过期，请重新检查发布内容" style={{ marginTop: 16 }} />}
        {!previewMatchesAccount && <Alert type="warning" showIcon title="当前登录账号发生变化，请重新检查发布账号" style={{ marginTop: 16 }} />}
        <div className="publish-confirm-check"><Checkbox checked={confirmed} disabled={previewExpired || !previewMatchesAccount || publishing} onChange={e => setConfirmed(e.target.checked)}>
          我已核对账号、文案、素材顺序和可见性，确认发布这篇笔记
        </Checkbox><div className="field-hint">确认有效至 {formatDate(preview.expires_at)}（北京时间）；点击确认按钮将实际提交到{platformName}。</div></div>
      </>}
      {!previewLoading && (previewError || previewExpired || !previewMatchesAccount) && <Button icon={<ReloadOutlined />} disabled={publishing} onClick={() => {
        if (publishItem) void loadPreview(publishItem, visibility);
      }} style={{ marginTop: 16 }}>重新检查发布内容</Button>}
    </Modal>
    <DeleteConfirmation open={!!deleting} title="删除发布稿" subject={deleting?.title || '这篇发布稿'}
      consequences={['删除本地发布稿、发布准备与发布任务记录，以及不再被引用的发布素材文件。', '采集记录、仿写项目和视频项目会保留。平台上已经提交或发布的作品不会被删除。']}
      onCancel={() => setDeleting(undefined)} onConfirm={removePublication} />
  </div>;
}
