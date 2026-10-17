import type { Platform } from './types';

export const platformNames: Record<Platform, string> = { xiaohongshu: '小红书' };
export const creatorURLs: Record<Platform, string> = { xiaohongshu: 'https://creator.xiaohongshu.com' };
export function workspacePath(path: string, platform: Platform): string {
  const url = new URL(path, 'http://workbench.local');
  url.searchParams.set('platform', platform);
  return `${url.pathname}${url.search}`;
}
export function validPlatformWorkURL(value: string): boolean {
  try {
    const url = new URL(value); const host = url.hostname.toLowerCase();
    return url.protocol === 'https:' && !url.username && !url.password && !url.port &&
      ['xiaohongshu.com', 'xhslink.com'].some(allowed => host === allowed || host.endsWith(`.${allowed}`));
  } catch { return false; }
}
