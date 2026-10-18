import { memberAuthResponse } from './member-auth';
import { expect, test } from '@playwright/test';

test('加入来源达到两份后打开最新项目，再次加入仍跳转到同一最新项目', async ({ page }) => {
  const note = { id: 'source', platform: 'xiaohongshu', title: '同一来源', body: '正文', type: 'normal', status: 'ready', metadata_status: 'succeeded', asset_ids: [], topics: [], tags: [], author: {} };
  const projects: any[] = []; let additions = 0;
  await page.route('**/api/v1/**', route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const request = route.request(); const path = new URL(request.url()).pathname.replace('/api/v1', '');
    const json = (value: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) });
    if (path === '/workspaces') return json({ items: [{ platform: 'xiaohongshu' }], total: 1 });
    if (path === '/session') return json({ csrf_token: 'source-limit' });
    if (path === '/collections') return json({ items: [note], total: 1 });
    if (path === '/rewrites' && request.method() === 'POST') {
      expect(request.postDataJSON().note_id).toBe(note.id); additions++;
      if (projects.length === 2) return json(projects[1]);
      const title = projects.length ? '第二份项目' : '第一份项目';
      const project = { id: `project-${projects.length + 1}`, title, note_id: note.id, versions: [{ id: `version-${projects.length + 1}`, number: 1, title, body: note.body, topics: [], asset_ids: [], script: '' }], options: {} };
      projects.push(project); return json(project, 201);
    }
    if (path === '/rewrites') return json({ items: projects, total: projects.length });
    const project = projects.find(item => path === `/rewrites/${item.id}`);
    if (project) return json({ rewrite: project, note, assets: [], videos: [] });
    return json({ items: [], total: 0 });
  });
  await page.goto('/');
  for (const title of ['第一份项目', '第二份项目', '第二份项目']) {
    await page.getByRole('menuitem', { name: '采集库', exact: true }).click();
    await page.getByRole('button', { name: '加入仿写库' }).click();
    await expect(page.locator('.rewrite-list-item.selected')).toContainText(title);
  }
  await expect(page.locator('.rewrite-list-item')).toHaveCount(2);
  await page.locator('.rewrite-list-item').filter({ hasText: '第一份项目' }).click();
  await expect(page.locator('.rewrite-list-item.selected')).toContainText('第一份项目');
  await page.getByRole('menuitem', { name: '采集库', exact: true }).click();
  await page.getByRole('button', { name: '加入仿写库' }).click();
  await expect(page.locator('.rewrite-list-item.selected')).toContainText('第二份项目');
  expect(projects).toHaveLength(2);
  expect(additions).toBe(4);
});
