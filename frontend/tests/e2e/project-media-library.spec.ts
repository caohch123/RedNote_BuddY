import { expect, test } from '@playwright/test';

test('项目内素材库独立查询，切换项目和页签隔离预览，兼容旧导航', async ({ page }) => {
  const requests: string[] = []; const writes: string[] = [];
  const note = { id: 'source', platform: 'xiaohongshu', title: '来源', body: '原正文', author: {}, asset_ids: [], topics: [], tags: [] };
  const projects = ['甲项目', '乙项目'].map((title, index) => ({ id: `project-${index}`, note_id: note.id, title,
    versions: [{ id: `version-${index}`, number: 1, title, body: '原正文', script: '', topics: [], asset_ids: [], source_snapshot: note }], image_rewrites: [] }));
  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jxdoAAAAASUVORK5CYII=', 'base64');
  await page.route('**/api/v1/**', async route => {
    const request = route.request(); const url = new URL(request.url()); const path = url.pathname.replace('/api/v1', '');
    const json = (value: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) });
    if (request.method() !== 'GET') { writes.push(path); return json({ error: '禁止写入' }, 403); }
    if (path === '/workspaces') return json({ items: [{ platform: 'xiaohongshu' }], total: 1 });
    if (path === '/rewrites') return json({ items: projects, total: projects.length });
    const project = projects.find(item => path === `/rewrites/${item.id}`);
    if (project) return json({ rewrite: project, note, assets: [], videos: [] });
    if (path === '/media-library') {
      const id = url.searchParams.get('rewrite_id'); requests.push(id || '');
      const selected = projects.find(item => item.id === id);
      if (!selected) return json({ error: '需要项目' }, 404);
      const asset = { id: `image-${id}`, owner_id: id, kind: 'image', mime: 'image/png', status: 'ready', width: 960, height: 1280 };
      const items = [{ id: asset.id, asset, rewrite_id: id, project_title: selected.title, origin: 'image_rewrite', revision: 1, stale: false }];
      return json({ items, total: items.length });
    }
    if (path.startsWith('/assets/')) return route.fulfill({ contentType: 'image/png', body: png });
    return json({ items: [], total: 0 });
  });
  await page.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'library');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true');
  });
  await page.goto('/');
  await expect(page.getByRole('menuitem', { name: /素材库/ })).toHaveCount(0);
  await expect(page.getByPlaceholder('搜索仿写项目')).toBeVisible();
  expect(await page.evaluate(() => localStorage.getItem('workbench.xiaohongshu.page'))).toBe('rewrites');
  await page.getByRole('textbox', { name: '仿写正文' }).fill('未保存的文案');
  await page.getByRole('tab', { name: '素材库', exact: true }).click();
  await expect(page.locator('.library-media-card')).toHaveCount(1);
  await page.getByRole('button', { name: '素材操作：甲项目' }).click();
  await expect(page.getByRole('link', { name: '下载素材' })).toHaveAttribute('href', /image-project-0/);
  await page.getByRole('button', { name: '素材操作：甲项目' }).click();
  await expect(page.getByRole('combobox', { name: '素材所属项目' })).toHaveCount(0);
  await page.getByRole('button', { name: '预览甲项目', exact: true }).click();
  await expect(page.getByRole('dialog', { name: '甲项目' })).toBeVisible();
  const dialog = page.getByRole('dialog', { name: '甲项目' });
  await dialog.locator('.ant-modal-footer').getByRole('button', { name: '关闭', exact: true }).click();
  await page.getByRole('tab', { name: '文案编辑', exact: true }).click();
  await expect(page.getByRole('searchbox', { name: '搜索素材库' })).toBeHidden();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('textbox', { name: '仿写正文' })).toHaveValue('未保存的文案');
  await page.locator('.rewrite-list-item').filter({ hasText: '乙项目' }).click();
  await page.getByRole('tab', { name: '素材库', exact: true }).click();
  await page.getByRole('button', { name: '素材操作：乙项目' }).click();
  await expect(page.getByRole('link', { name: '下载素材' })).toHaveAttribute('href', /image-project-1/);
  await page.getByRole('button', { name: '素材操作：乙项目' }).click();
  await expect(page.getByRole('button', { name: '预览甲项目', exact: true })).toHaveCount(0);
  for (const width of [1536, 825, 597]) {
    await page.setViewportSize({ width, height: 751 });
    await expect(page.getByRole('searchbox', { name: '搜索素材库' })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  }
  expect(new Set(requests)).toEqual(new Set(['project-0', 'project-1']));
  expect(writes).toEqual([]);
});
