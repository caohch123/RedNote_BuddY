import { Alert, Button, Empty, Image, Space, Tag, Typography } from 'antd';
import { useEffect, useRef, useState } from 'react';
import { FileImageOutlined, ReloadOutlined } from '@ant-design/icons';
import { assetURL } from '../api';
import type { Asset } from '../types';
import { statusLabels } from '../utils';
import { useWorkspace } from '../workspace';

export function Status({ value, label }: { value?: string; label?: string }) {
  const color = ['succeeded', 'success', 'ready', 'completed', 'published'].includes(value || '') ? 'success'
    : ['failed', 'publish_failed', 'submission_unknown', 'rejected'].includes(value || '') ? 'error'
      : ['running', 'processing', 'downloading', 'queued', 'publishing', 'preparing_publish', 'submitted', 'reviewing'].includes(value || '') ? 'processing'
        : ['partial', 'paused', 'blocked', 'not_configured', 'waiting_config', 'stale', 'awaiting_confirmation', 'verification_required'].includes(value || '') ? 'warning' : 'default';
  const publishingLabel = value === 'publishing' ? '发布中' : value === 'publish_failed' ? '发布失败' : undefined;
  return <Tag color={color}>{label || publishingLabel || statusLabels[value || ''] || value || '—'}</Tag>;
}

export function Failure({ error, retry }: { error: string; retry?: () => void }) {
  return error ? <Alert type="error" showIcon title={error} className="resource-error"
    action={retry && <Button size="small" onClick={retry} icon={<ReloadOutlined />}>重试</Button>} /> : null;
}

export function Nothing({ description = '暂无内容', children }: { description?: string; children?: React.ReactNode }) {
  return <div className="empty-panel"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={description}>{children}</Empty></div>;
}

export function Media({ asset, compact = false }: { asset: Asset; compact?: boolean }) {
  const { platform, active } = useWorkspace();
  const [preview, setPreview] = useState(false);
  const media = useRef<HTMLMediaElement | null>(null);
  const intrinsicRatio = compact && asset.width > 0 && asset.height > 0 ? { aspectRatio: `auto ${asset.width} / ${asset.height}` } : undefined;
  useEffect(() => { if (!active) media.current?.pause(); }, [active]);
  if (asset.status !== 'ready' && asset.status !== 'succeeded' && asset.status !== 'completed' && asset.status !== 'success') {
    return <div className="missing-media"><FileImageOutlined /><Status value={asset.status} />
      {!compact && asset.error && <Typography.Text type="danger">{asset.error}</Typography.Text>}</div>;
  }
  if (asset.kind === 'video' || asset.mime?.startsWith('video/')) {
    return <video ref={node => { media.current = node; }} src={assetURL(asset.id, platform)} controls playsInline preload="metadata" style={intrinsicRatio} className={compact ? 'compact-video' : 'media-video'} />;
  }
  if (asset.kind === 'audio' || asset.mime?.startsWith('audio/')) return <audio ref={node => { media.current = node; }} src={assetURL(asset.id, platform)} controls preload="metadata" />;
  if (asset.kind === 'image' || asset.kind === 'cover' || asset.mime?.startsWith('image/')) {
    return <Image src={assetURL(asset.id, platform)} alt={`素材 ${asset.position + 1}`} loading={compact ? 'lazy' : undefined} style={intrinsicRatio} classNames={{ popup: { root: active ? undefined : 'inactive-workspace-portal' } }} preview={{ open: active && preview, onOpenChange: value => { if (active) setPreview(value); } }} className={compact ? 'compact-image' : 'media-image'} />;
  }
  return <Space><FileImageOutlined /><Button href={assetURL(asset.id, platform)} download>下载文件</Button></Space>;
}

export function Field({ label, children, hint }: { label: string; children: React.ReactNode; hint?: string }) {
  return <div className="field"><label>{label}</label>{children}{hint && <div className="field-hint">{hint}</div>}</div>;
}
