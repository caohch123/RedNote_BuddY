import { useEffect, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import { Avatar, Button, Image } from 'antd';
import { ExportOutlined, FileImageOutlined, HeartOutlined, LeftOutlined, MessageOutlined, PlayCircleFilled, RightOutlined, StarOutlined, UserOutlined } from '@ant-design/icons';
import { assetURL } from '../api';
import type { Asset, Note, Platform } from '../types';
import { formatDate, number } from '../utils';
import '../styles/collection-note-preview.css';

export interface CollectionNotePreviewProps {
  note: Note; assets?: Asset[]; platform: Platform; active?: boolean; compact?: boolean;
  coverRefresh?: number; fields?: string[];
}

export default function CollectionNotePreview({ note, assets, platform, active = true, compact = false, coverRefresh = 0, fields }: CollectionNotePreviewProps) {
  const [index, setIndex] = useState(0);
  const [preview, setPreview] = useState(false);
  const [failed, setFailed] = useState(false);
  const [enabledMedia, setEnabledMedia] = useState('');
  const [direction, setDirection] = useState<'next' | 'previous'>('next');
  const media = useRef<HTMLMediaElement | null>(null);
  const visualElement = useRef<HTMLDivElement | null>(null);
  const scroll = useRef<HTMLDivElement | null>(null);
  const touch = useRef<{ x: number; y: number } | null>(null);
  const hasVideo = assets?.some(asset => asset.kind === 'video' || asset.mime?.startsWith('video/'));
  const ordered = assets ? [...new Map(assets.map(asset => [asset.id, asset])).values()]
    .filter(asset => ['image', 'cover', 'video', 'audio'].includes(asset.kind) || /^(image|video|audio)\//.test(asset.mime || ''))
    .filter(asset => !(note.type === 'video' && hasVideo && (asset.kind === 'cover' || asset.id === note.cover_asset_id)))
    .sort((a, b) => a.position - b.position || a.id.localeCompare(b.id)) : [];
  const currentIndex = Math.min(index, Math.max(0, ordered.length - 1));
  const item = ordered[currentIndex];
  const coverID = note.cover_asset_id;
  const sourceID = assets ? item?.id : coverID;
  const source = sourceID ? `${assetURL(sourceID, platform)}&cover_refresh=${coverRefresh}` : '';
  const count = assets ? ordered.length : 0;
  const ready = assets ? item && ['ready', 'succeeded', 'completed', 'success'].includes(item.status) : !!coverID;
  const video = !!item && (item.kind === 'video' || item.mime?.startsWith('video/'));
  const audio = !!item && (item.kind === 'audio' || item.mime?.startsWith('audio/'));
  const image = !assets || (!!item && (item.kind === 'image' || item.kind === 'cover' || item.mime?.startsWith('image/')));
  const poster = coverID ? `${assetURL(coverID, platform)}&cover_refresh=${coverRefresh}` : undefined;
  const mediaStarted = !!sourceID && enabledMedia === sourceID;
  const title = note.title || '等待元数据';
  const pause = () => media.current?.pause();
  useEffect(() => {
    const current = media.current;
    setFailed(false); setPreview(false); setEnabledMedia('');
    return () => current?.pause();
  }, [source, item?.status]);
  useEffect(() => { setFailed(false); }, [note.media_status]);
  useEffect(() => { setIndex(0); setPreview(false); setFailed(false); setEnabledMedia(''); scroll.current?.scrollTo({ top: 0 }); }, [note.id, platform]);
  useEffect(() => { if (!active) { pause(); setPreview(false); } }, [active]);
  useEffect(() => {
    const element = visualElement.current;
    if (!element) return;
    const observer = new IntersectionObserver(entries => { if (!entries[0]?.isIntersecting) pause(); }, { threshold: .01 });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    const hidden = () => { if (document.hidden) pause(); };
    document.addEventListener('visibilitychange', hidden);
    return () => document.removeEventListener('visibilitychange', hidden);
  }, []);
  const move = (next: number) => {
    const target = Math.max(0, Math.min(ordered.length - 1, next));
    if (target === currentIndex) return;
    pause(); setPreview(false); setFailed(false); setEnabledMedia(''); setDirection(target > currentIndex ? 'next' : 'previous'); setIndex(target);
  };
  const play = () => {
    const player = media.current;
    if (!active || !player || !sourceID) return;
    const requestedID = sourceID;
    flushSync(() => setEnabledMedia(requestedID));
    void player.play().catch(() => {
      if (media.current === player) setEnabledMedia(current => current === requestedID ? '' : current);
    });
  };
  const metrics = [
    { key: 'likes', label: '点赞', icon: <HeartOutlined aria-hidden="true" />, value: note.likes },
    { key: 'collects', label: '收藏', icon: <StarOutlined aria-hidden="true" />, value: note.collects },
    { key: 'comments', label: '评论', icon: <MessageOutlined aria-hidden="true" />, value: note.comments },
  ].filter(metric => !compact || !fields || fields.includes(metric.key));
  const visual = ready && !failed && source ? <>
    {image && (compact ? <img className="collection-card-image" src={source} alt={title} loading="lazy" decoding="async" draggable={false} onError={() => setFailed(true)} /> :
      <Image key={source} src={source} alt={`笔记素材 ${currentIndex + 1}`} onError={() => setFailed(true)}
        classNames={{ popup: { root: active && preview ? undefined : 'inactive-workspace-portal' } }} preview={{ open: active && preview, onOpenChange: setPreview }} />)}
    {video && <video key={source} ref={node => { media.current = node; }} src={mediaStarted ? source : undefined} poster={poster} controls={mediaStarted} playsInline preload="none" onError={() => setFailed(true)} aria-label={`笔记视频 ${currentIndex + 1}`} />}
    {audio && <audio key={source} ref={node => { media.current = node; }} src={mediaStarted ? source : undefined} controls={mediaStarted} preload="none" onError={() => setFailed(true)} aria-label={`笔记音频 ${currentIndex + 1}`} />}
    {(video || audio) && !mediaStarted && <button type="button" className="collection-device-play-button" aria-label={`播放笔记${video ? '视频' : '音频'} ${currentIndex + 1}`} onClick={play}><PlayCircleFilled className="collection-device-play" aria-hidden="true" /></button>}
    {!image && !video && !audio && <span className="collection-device-empty"><FileImageOutlined aria-hidden="true" />暂无预览</span>}
  </> : <span className="collection-device-empty"><FileImageOutlined aria-hidden="true" />{failed ? '素材暂不可用' : sourceID ? '素材准备中' : '暂无媒体'}</span>;
  return <div className={`collection-device collection-note-preview${compact ? ' is-compact' : ''}`} data-note-id={note.id}>
    <div className="collection-device-screen">
      <div className="collection-device-status" aria-hidden="true"><span>9:41</span><i className="collection-device-speaker" />
        <svg viewBox="0 0 56 16" width="52" height="15"><path d="M2 12h2V8H2zm5 0h2V6H7zm5 0h2V3h-2zm5 0h2V0h-2z" fill="currentColor" /><path d="M24 4q7-6 14 0m-11 3q4-4 8 0m-5 3q1-1 2 0" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" /><rect x="42" y="2" width="11" height="9" rx="2" fill="none" stroke="currentColor" /><rect x="44" y="4" width="7" height="5" rx="1" fill="currentColor" /><path d="M55 5v3" stroke="currentColor" /></svg>
      </div>
      <div className="collection-device-author"><LeftOutlined className="collection-device-back" aria-hidden="true" />
        <Avatar size={30} src={note.author?.avatar_url ? <img src={note.author.avatar_url} alt="" referrerPolicy="no-referrer" /> : undefined} icon={<UserOutlined aria-hidden="true" />} />
        <strong>{note.author?.name || '未知博主'}</strong>
        {note.url && <a href={note.url} aria-label="查看原文" target="_blank" rel="noreferrer" className="collection-device-source"><ExportOutlined aria-hidden="true" /></a>}
      </div>
      <div ref={scroll} className="collection-device-scroll" tabIndex={compact ? undefined : 0} aria-label="笔记屏幕内容">
        <section className="collection-detail-media" aria-label="笔记媒体">
          <div ref={visualElement} className="collection-device-visual" tabIndex={count > 1 ? 0 : undefined} aria-label="笔记媒体翻页"
            onKeyDown={e => {
              if (e.target !== e.currentTarget || e.altKey || e.ctrlKey || e.metaKey) return;
              if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') { e.preventDefault(); move(currentIndex + (e.key === 'ArrowRight' ? 1 : -1)); }
            }}
            onPointerDown={e => { if (compact && image && e.pointerType === 'mouse' && e.button === 0) touch.current = { x: e.clientX, y: e.clientY }; }}
            onPointerUp={e => {
              if (e.pointerType !== 'mouse' || !compact || !image) return;
              const start = touch.current; touch.current = null;
              if (!start) return;
              const dx = e.clientX - start.x; const dy = e.clientY - start.y;
              if (Math.abs(dx) > 45 && Math.abs(dx) > Math.abs(dy) * 1.4) move(currentIndex + (dx < 0 ? 1 : -1));
            }}
            onPointerCancel={() => { touch.current = null; }}
            onTouchStart={e => { const t = e.touches[0]; touch.current = e.touches.length === 1 ? { x: t.clientX, y: t.clientY } : null; }} onTouchEnd={e => {
            const start = touch.current; touch.current = null;
            if (!start || video || audio || preview || !e.changedTouches[0]) return;
            const dx = e.changedTouches[0].clientX - start.x; const dy = e.changedTouches[0].clientY - start.y;
            if (Math.abs(dx) > 45 && Math.abs(dx) > Math.abs(dy) * 1.4) move(currentIndex + (dx < 0 ? 1 : -1));
          }}>
            <div key={source || 'empty'} className="collection-device-slide" data-direction={direction}>{visual}</div>
            {count > 0 && <span className="collection-device-counter" aria-label="素材页码" aria-live="polite">{currentIndex + 1} / {count}</span>}
            {count > 1 && <div className="collection-device-arrows"><Button shape="circle" size="small" aria-label="上一张素材" icon={<LeftOutlined />} disabled={currentIndex === 0} onClick={() => move(currentIndex - 1)} />
              <Button shape="circle" size="small" aria-label="下一张素材" icon={<RightOutlined />} disabled={currentIndex >= count - 1} onClick={() => move(currentIndex + 1)} /></div>}
          </div>
          {count > 1 && <div className="collection-device-dots" aria-label="选择素材">{ordered.map((asset, i) => <button type="button" key={asset.id} className={i === currentIndex ? 'selected' : ''} aria-label={`查看素材 ${i + 1}`} aria-current={i === currentIndex ? 'true' : undefined} onClick={() => move(i)} />)}</div>}
        </section>
        <article className="collection-device-body">
          <h3>{title}</h3>
          <div className="collection-note-body">{note.body || ''}</div>
          {!!note.topics?.length && <div className="collection-topics">{note.topics.map((topic, i) => <span key={`${topic}-${i}`}>#{topic.replace(/^#/, '')}</span>)}</div>}
          {note.published_at && <time className="collection-device-date">{formatDate(note.published_at)}</time>}
        </article>
      </div>
      <div className="collection-engagement" aria-label="原文互动数据">{metrics.map(metric => <div key={metric.key} aria-label={`${metric.label} ${number(metric.value)}`}>{metric.icon}<strong>{number(metric.value)}</strong><span>{metric.label}</span></div>)}</div>
      <div className="collection-device-home" aria-hidden="true"><i /></div>
    </div>
  </div>;
}
