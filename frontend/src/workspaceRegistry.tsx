import { createContext, useContext } from 'react';
import type { Platform } from './types';
import type { DeletionResult } from './deletion';

export interface WorkspaceRecord { id: string; platform: Platform; name: string }
export interface WorkspaceRegistry {
  items: WorkspaceRecord[];
  add: (platform: Platform) => Promise<void>;
  remove: (platform: Platform) => Promise<DeletionResult | void>;
}
export const WorkspaceRegistryContext = createContext<WorkspaceRegistry>({ items: [], add: async () => {}, remove: async () => {} });
export function useWorkspaceRegistry() { return useContext(WorkspaceRegistryContext); }
