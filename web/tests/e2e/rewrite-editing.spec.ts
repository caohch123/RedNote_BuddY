import { expect, test, type Page } from '@playwright/test';

async function fixture(page: Page, longTitle = false, rejectRenameOnce = false) {
  const title = longTitle ? '原'.repeat(25) : '原始文案标题';
  const note = { id: 'source-note', platform: 'xiaohongshu', title, body: '来源正文', topics: [], asset_ids: [] };
  const original = { id: 'version-1', number: 1, title, body: '完整的仿写正文', topics: [], asset_ids: [], script: '口播', confirmed: true };
  const rewrite: any = { id: 'rename-project', note_id: note.id, title, versions: [original], confirmed_version_id: original.id, options: {} };
  const writes: { method: string; path: string; platform: string | null; body: any }[] = [];
  await page.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'rewrites');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true');
  });
  await page.route('**/api/v1/**', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname.replace('/api/v1', '');
    const json = (value: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) });
    if (req.method() !== 'GET') {
      expect(req.headers()['x-csrf-token']).toBe('rename-csrf');
      writes.push({ method: req.method(), path, platform: url.searchParams.get('platform'), body: req.postDataJSON() });
    }
    if (path === '/session') return json({ csrf_token: 'rename-csrf' });
    if (path === '/workspaces') return json({ items: [{ platform: 'xiaohongshu' }], total: 1 });
    if (path === '/rewrites') return json({ items: [rewrite], total: 1 });
    if (path === `/rewrites/${rewrite.id}` && req.method() === 'PATCH') {
      if (rejectRenameOnce) { rejectRenameOnce = false; return json({ error: '暂时无法保存项目名称' }, 503); }
      rewrite.title = req.postDataJSON().title.trim(); rewrite.title_customized = true;
      return json(rewrite);
    }
    if (path === `/rewrites/${rewrite.id}/versions`) {
      const next = { ...req.postDataJSON(), id: 'version-2', number: 2, confirmed: false };
      rewrite.versions.push(next); if (!rewrite.title_customized) rewrite.title = next.title;
      return json(next, 201);
    }
    if (path === `/rewrites/${rewrite.id}`) return json({ rewrite, note, assets: [], videos: [] });
    return json({ items: [], total: 0 });
  });
  await page.goto('/');
  await expect(page.getByRole('button', { name: '重命名项目', exact: true })).toBeVisible();
  return { rewrite, original, writes };
}

test('Edge：重命名支持回车保存、取消、刷新恢复且不改变文案版本', async ({ page }) => {
  const { rewrite, original, writes } = await fixture(page);
  await page.setViewportSize({ width: 672, height: 751 });
  await page.getByRole('button', { name: '重命名项目', exact: true }).click();
  const input = page.getByRole('textbox', { name: '项目名称', exact: true });
  await expect(input).toBeFocused();
  await input.fill('  我的养生项目  '); await input.press('Enter');
  await expect(page.getByRole('button', { name: '重命名项目', exact: true })).toHaveText('我的养生项目');
  await expect(page.locator('.rewrite-list-item.selected')).toContainText('我的养生项目');
  await expect(page.getByRole('textbox', { name: '仿写标题', exact: true })).toHaveValue(original.title);
  expect(rewrite.versions).toHaveLength(1);
  await page.reload();
  await expect(page.getByRole('button', { name: '重命名项目', exact: true })).toHaveText('我的养生项目');
  await page.getByRole('button', { name: '重命名项目', exact: true }).click();
  await input.fill('取消的名称'); await input.press('Escape');
  await expect(input).toHaveCount(0);
  await page.getByRole('textbox', { name: '仿写标题', exact: true }).fill('新的笔记标题');
  await page.getByRole('button', { name: /保存新版本/ }).click();
  await expect(page.getByText(/当前版本 2 ·/)).toBeVisible();
  await expect(page.getByRole('button', { name: '重命名项目', exact: true })).toHaveText('我的养生项目');
  expect(rewrite.versions[0].title).toBe(original.title);
  expect(writes.filter(write => write.method === 'PATCH')).toHaveLength(1);
  expect(writes.every(write => write.platform === 'xiaohongshu')).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(672);
});

test('Edge：空项目名称不提交，保存失败保留编辑并允许重试', async ({ page }) => {
  const { writes } = await fixture(page, false, true);
  await page.getByRole('button', { name: '重命名项目', exact: true }).click();
  const input = page.getByRole('textbox', { name: '项目名称', exact: true });
  await input.fill('  '); await input.press('Enter');
  await expect(page.getByText('项目名称不能为空，且不能超过 100 个字符', { exact: true })).toBeVisible();
  expect(writes).toHaveLength(0);
  await input.fill('失败后重试的项目'); await page.getByRole('button', { name: '保存项目名称', exact: true }).click();
  await expect(page.getByText('暂时无法保存项目名称', { exact: true })).toBeVisible();
  await expect(input).toHaveValue('失败后重试的项目');
  await input.press('Enter');
  await expect(page.getByRole('button', { name: '重命名项目', exact: true })).toHaveText('失败后重试的项目');
  expect(writes).toHaveLength(2);
});

test('Edge：旧超长标题提示修正，粘贴与表情按20个字符限制且保存一致', async ({ page }) => {
  const { writes, rewrite } = await fixture(page, true);
  const input = page.getByRole('textbox', { name: '仿写标题', exact: true });
  await expect(input).toHaveValue('原'.repeat(25));
  await expect(page.getByText('标题不能超过 20 个字符，请缩短后保存', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: /创建发布稿/ })).toHaveCount(0);
  await page.getByRole('button', { name: /保存新版本/ }).click();
  expect(writes).toHaveLength(0);
  await input.fill('中'.repeat(21));
  await expect(input).toHaveValue('中'.repeat(20));
  await expect(page.locator('.ant-input-show-count-suffix')).toHaveText('20 / 20');
  await input.fill('😀'.repeat(21));
  await expect(input).toHaveValue('😀'.repeat(20));
  await expect(page.locator('.ant-input-show-count-suffix')).toHaveText('20 / 20');
  await page.getByRole('button', { name: /保存新版本/ }).click();
  await expect(page.getByText(/当前版本 2 ·/)).toBeVisible();
  expect(writes).toHaveLength(1); expect(writes[0].body.title).toBe('😀'.repeat(20));
  expect(rewrite.versions[0].title).toBe('原'.repeat(25));
  await page.reload(); await expect(input).toHaveValue('😀'.repeat(20));
});
