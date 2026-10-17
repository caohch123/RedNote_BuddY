import { useRef, useState } from 'react';
import { Alert, Button, Dropdown, Input, Modal, Select, Spin, Upload, App } from 'antd';
import { DeleteOutlined, DownloadOutlined, EllipsisOutlined, PlayCircleFilled, PlusOutlined, ReloadOutlined, UploadOutlined } from '@ant-design/icons';
import { api, assetURL, upload, errorText } from '../api';
import { workspacePath } from '../platform';
import { useWorkspace } from '../workspace';
import { useResource } from '../hooks';
import type { Asset, LibraryMedia, List } from '../types';
import { Failure, Media, Nothing } from '../components/Common';

const origins = { image_rewrite: '图片仿写', video_rewrite: '视频仿写', video_image: '视频配图', video_render: '视频成片', upload: '用户上传' };

export default function ProjectMediaLibrary({ rewriteID, visible, selectedIDs, onAdd, onUpdated }: { rewriteID: string; visible: boolean; selectedIDs: string[]; onAdd: (asset: Asset) => void; onUpdated: () => void }) {
  const workspace = useWorkspace();
  const { platform } = workspace;
  const active = workspace.active && visible;
  const resource = useResource<List<LibraryMedia>>(active ? workspacePath(`/media-library?rewrite_id=${encodeURIComponent(rewriteID)}`, platform) : null, active ? 5000 : 0);
  const [search, setSearch] = useState(''); const [kind, setKind] = useState('');
  const [origin, setOrigin] = useState(''); const [uploading, setUploading] = useState(false); const { message } = App.useApp();
  const [preview, setPreview] = useState<LibraryMedia>();
  const [menuID, setMenuID] = useState('');
  const [deleting, setDeleting] = useState<LibraryMedia>(); const [deleteBusy, setDeleteBusy] = useState(false); const [deleteError, setDeleteError] = useState('');
  const deleteInFlight = useRef(false); const [removedIDs, setRemovedIDs] = useState<string[]>([]);
  const all = resource.data?.items.filter(item => item.rewrite_id === rewriteID && !removedIDs.includes(item.id) && !item.asset.library_deleted_at) || [];
  const items = all.filter(item => (!origin || item.origin === origin) && (!kind || item.asset.mime.startsWith(`${kind}/`)) && `${item.asset.name || ''} ${item.asset.extraction?.text || ''} ${item.project_title} ${origins[item.origin]} ${item.model || ''}`.toLowerCase().includes(search.trim().toLowerCase()));
  async function remove() {
    if (!deleting || deleteInFlight.current) return;
    deleteInFlight.current = true; setDeleteBusy(true); setDeleteError('');
    try {
      await api(workspacePath(`/rewrites/${encodeURIComponent(rewriteID)}/media-library/${encodeURIComponent(deleting.id)}`, platform), 'DELETE', { confirm: true });
      setRemovedIDs(ids => [...ids, deleting.id]); if (preview?.id === deleting.id) setPreview(undefined);
      setDeleting(undefined); message.success('已从素材库删除'); onUpdated(); await resource.reload(true);
    } catch (e) { setDeleteError(errorText(e)); } finally { deleteInFlight.current = false; setDeleteBusy(false); }
  }
  return <div className="page-stack project-media-library" data-rewrite-id={rewriteID}>
    <div className="library-toolbar"><Input.Search aria-label="搜索素材库" placeholder="搜索素材" allowClear value={search} onChange={e => setSearch(e.target.value)} />
      <Select aria-label="素材类型" virtual={false} value={kind} onChange={setKind} options={[{ value: '', label: '全部类型' }, { value: 'image', label: '图片' }, { value: 'video', label: '视频' }]} />
      <Select aria-label="素材来源" virtual={false} value={origin} onChange={setOrigin} options={[{ value: '', label: '全部来源' }, ...Object.entries(origins).map(([value, label]) => ({ value, label }))]} />
      <Upload accept="image/*" showUploadList={false} beforeUpload={async file => { setUploading(true); try { await upload(file, rewriteID, workspacePath('/assets', platform)); onUpdated(); await resource.reload(); message.success('已保存到项目素材库'); } catch (e) { message.error(errorText(e)); } finally { setUploading(false); } return false; }}><Button icon={<UploadOutlined />} loading={uploading}>上传图片</Button></Upload>
      <span className="library-count">{items.length} 个素材</span><Button aria-label="刷新素材库" icon={<ReloadOutlined />} onClick={() => void resource.reload()} /></div>
    <Failure error={resource.error} retry={() => void resource.reload()} />
    <Spin spinning={resource.loading}>{!items.length && !resource.error && <Nothing description={all.length ? '没有符合条件的素材' : '当前项目暂无仿写素材，生成图片或完成视频后将自动保存到这里'} />}
      <div className="library-media-grid">{items.map(item => {
        const isVideo = item.asset.mime.startsWith('video/');
        const name = item.asset.name || item.project_title || (isVideo ? '视频素材' : '图片素材');
        const selected = selectedIDs.includes(item.id);
        const dimensions = { width: item.asset.width || undefined, height: item.asset.height || undefined };
        return <article key={item.id} className="library-media-card" data-asset-id={item.id} data-kind={isVideo ? 'video' : 'image'}>
          <button type="button" className="library-media-preview" aria-label={`预览${name}`} onClick={() => { setMenuID(''); setPreview(item); }}>
            {isVideo ? <><video {...dimensions} src={assetURL(item.preview_asset_id || item.id, platform)} poster={item.cover_asset_id ? assetURL(item.cover_asset_id, platform) : undefined} preload="metadata" muted playsInline /><PlayCircleFilled className="library-video-play" /></>
              : <img {...dimensions} src={assetURL(item.id, platform)} alt={name} loading="lazy" />}
          </button>
          <Dropdown trigger={['click']} placement="bottomRight" open={active && menuID === item.id} onOpenChange={open => setMenuID(open ? item.id : '')}
            classNames={{ root: active ? 'library-media-menu' : 'library-media-menu inactive-workspace-portal' }}
            menu={{ items: [
              { key: 'add', label: selected ? '已加入发布素材' : '加入发布素材', icon: <PlusOutlined aria-hidden />, disabled: selected },
              { key: 'download', label: <a href={assetURL(item.id, platform)} download aria-label="下载素材">下载素材</a>, icon: <DownloadOutlined /> },
              { type: 'divider' },
              { key: 'delete', label: '删除素材', icon: <DeleteOutlined aria-hidden />, danger: true },
            ], onClick: ({ key }) => {
              if (!active) return;
              setMenuID('');
              if (key === 'add' && !selected) { onAdd(item.asset); message.success('已加入发布素材，保存版本后生效'); }
              if (key === 'delete') { setDeleting(item); setDeleteError(''); }
            } }}>
            <Button type="text" shape="circle" className="library-media-menu-trigger" aria-label={`素材操作：${name}`} aria-haspopup="menu" aria-expanded={active && menuID === item.id} icon={<EllipsisOutlined />} />
          </Dropdown>
        </article>;
      })}</div>
    </Spin>
    <Modal title={preview?.project_title || '素材预览'} open={active && !!preview} rootClassName={!active ? 'inactive-workspace-portal' : undefined} width={850} onCancel={() => setPreview(undefined)}
      footer={<Button aria-label="关闭" onClick={() => setPreview(undefined)}>关闭</Button>}>
      {active && preview && <Media key={preview.id} asset={preview.preview_asset_id ? { ...preview.asset, id: preview.preview_asset_id } : preview.asset} />}
    </Modal>
    <Modal title="删除素材" open={active && !!deleting} onCancel={() => { if (!deleteBusy) setDeleting(undefined); }} onOk={() => void remove()} okText="确认删除" cancelText="取消" confirmLoading={deleteBusy} okButtonProps={{ danger: true }} cancelButtonProps={{ 'aria-label': '取消', disabled: deleteBusy }} closable={!deleteBusy} mask={{ closable: !deleteBusy }}>
      <p>从当前项目素材库删除这份素材？</p><p>已保存的生成历史和发布素材会保留，避免影响已有内容。</p>
      {deleteError && <Alert type="error" title={deleteError} />}
    </Modal>
  </div>;
}
