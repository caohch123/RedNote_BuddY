import { useRef, useState, type DragEvent } from 'react';
import { Checkbox } from 'antd';
import { HolderOutlined } from '@ant-design/icons';
import type { Asset } from '../types';
import { defaultPublicationCards, movePublicationMedia, reorderPublicationCards, togglePublicationMedia } from '../publicationMedia';
import { Media } from './Common';

const dragType = 'application/x-workbench-publication-media';

export default function MediaSelectionGrid({ assets, selectedIDs, onChange, disabled = false }: {
  assets: Asset[]; selectedIDs: string[]; onChange: (update: (ids: string[]) => string[]) => void; disabled?: boolean;
}) {
  const byID = new Map(assets.map(asset => [asset.id, asset]));
  const selected = new Set(selectedIDs);
  // Toggling keeps the card in its original slot; new uploads append at the end.
  const [cardIDs, setCardIDs] = useState(() => defaultPublicationCards([...byID.keys()], selectedIDs));
  const displayedIDs = [...cardIDs.filter(id => byID.has(id)), ...[...byID.keys()].filter(id => !cardIDs.includes(id))];
  const dragID = useRef('');
  const [draggingID, setDraggingID] = useState('');
  const [dropID, setDropID] = useState('');
  const [announcement, setAnnouncement] = useState('');
  const clearDrag = () => { dragID.current = ''; setDraggingID(''); setDropID(''); };
  const move = (sourceID: string, targetID: string) => {
    if (disabled || !byID.has(sourceID) || !byID.has(targetID)) return;
    const next = movePublicationMedia(selectedIDs, sourceID, targetID);
    if (next === selectedIDs) return;
    setCardIDs(reorderPublicationCards(displayedIDs, next));
    onChange(ids => movePublicationMedia(ids, sourceID, targetID));
    setAnnouncement(`素材 ${byID.get(sourceID)!.position + 1} 已移至第 ${next.indexOf(sourceID) + 1} 位`);
  };
  const canDrop = (id: string, event: DragEvent<HTMLElement>) => !disabled && dragID.current !== id
    && selected.has(dragID.current) && selected.has(id) && event.dataTransfer.types.includes(dragType);
  return <div className="media-select-grid publication-media-grid">{displayedIDs.map(id => {
    const asset = byID.get(id)!;
    const sortable = !disabled && selected.has(id);
    return <article key={id} data-asset-id={id} role="group" aria-label={`素材 ${asset.position + 1}`}
      tabIndex={sortable ? 0 : undefined} draggable={sortable}
      className={`media-selection-card${selected.has(id) ? ' selected-media' : ''}${sortable && draggingID === id ? ' media-dragging' : ''}${sortable && dropID === id ? ' media-drop-target' : ''}`}
      onDragStart={event => {
        const target = event.target as HTMLElement;
        if (!sortable || (!target.closest('.publication-media-drag-handle') && target.closest('input, button, video, audio, a, .ant-checkbox-wrapper'))) { event.preventDefault(); return; }
        dragID.current = id;
        event.dataTransfer.effectAllowed = 'move';
        event.dataTransfer.setData(dragType, id);
        const bounds = event.currentTarget.getBoundingClientRect();
        event.dataTransfer.setDragImage(event.currentTarget, event.clientX - bounds.left, event.clientY - bounds.top);
        setDraggingID(id);
      }}
      onDragOver={event => {
        if (!canDrop(id, event)) return;
        event.preventDefault(); event.dataTransfer.dropEffect = 'move'; setDropID(id);
      }}
      onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDropID(current => current === id ? '' : current); }}
      onDrop={event => {
        if (canDrop(id, event) && event.dataTransfer.getData(dragType) === dragID.current) {
          event.preventDefault(); move(dragID.current, id);
        }
        clearDrag();
      }}
      onDragEnd={clearDrag}
      onKeyDown={event => {
        if (!sortable || (event.target !== event.currentTarget && !(event.target as HTMLElement).closest('.publication-media-drag-handle')) || !event.altKey || !['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(event.key)) return;
        event.preventDefault();
        const ordered = displayedIDs.filter(item => selected.has(item));
        const direction = ['ArrowLeft', 'ArrowUp'].includes(event.key) ? -1 : 1;
        const targetID = ordered[ordered.indexOf(id) + direction];
        if (targetID) move(id, targetID);
      }}>
      <Media asset={asset} compact />
      <button type="button" className="publication-media-drag-handle" aria-label={`拖拽素材 ${asset.position + 1}`} aria-keyshortcuts="Alt+ArrowLeft Alt+ArrowRight Alt+ArrowUp Alt+ArrowDown" draggable={sortable} disabled={!sortable}><HolderOutlined /></button>
      <Checkbox className="publication-media-selection" checked={selected.has(id)} disabled={disabled} aria-label={`选用素材 ${asset.position + 1}`}
        onChange={event => {
          const checked = event.target.checked;
          onChange(ids => togglePublicationMedia(ids, id, checked, displayedIDs));
        }}>素材 {asset.position + 1} · {asset.kind === 'video' || asset.mime?.startsWith('video/') ? '视频' : '图片'}</Checkbox>
    </article>;
  })}<span className="media-sort-announcement" aria-live="polite" aria-atomic="true">{announcement}</span></div>;
}
