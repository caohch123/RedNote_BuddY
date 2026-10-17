import { useEffect, useRef } from 'react';
import type { Platform } from './types';

const eventName = 'workbench:jobs-cleared';
const cleared = new Map<Platform, Set<string>>();
export function jobWasCleared(platform: Platform, id: string) { return cleared.get(platform)?.has(id) || false; }
export function notifyJobsCleared(platform: Platform, ids: string[]) {
  const known = cleared.get(platform) || new Set<string>();
  ids.forEach(id => known.add(id)); cleared.set(platform, known);
  window.dispatchEvent(new CustomEvent(eventName, { detail: { platform, ids } }));
}
export function useJobsCleared(platform: Platform, callback: (ids: string[]) => void) {
  const handler = useRef(callback); handler.current = callback;
  useEffect(() => {
    const receive = (event: Event) => {
      const detail = (event as CustomEvent<{ platform: Platform; ids: string[] }>).detail;
      if (detail.platform === platform) handler.current(detail.ids);
    };
    window.addEventListener(eventName, receive);
    return () => window.removeEventListener(eventName, receive);
  }, [platform]);
}
