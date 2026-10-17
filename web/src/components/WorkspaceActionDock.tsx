import { createContext, useContext, useLayoutEffect, useRef, useState } from 'react';
import type { HTMLAttributes } from 'react';
import { createPortal } from 'react-dom';
import { useWorkspace } from '../workspace';

export const WorkspaceActionHost = createContext<HTMLDivElement | null>(null);

// The host takes up workspace layout space, while the anchor keeps the actions
// aligned with their original content column as the sidebar or window changes.
export default function WorkspaceActionDock({ children, ...props }: HTMLAttributes<HTMLDivElement>) {
  const host = useContext(WorkspaceActionHost);
  const { active } = useWorkspace();
  const anchor = useRef<HTMLDivElement>(null);
  const [insets, setInsets] = useState<{ left: number; right: number }>();
  useLayoutEffect(() => {
    if (!host || !active || !anchor.current) { setInsets(undefined); return; }
    const target = anchor.current;
    let frame = 0;
    function measure() {
      frame = 0;
      const box = target.getBoundingClientRect();
      const hostBounds = host!.getBoundingClientRect();
      // An empty slot is collapsed; use its layout parent until the first portal mounts.
      const bounds = hostBounds.width ? hostBounds : host!.parentElement!.getBoundingClientRect();
      const next = box.width > 0 && bounds.width > 0 && !target.closest('[hidden]')
        ? { left: Math.max(0, box.left - bounds.left), right: Math.max(0, bounds.right - box.right) }
        : undefined;
      setInsets(previous => previous?.left === next?.left && previous?.right === next?.right ? previous : next);
    }
    const schedule = () => { if (!frame) frame = requestAnimationFrame(measure); };
    const observer = new ResizeObserver(schedule);
    observer.observe(target); observer.observe(host);
    const scrollport = target.closest('.main-content');
    scrollport?.addEventListener('scroll', schedule, { passive: true });
    window.addEventListener('resize', schedule);
    measure();
    return () => { observer.disconnect(); cancelAnimationFrame(frame); scrollport?.removeEventListener('scroll', schedule); window.removeEventListener('resize', schedule); };
  }, [host, active]);
  return <>
    <div ref={anchor} className="workspace-action-anchor" aria-hidden="true" />
    {host && active && insets && createPortal(<div className="workspace-action-dock" style={{ paddingLeft: insets.left, paddingRight: insets.right }}>
      <div {...props}>{children}</div>
    </div>, host)}
  </>;
}
