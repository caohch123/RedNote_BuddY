import { expect, test, type Page } from '@playwright/test';

async function fixture(page: Page, options: { failOnce?: boolean; hold?: boolean } = {}) {
  const note = { id: 'pin-note', platform: 'xiaohongshu', title: '原笔记', body: '正文', topics: [], asset_ids: [] };
  const projects = ['最新项目', '较早项目', '最早项目'].map((title, index) => ({
    id: `pin-${index}`, note_id: note.id, title, pinned: false, options: {},
    updated_at: `2026-10-0${3 - index}T12:00:00Z`, created_at: '2026-10-01T00:00:00Z',
    versions: [{ id: `version-${index}`, number: 1, title, body: '保留当前文案', script: '', topics: [], asset_ids: [] }],
  }));
  const writes: any[] = []; let fail = options.failOnce;
  let release!: () => void; const pending = new Promise<void>(resolve => { release = resolve; });
  await page.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'rewrites');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true');
  });
  await page.route('**/api/v1/**', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname.replace('/api/v1', '');
    const json = (value: unknown, status = 200) => route.fulfill({ status, json: value });
    if (path === '/session') return json({ csrf_token: 'pin-csrf' });
    if (path === '/workspaces') return json({ items: [{ platform: 'xiaohongshu' }], total: 1 });
    if (path === '/rewrites') return json({ items: [...projects].sort((a, b) => Number(b.pinned) - Number(a.pinned) || b.updated_at.localeCompare(a.updated_at)), total: projects.length });
    const project = projects.find(p => path === `/rewrites/${p.id}`);
    if (project && req.method() === 'PATCH') {
      expect(req.headers()['x-csrf-token']).toBe('pin-csrf'); expect(url.searchParams.get('platform')).toBe('xiaohongshu');
      const body = req.postDataJSON(); writes.push(body); expect(Object.keys(body)).toEqual(['pinned']);
      if (fail) { fail = false; return json({ error: '暂时无法置顶' }, 503); }
      if (options.hold) await pending;
      project.pinned = body.pinned; return json(project);
    }
    if (project) return json({ rewrite: project, note, assets: [], videos: [] });
    if (req.method() !== 'GET') throw new Error(`Unexpected mutation: ${path}`);
    return json({ items: [], total: 0 });
  });
  await page.goto('/'); await expect(page.getByRole('button', { name: '重命名项目', exact: true })).toHaveText('最新项目');
  return { projects, writes, release };
}
const rows = (page: Page) => page.locator('.rewrite-list-row');
const row = (page: Page, index: number) => page.locator(`[data-rewrite-id="pin-${index}"]`);
async function order(page: Page, ids: string[]) {
  await expect.poll(() => rows(page).evaluateAll(elements => elements.map(el => el.getAttribute('data-rewrite-id')))).toEqual(ids);
}

for (const width of [474, 825, 1536]) test(`Edge ${width}px 置顶可持久化、取消后恢复顺序且不丢失当前编辑`, async ({ page }) => {
  await page.setViewportSize({ width, height: 751 }); const f = await fixture(page);
  const draft = page.getByRole('textbox', { name: '仿写标题', exact: true }); await draft.fill('未保存的输入');
  await row(page, 1).getByRole('button', { name: '置顶仿写项目 较早项目', exact: true }).click();
  await order(page, ['pin-1', 'pin-0', 'pin-2']);
  await expect(row(page, 1).getByRole('button', { name: '取消置顶仿写项目 较早项目', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await expect(draft).toHaveValue('未保存的输入'); await expect(page.locator('.rewrite-list-item.selected')).toContainText('最新项目');
  await row(page, 2).getByRole('button', { name: '置顶仿写项目 最早项目', exact: true }).click();
  await order(page, ['pin-1', 'pin-2', 'pin-0']);
  await page.reload(); await expect(page.getByRole('button', { name: '重命名项目', exact: true })).toHaveText('较早项目');
  await order(page, ['pin-1', 'pin-2', 'pin-0']);
  await row(page, 1).getByRole('button', { name: '取消置顶仿写项目 较早项目', exact: true }).click();
  await row(page, 2).getByRole('button', { name: '取消置顶仿写项目 最早项目', exact: true }).click();
  await order(page, ['pin-0', 'pin-1', 'pin-2']);
  expect(f.projects.map(p => p.updated_at)).toEqual(['2026-10-03T12:00:00Z', '2026-10-02T12:00:00Z', '2026-10-01T12:00:00Z']);
  expect(f.writes).toEqual([{ pinned: true }, { pinned: true }, { pinned: false }, { pinned: false }]);
  const buttons = row(page, 1).locator('.rewrite-row-actions button');
  await expect(buttons).toHaveCount(2);
  const sizes = await buttons.evaluateAll(elements => elements.map(el => ({ width: el.getBoundingClientRect().width, height: el.getBoundingClientRect().height })));
  expect(sizes).toEqual([{ width: 24, height: 24 }, { width: 24, height: 24 }]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
});

test('Edge 置顶失败保持顺序与编辑，允许重试', async ({ page }) => {
  const f = await fixture(page, { failOnce: true });
  await row(page, 1).getByRole('button', { name: '置顶仿写项目 较早项目', exact: true }).click();
  await expect(page.getByText('暂时无法置顶', { exact: true })).toBeVisible();
  await order(page, ['pin-0', 'pin-1', 'pin-2']);
  await row(page, 1).getByRole('button', { name: '置顶仿写项目 较早项目', exact: true }).click();
  await order(page, ['pin-1', 'pin-0', 'pin-2']); expect(f.writes).toHaveLength(2);
});

test('Edge 快速重复点击置顶只提交一次', async ({ page }) => {
  const f = await fixture(page, { hold: true });
  const pin = row(page, 1).getByRole('button', { name: '置顶仿写项目 较早项目', exact: true });
  await pin.evaluate(button => { (button as HTMLButtonElement).click(); (button as HTMLButtonElement).click(); });
  await expect.poll(() => f.writes.length).toBe(1);
  f.release(); await order(page, ['pin-1', 'pin-0', 'pin-2']); expect(f.writes).toHaveLength(1);
});
