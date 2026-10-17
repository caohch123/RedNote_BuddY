import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Button, Card, Dropdown } from 'antd';
import { DeleteOutlined, EllipsisOutlined, PictureOutlined, UploadOutlined, VideoCameraOutlined } from '@ant-design/icons';

type Props = { title: string; kind: 'image' | 'video'; preview?: ReactNode; badge?: ReactNode; meta?: string;
  active: boolean; disabled?: boolean; primary?: boolean; onChoose: () => void; onRemove: () => void;
  onUpload: (file: File) => Promise<unknown> };

export default function ReferenceSlotCard({ title, kind, preview, badge, meta, active, disabled = false, primary, onChoose, onRemove, onUpload }: Props) {
  const [open, setOpen] = useState(false); const [uploading, setUploading] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const blocked = disabled || uploading || !active;
  useEffect(() => { if (blocked) setOpen(false); }, [blocked]);
  const icon = kind === 'image' ? <PictureOutlined aria-hidden /> : <VideoCameraOutlined aria-hidden />;
  return <Card size="small" className={`reference-slot-card${preview ? ' has-media' : ''}${primary ? ' is-primary' : ''}`} data-reference-title={title}>
    {preview ? <div className="reference-slot-filled"><div className="reference-slot-preview">{preview}</div><span className="reference-slot-label">{title}</span></div>
      : <button type="button" className="reference-slot-empty" aria-label={`选择${title}`} disabled={blocked} onClick={onChoose}>{icon}<span className="reference-slot-label">{title}</span></button>}
    {badge && <div className="reference-slot-badge">{badge}</div>}
    {meta && <span className="reference-slot-meta">{meta}</span>}
    <Dropdown trigger={['click']} placement="bottomRight" open={active && open} onOpenChange={setOpen} classNames={{ root: active ? 'reference-slot-menu' : 'reference-slot-menu inactive-workspace-portal' }}
      menu={{ items: [
        { key: 'choose', label: preview ? '更换' : '选择', icon, disabled: blocked },
        { key: 'upload', label: kind === 'image' ? '上传图片' : '上传视频', icon: <UploadOutlined aria-hidden />, disabled: blocked },
        ...(preview ? [{ key: 'remove', label: '移除', icon: <DeleteOutlined aria-hidden />, disabled: blocked }] : []),
      ], onClick: ({ key }) => {
        if (blocked) return; setOpen(false);
        if (key === 'choose') onChoose();
        else if (key === 'upload') input.current?.click();
        else if (key === 'remove') onRemove();
      } }}>
      <Button type="text" shape="circle" className="reference-slot-menu-button" aria-label={`${title}菜单`} aria-haspopup="menu" aria-expanded={active && open} disabled={blocked} loading={uploading} icon={<EllipsisOutlined />} />
    </Dropdown>
    <input ref={input} hidden type="file" accept={kind === 'image' ? 'image/*' : 'video/mp4,video/quicktime'} onChange={async event => {
      const file = event.target.files?.[0]; event.target.value = ''; if (!file || blocked) return;
      setUploading(true); try { await onUpload(file); } finally { setUploading(false); }
    }} />
  </Card>;
}
