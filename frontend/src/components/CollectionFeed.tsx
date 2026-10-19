import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { Button, Checkbox, Dropdown, Tag } from 'antd';
import { ArrowRightOutlined, CopyOutlined, DeleteOutlined, EllipsisOutlined, ExportOutlined, EyeOutlined } from '@ant-design/icons';
import type { CollectionDetail, Note, Platform } from '../types';
import { formatDate, number, ratio } from '../utils';
import { Status } from './Common';
import CollectionNotePreview from './CollectionNotePreview';
import { useResource } from '../hooks';
import { workspacePath } from '../platform';
import { useWorkspace } from '../workspace';

function MasonryItem({ children }: { children: ReactNode }) {
  const measured = useRef<HTMLDivElement>(null);
  const [span, setSpan] = useState(440);
  useLayoutEffect(() => {
    const node = measured.current;
    if (!node) return;
    const update = () => {
      const height = node.getBoundingClientRect().height;
      if (height > 0) setSpan(Math.ceil(height + 24));
    };
    const observer = new ResizeObserver(update);
    observer.observe(node); update();
    return () => observer.disconnect();
  }, []);
  return <div className="collection-masonry-item" role="listitem" style={{ gridRowEnd: `span ${span}` }}><div ref={measured}>{children}</div></div>;
}

interface NoteCardProps {
  note: Note; platform: Platform; selected: boolean; busy: boolean; fields: string[]; coverRefresh: number;
  onSelect: (checked: boolean) => void; onOpen: () => void; onRewrite: () => void; onDelete: () => void; onCopy: () => void;
}

function NoteCard({ note, platform, selected, busy, fields, coverRefresh, onSelect, onOpen, onRewrite, onDelete, onCopy }: NoteCardProps) {
  const { active } = useWorkspace();
  const card = useRef<HTMLElement>(null);
  const [visible, setVisible] = useState(false);
  const [requested, setRequested] = useState(false);
  useEffect(() => {
    const element = card.current;
    if (!element) return;
    const observer = new IntersectionObserver(entries => setVisible(entries[0]?.isIntersecting || false), { threshold: .01 });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  useEffect(() => { if (active && visible) setRequested(true); }, [active, visible]);
  const detail = useResource<CollectionDetail>(requested ? workspacePath(`/collections/${encodeURIComponent(note.id)}`, platform) : null);
  const assets = detail.data?.note?.id === note.id && Array.isArray(detail.data?.assets) ? detail.data.assets : undefined;
  const mediaSignature = `${coverRefresh}:${note.media_status}:${(note.asset_ids || []).join('|')}`;
  const previousSignature = useRef(mediaSignature);
  useEffect(() => {
    if (!requested || !active || !visible || previousSignature.current === mediaSignature) return;
    previousSignature.current = mediaSignature; void detail.reload(true);
  }, [requested, active, visible, mediaSignature, detail.reload]);
  const pending = !assets || note.media_status !== 'succeeded' || !!detail.error || assets.some(asset => !['ready', 'succeeded', 'completed', 'success', 'failed'].includes(asset.status));
  useEffect(() => {
    if (!requested || !active || !visible || !pending) return;
    const timer = window.setInterval(() => void detail.reload(true), 2000);
    return () => window.clearInterval(timer);
  }, [requested, active, visible, pending, detail.reload]);
  const title = note.title || '等待采集元数据';
  const show = (field: string) => fields.includes(field);
  return <article ref={card} className={`collection-note-card${selected ? ' is-selected' : ''}`} data-note-id={note.id}>
    <CollectionNotePreview note={note} assets={assets} platform={platform} active={active && visible} compact coverRefresh={coverRefresh} fields={fields} />
    <div className="collection-card-controlbar">
      <Checkbox aria-label={`选择笔记：${title}`} checked={selected} onChange={event => onSelect(event.target.checked)} />
      <Button type="text" className="collection-card-rewrite" aria-label="加入仿写库" disabled={busy || note.metadata_status !== 'succeeded'} onClick={onRewrite}>加入仿写库<ArrowRightOutlined /></Button>
      <Dropdown trigger={['click']} placement="bottomRight" menu={{ items: [
        { key: 'detail', label: '查看详情', icon: <EyeOutlined />, onClick: onOpen },
        { key: 'source', label: <a href={note.url} target="_blank" rel="noreferrer">查看原文</a>, icon: <ExportOutlined />, disabled: !note.url },
        { key: 'copy', label: '复制笔记链接', icon: <CopyOutlined />, disabled: !note.url, onClick: onCopy },
        { type: 'divider' },
        { key: 'delete', label: '删除采集记录', icon: <DeleteOutlined />, danger: true, disabled: busy || note.status === 'collecting', onClick: onDelete },
      ] }}><Button type="text" shape="circle" className="collection-card-more" aria-label={`更多操作：${title}`} icon={<EllipsisOutlined />} /></Dropdown>
    </div>
    <div className="collection-card-information">
      {show('tags') && !!note.tags?.length && <div className="collection-card-tags">{note.tags.slice(0, 3).map(tag => <Tag key={tag}>{tag}</Tag>)}{note.tags.length > 3 && <span>+{note.tags.length - 3}</span>}</div>}
      {(show('shares') || show('like_collect_ratio') || show('like_comment_ratio')) && <div className="collection-card-extra-metrics">
        {show('shares') && <span>分享 {number(note.shares)}</span>}{show('like_collect_ratio') && <span>赞藏比 {ratio(note.likes, note.collects)}</span>}{show('like_comment_ratio') && <span>赞评比 {ratio(note.likes, note.comments)}</span>}
      </div>}
      {(show('status') || show('collected_at') || show('published_at')) && <div className="collection-card-meta">{show('status') && <Status value={note.status} />}
        {show('collected_at') && <time title={`采集于 ${formatDate(note.collected_at)}`}>{formatDate(note.collected_at)}</time>}
        {show('published_at') && <time title="发布时间">发布于 {formatDate(note.published_at)}</time>}
      </div>}
    </div>
  </article>;
}

interface FeedProps {
  notes: Note[]; platform: Platform; selected: React.Key[]; busy: boolean; fields: string[]; coverRefresh: number;
  onSelect: (id: string, checked: boolean) => void; onOpen: (id: string) => void;
  onRewrite: (id: string) => void; onDelete: (note: Note) => void; onCopy: (note: Note) => void;
}
export default function CollectionFeed({ notes, platform, selected, busy, fields, coverRefresh, onSelect, onOpen, onRewrite, onDelete, onCopy }: FeedProps) {
  return <div className="collection-feed"><div className="collection-masonry" role="list" aria-label="采集笔记">{notes.map(note => <MasonryItem key={note.id}>
    <NoteCard note={note} platform={platform} selected={selected.includes(note.id)} busy={busy} fields={fields} coverRefresh={coverRefresh}
      onSelect={checked => onSelect(note.id, checked)} onOpen={() => onOpen(note.id)} onRewrite={() => onRewrite(note.id)} onDelete={() => onDelete(note)} onCopy={() => onCopy(note)} />
  </MasonryItem>)}</div></div>;
}
