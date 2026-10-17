import { createContext, useContext } from 'react';
import type { Platform } from './types';

export interface WorkspaceState { platform: Platform; active: boolean }
export const WorkspaceContext = createContext<WorkspaceState>({ platform: 'xiaohongshu', active: true });
export function useWorkspace() { return useContext(WorkspaceContext); }

export function readWorkspace(): Platform {
  return 'xiaohongshu';
}
export function saveWorkspace() {
  try { localStorage.setItem('workbench.workspace', 'xiaohongshu'); } catch { /* Keep working when browser storage is unavailable. */ }
}
