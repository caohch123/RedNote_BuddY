import { createContext, useContext, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useWorkspace } from '../workspace';

export const WorkspaceScrollbarHost = createContext<HTMLDivElement | null>(null);

interface ScrollMetrics { width: number; visibleWidth: number; contentWidth: number; max: number; left: number }
const initial: ScrollMetrics = { width: 0, visibleWidth: 0, contentWidth: 0, max: 0, left: 0 };
const clamp = (value: number, max: number) => Math.max(0, Math.min(max, value));

export default function WorkspaceTableScrollbar({ panel, label = '采集表格横向滚动条' }: { panel: HTMLElement | null; label?: string }) {
  const host = useContext(WorkspaceScrollbarHost);
  const { active } = useWorkspace();
  const body = useRef<HTMLElement | null>(null);
  const header = useRef<HTMLElement | null>(null);
  const lastLeft = useRef(0);
  const rail = useRef<HTMLDivElement | null>(null);
  const drag = useRef<{ pointer: number; x: number; left: number; max: number; travel: number } | null>(null);
  const [metrics, setMetrics] = useState(initial);
  const [dragging, setDragging] = useState(false);

  useEffect(() => {
    if (!active || !panel || !host) { body.current = null; header.current = null; drag.current = null; return; }
    const node = panel.querySelector<HTMLElement>('.ant-table-body');
    if (!node) return;
    const head = panel.querySelector<HTMLElement>('.ant-table-header');
    body.current = node;
    header.current = head;
    node.scrollLeft = clamp(lastLeft.current, Math.max(0, node.scrollWidth - node.clientWidth));
    const syncPosition = (source: HTMLElement, target: HTMLElement | null) => {
      if (source.clientWidth && target?.clientWidth && target.scrollLeft !== source.scrollLeft) target.scrollLeft = source.scrollLeft;
    };
    let frame = 0;
    const measure = () => {
      if (!node.clientWidth) return;
      syncPosition(node, head);
      lastLeft.current = node.scrollLeft;
      const max = Math.max(0, node.scrollWidth - node.clientWidth);
      // The host is hidden while empty; its parent still provides the available width.
      const width = rail.current?.clientWidth || Math.min(640, Math.max(0, (host.parentElement?.clientWidth || 0) - 24));
      const next = { width, visibleWidth: node.clientWidth, contentWidth: node.scrollWidth, max, left: clamp(node.scrollLeft, max) };
      setMetrics(previous => Object.keys(next).every(key => previous[key as keyof ScrollMetrics] === next[key as keyof ScrollMetrics]) ? previous : next);
    };
    const schedule = () => { cancelAnimationFrame(frame); frame = requestAnimationFrame(measure); };
    // rc-table briefly locks the previous scroll source. Keep both surfaces in
    // sync when the user immediately switches from the header to the dock.
    const onBodyScroll = () => { syncPosition(node, head); if (node.clientWidth) lastLeft.current = node.scrollLeft; schedule(); };
    const onHeaderScroll = () => { if (head) syncPosition(head, node); if (node.clientWidth) lastLeft.current = node.scrollLeft; schedule(); };
    const observer = new ResizeObserver(schedule);
    observer.observe(host); observer.observe(node);
    const table = node.querySelector('table'); if (table) observer.observe(table);
    node.addEventListener('scroll', onBodyScroll, { passive: true });
    head?.addEventListener('scroll', onHeaderScroll, { passive: true });
    window.addEventListener('resize', schedule);
    measure();
    return () => {
      cancelAnimationFrame(frame); observer.disconnect();
      node.removeEventListener('scroll', onBodyScroll); head?.removeEventListener('scroll', onHeaderScroll); window.removeEventListener('resize', schedule);
      body.current = null; header.current = null; drag.current = null;
    };
  }, [active, panel, host]);

  if (!active || !host || metrics.max <= 1 || !metrics.width) return null;
  const thumbWidth = Math.min(metrics.width - 20, Math.max(40, Math.min(metrics.width / 2, metrics.width * metrics.visibleWidth / metrics.contentWidth)));
  const travel = Math.max(1, metrics.width - thumbWidth);
  const scrollTo = (left: number) => {
    if (!body.current) return;
    body.current.scrollLeft = clamp(left, metrics.max);
    lastLeft.current = body.current.scrollLeft;
    if (header.current) header.current.scrollLeft = body.current.scrollLeft;
  };
  return createPortal(<div className="workspace-scrollbar-dock">
    <div ref={rail} className={`workspace-horizontal-scrollbar${dragging ? ' dragging' : ''}`} role="scrollbar" tabIndex={0}
      aria-label={label} aria-orientation="horizontal" aria-valuemin={0} aria-valuemax={Math.round(metrics.max)} aria-valuenow={Math.round(metrics.left)}
      onPointerDown={event => {
        if (event.button !== 0 || !body.current) return;
        event.preventDefault(); event.currentTarget.focus(); event.currentTarget.setPointerCapture(event.pointerId);
        const hitThumb = (event.target as HTMLElement).classList.contains('workspace-scrollbar-thumb');
        const left = hitThumb ? body.current.scrollLeft : clamp((event.clientX - event.currentTarget.getBoundingClientRect().left - thumbWidth / 2) / travel * metrics.max, metrics.max);
        scrollTo(left); drag.current = { pointer: event.pointerId, x: event.clientX, left, max: metrics.max, travel }; setDragging(true);
      }}
      onPointerMove={event => {
        const current = drag.current;
        if (current?.pointer === event.pointerId) scrollTo(current.left + (event.clientX - current.x) / current.travel * current.max);
      }}
      onPointerUp={event => { drag.current = null; setDragging(false); if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId); }}
      onPointerCancel={() => { drag.current = null; setDragging(false); }}
      onLostPointerCapture={() => { drag.current = null; setDragging(false); }}
      onKeyDown={event => {
        const current = body.current?.scrollLeft || 0;
        const targets: Record<string, number> = { ArrowLeft: current - 40, ArrowRight: current + 40, Home: 0, End: metrics.max,
          PageUp: current - metrics.visibleWidth * .8, PageDown: current + metrics.visibleWidth * .8 };
        if (event.key in targets) { event.preventDefault(); scrollTo(targets[event.key]); }
      }}>
      <div className="workspace-scrollbar-thumb" style={{ width: thumbWidth, transform: `translateX(${metrics.left / metrics.max * travel}px)` }} />
    </div>
  </div>, host);
}
