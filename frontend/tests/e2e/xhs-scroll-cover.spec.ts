import { memberAuthResponse } from './member-auth';
import { expect, test } from '@playwright/test';

const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aJEkAAAAASUVORK5CYII=', 'base64');

test('小红书页面在滚动锁残留时仍可滚动，封面下载完成后出现并可重试', async ({ page }) => {
  let coverReady = false;
  let failNextImage = false;
  let imageRequests = 0;
  const notes = Array.from({ length: 20 }, (_, index) => ({
    id: `note-${index}`, platform: 'xiaohongshu', note_id: `source-${index}`, url: 'https://www.xiaohongshu.com/explore/example',
    title: index === 0 ? '小红书封面恢复示例' : `小红书笔记 ${index}`, body: '采集正文', author: { name: '测试作者' },
    type: 'image', status: index === 0 ? 'collecting' : 'ready', metadata_status: 'succeeded',
    media_status: index === 0 ? 'running' : 'succeeded', cover_asset_id: '',
    asset_ids: index === 0 ? ['cover-ready'] : [], tags: [], topics: [], collected_at: '2026-10-09T08:00:00Z',
  }));
  await page.route('**/api/v1/**', async route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    const json = (body: unknown) => route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) });
    if (path === '/workspaces') return json({ items: [{ id: 'xiaohongshu', platform: 'xiaohongshu', name: '小红书工作区' }], total: 1 });
    if (path === '/jobs') return json({ items: [], total: 0 });
    if (path === '/collections') return json({ items: notes.map((note, index) => index === 0 && coverReady
      ? { ...note, status: 'ready', media_status: 'succeeded', cover_asset_id: 'cover-ready' } : note), total: notes.length });
    if (path === '/collections/note-0') return json({ note: coverReady
      ? { ...notes[0], status: 'ready', media_status: 'succeeded', cover_asset_id: 'cover-ready' } : notes[0], assets: coverReady ? [{ id: 'cover-ready', owner_id: 'note-0', kind: 'image', mime: 'image/png', status: 'ready', position: 0, width: 1, height: 1, duration: 0, extraction: { text: '', segments: [], status: 'succeeded', edited: false } }] : [] });
    if (/^\/collections\/note-\d+$/.test(path)) return json({ note: notes.find(note => note.id === path.split('/')[2]), assets: [] });
    if (path === '/assets/cover-ready/file') {
      imageRequests++;
      if (failNextImage) return route.fulfill({ status: 503, body: '暂时不可用' });
      return route.fulfill({ contentType: 'image/png', body: png });
    }
    return json({ items: [], total: 0 });
  });
  await page.goto('/');
  const workspace = page.locator('[data-workspace="xiaohongshu"]');
  const content = workspace.locator('.main-content');
  await expect(page.getByRole('textbox', { name: '小红书链接', exact: true })).toBeVisible();
  expect(imageRequests).toBe(0);
  await page.evaluate(() => { document.body.style.overflow = 'hidden'; });
  await content.hover();
  await page.mouse.wheel(0, 500);
  await expect.poll(() => content.evaluate(element => element.scrollTop)).toBeGreaterThan(0);
  await page.evaluate(() => { document.body.style.overflow = ''; });
  await workspace.getByRole('button', { name: '更多操作：小红书封面恢复示例', exact: true }).click();
  await page.getByRole('menuitem', { name: '查看详情', exact: false }).click();
  await page.getByRole('dialog', { name: '笔记阅读' }).getByRole('button', { name: '关闭' }).click();
  await content.hover();
  await page.mouse.wheel(0, -400);
  await expect.poll(() => content.evaluate(element => element.scrollTop)).toBeLessThan(500);

  coverReady = true;
  failNextImage = true;
  await workspace.getByRole('button', { name: '刷新采集库', exact: true }).click();
  await expect.poll(() => imageRequests).toBe(1);
  failNextImage = false;
  await workspace.getByRole('button', { name: '刷新采集库', exact: true }).click();
  const cover = workspace.locator('.collection-note-card img[alt="小红书封面恢复示例"]');
  await expect.poll(() => cover.evaluate(image => (image as HTMLImageElement).naturalWidth)).toBe(1);
  expect(imageRequests).toBeGreaterThanOrEqual(2);
});
