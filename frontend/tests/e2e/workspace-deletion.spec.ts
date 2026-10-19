import { memberAuthResponse } from './member-auth';
import { expect, test, type Page } from '@playwright/test';

const names = { xiaohongshu: '小红书' };

async function fixtures(page: Page, jobs: any[] = []) {
  const calls: { path: string; platform: string | null; method: string; body: any }[] = [];
  let workspaces: ('xiaohongshu')[] = ['xiaohongshu']; let blocked = false;
  const data = Object.fromEntries(workspaces.map(platform => {
    const notes = [1, 2].map(number => ({ id: `n-${platform}-${number}`, platform, title: `${names[platform]}素材${number}`, body: '原文',
      author: { name: '作者' }, type: 'normal', status: number === 1 ? 'ready' : 'failed', metadata_status: number === 1 ? 'succeeded' : 'failed',
      asset_ids: [], tags: [], topics: [], collected_at: '2026-10-08T00:00:00Z', url: 'https://example.com' }));
    const version = { id: `v-${platform}`, number: 1, title: `${names[platform]}仿写内容`, body: '保存正文', script: '口播内容', topics: [], asset_ids: [], created_at: '2026-10-08T00:00:00Z' };
    const rewrite = { id: `r-${platform}`, note_id: notes[0].id, title: `${names[platform]}仿写项目`, versions: [version], options: { include_body: true, extraction_asset_ids: [] } };
    const publication = { id: `p-${platform}`, rewrite_id: rewrite.id, version_id: version.id, title: `${names[platform]}发布稿`, body: '发布正文', topics: [], asset_ids: [], status: 'ready', target_platform: platform };
    return [platform, { notes, rewrites: [rewrite], publications: [publication] }];
  })) as Record<string, any>;
  await page.route('**/api/v1/**', async route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const request = route.request(); const url = new URL(request.url()); const path = url.pathname.replace('/api/v1', '');
    const platform = url.searchParams.get('platform'); const method = request.method();
    const body = request.headers()['content-type']?.includes('application/json') ? request.postDataJSON() : undefined;
    calls.push({ path, platform, method, body });
    const json = (value: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) });
    if (path === '/session') return json({ csrf_token: 'deletion-fixture' });
    if (method !== 'GET' && request.headers()['x-csrf-token'] !== 'deletion-fixture') return json({ error: 'CSRF 缺失' }, 403);
    if (method === 'DELETE' && !body?.confirm) return json({ error: '缺少确认' }, 400);
    if (method === 'DELETE' && blocked) return json({ error: '关联任务正在运行，请完成后重试', code: 'deletion_busy' }, 409);
    if (path === '/workspaces') {
      if (method === 'POST') { workspaces.push(body.platform); return json({ id: body.platform, platform: body.platform, name: `${names[body.platform as 'xiaohongshu']}工作区` }, 201); }
      return json({ items: workspaces.map(value => ({ id: value, platform: value, name: `${names[value]}工作区` })), total: workspaces.length });
    }
    if (path.startsWith('/workspaces/') && method === 'DELETE') {
      const value = path.split('/')[2] as 'xiaohongshu'; workspaces = workspaces.filter(item => item !== value);
      data[value] = { notes: [], rewrites: [], publications: [] }; return json({ deleted: { notes: 2, rewrites: 1, publications: 1, videos: 0, assets: 0, jobs: 1 }, files_deleted: 0, retained_assets: 0 });
    }
    if (path.endsWith('/account')) return json({ status: 'logged_out', dependency_ready: true });
    if (path === '/health') return json({ dependencies: [] });
    if (path === '/settings') return json({ collector_url: '', providers: {}, cookie_configured: false });
    if (path === '/jobs') return json({ items: jobs, total: jobs.length });
    if (!platform || !workspaces.includes(platform as 'xiaohongshu')) return json({ error: '工作区不存在' }, 404);
    const current = data[platform as 'xiaohongshu'];
    const success = () => json({ deleted: { notes: 1, rewrites: 1, publications: 1, videos: 0, assets: 0, jobs: 1 }, files_deleted: 0, retained_assets: 0 });
    if (path === '/collections' && method === 'DELETE' || path.startsWith('/collections/') && method === 'DELETE') {
      const ids = path === '/collections' ? body.ids : [path.split('/')[2]];
      const deletedRewrites = current.rewrites.filter((item: any) => ids.includes(item.note_id)).map((item: any) => item.id);
      current.notes = current.notes.filter((item: any) => !ids.includes(item.id)); current.rewrites = current.rewrites.filter((item: any) => !deletedRewrites.includes(item.id));
      current.publications = current.publications.filter((item: any) => !deletedRewrites.includes(item.rewrite_id)); return success();
    }
    if (path === '/collections') return json({ items: current.notes, total: current.notes.length });
    if (path.startsWith('/collections/')) { const note = current.notes.find((item: any) => item.id === path.split('/')[2]); return note ? json({ note, assets: [] }) : json({ error: '记录不存在' }, 404); }
    if (path.startsWith('/rewrites/') && method === 'DELETE') {
      const id = path.split('/')[2]; current.rewrites = current.rewrites.filter((item: any) => item.id !== id); current.publications = current.publications.filter((item: any) => item.rewrite_id !== id); return success();
    }
    if (path === '/rewrites') return json({ items: current.rewrites, total: current.rewrites.length });
    if (path.startsWith('/rewrites/')) { const rewrite = current.rewrites.find((item: any) => item.id === path.split('/')[2]); return rewrite ? json({ rewrite, note: current.notes[0], assets: [], videos: [] }) : json({ error: '项目不存在' }, 404); }
    if (path.startsWith('/publications/') && method === 'DELETE') { current.publications = current.publications.filter((item: any) => item.id !== path.split('/')[2]); return success(); }
    if (path === '/publications') return json({ items: current.publications, total: current.publications.length });
    if (path.startsWith('/publications/')) { const value = current.publications.find((item: any) => item.id === path.split('/')[2]); return value ? json(value) : json({ error: '稿件不存在' }, 404); }
    if (path === '/assets') return json({ items: [], total: 0 });
    return json({ error: `未模拟 ${method} ${path}` }, 404);
  });
  return { calls, data, setBlocked: (value: boolean) => { blocked = value; }, workspaces: () => workspaces };
}

async function finalConfirm(page: Page, title: string) {
  const first = page.getByRole('dialog', { name: title, exact: false });
  await first.getByRole('button', { name: '继续删除', exact: false }).click();
  const second = page.getByRole('dialog', { name: `再次确认：${title}`, exact: false });
  await expect(second.getByRole('button', { name: '确认删除', exact: false })).toBeDisabled();
  await second.getByRole('checkbox', { name: '我已了解删除范围，确认永久删除' }).check();
  await second.getByRole('button', { name: '确认删除', exact: false }).click();
}

for (const width of [360, 768, 1440]) {
  test(`顶部控件在 ${width}px 可见且不重叠，页面切换后保留输入`, async ({ page }, testInfo) => {
    await fixtures(page); await page.setViewportSize({ width, height: 900 }); await page.goto('/');
    await expect(page.getByRole('textbox', { name: '小红书链接', exact: true })).toBeVisible();
    const topbar = page.locator('[data-workspace="xiaohongshu"] .topbar');
    await expect(topbar.getByRole('button')).toHaveCount(2);
    await expect(topbar.getByText('本地运行', { exact: false })).toHaveCount(0);
    const boxes = await topbar.getByRole('button').evaluateAll(buttons => buttons.map(button => { const rect = button.getBoundingClientRect(); return { left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom }; }));
    expect(boxes[0].left).toBeGreaterThanOrEqual(0); expect(boxes[1].right).toBeLessThanOrEqual(width);
    expect(boxes[0].right).toBeLessThanOrEqual(boxes[1].left);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
    await page.screenshot({ path: testInfo.outputPath(`responsive-${width}.png`), fullPage: true });
    await page.getByRole('textbox', { name: '小红书链接', exact: false }).fill('未提交的内容');
    await page.getByRole('button', { name: '表格视图', exact: true }).click();
    await page.getByRole('button', { name: '列配置', exact: false }).click(); await expect(page.getByText('显示列', { exact: false })).toBeVisible();
    await page.getByRole('menuitem', { name: /仿写库/ }).click(); await expect(page.getByText('显示列', { exact: false })).toBeHidden();
    await page.getByRole('menuitem', { name: /采集库/ }).click();
    await expect(page.getByRole('textbox', { name: '小红书链接', exact: false })).toHaveValue('未提交的内容');
  });
}

test('单条删除需两次确认，取消不请求，成功关闭详情并清除关联内容', async ({ page }) => {
  const fixture = await fixtures(page); await page.goto('/'); await page.getByRole('button', { name: '更多操作：小红书素材1', exact: true }).click();
  await page.getByRole('menuitem', { name: '查看详情', exact: false }).click();
  const detail = page.getByRole('dialog', { name: '笔记阅读', exact: false }); await detail.getByRole('button', { name: '删除记录', exact: false }).click();
  const first = page.getByRole('dialog', { name: '删除采集记录', exact: false }); await expect(first.getByText(/仿写项目、视频项目、发布稿/)).toBeVisible();
  await first.getByRole('button', { name: '继续删除', exact: false }).click();
  await page.getByRole('dialog', { name: '再次确认：删除采集记录', exact: false }).getByRole('button', { name: /取\s*消/, exact: false }).click();
  expect(fixture.calls.filter(call => call.method === 'DELETE')).toHaveLength(0);
  await detail.getByRole('button', { name: '删除记录', exact: false }).click(); await finalConfirm(page, '删除采集记录'); await expect(detail).toBeHidden();
  const deletion = fixture.calls.find(call => call.method === 'DELETE'); expect(deletion).toMatchObject({ path: '/collections/n-xiaohongshu-1', platform: 'xiaohongshu', body: { confirm: true } });
  expect(fixture.data.xiaohongshu.rewrites).toHaveLength(0); expect(fixture.data.xiaohongshu.publications).toHaveLength(0);
  await page.getByRole('menuitem', { name: '仿写库', exact: false }).click(); await expect(page.getByPlaceholder('搜索仿写项目')).toBeVisible({ timeout: 15000 });
  await expect(page.getByText('先从采集库选择笔记，加入仿写库', { exact: true })).toBeVisible();
});

test('批量删除包含失败记录，服务拒绝时保留选择和确认，再试后按工作区请求', async ({ page }) => {
  const fixture = await fixtures(page); fixture.setBlocked(true); await page.goto('/');
  await expect(page.getByRole('textbox', { name: '小红书链接', exact: true })).toBeVisible(); await page.getByRole('checkbox', { name: '选择本页', exact: true }).check();
  await page.getByRole('button', { name: '批量删除', exact: false }).click(); await finalConfirm(page, '批量删除采集记录');
  const modal = page.getByRole('dialog', { name: '再次确认：批量删除采集记录', exact: false }); await expect(modal.getByText('关联任务正在运行，请完成后重试', { exact: false })).toBeVisible();
  expect(fixture.data.xiaohongshu.notes).toHaveLength(2); fixture.setBlocked(false); await modal.getByRole('button', { name: '确认删除', exact: false }).click(); await expect(modal).toBeHidden();
  const deletions = fixture.calls.filter(call => call.method === 'DELETE'); expect(deletions).toHaveLength(2);
  expect(deletions[1]).toMatchObject({ path: '/collections', platform: 'xiaohongshu', body: { confirm: true, ids: ['n-xiaohongshu-1', 'n-xiaohongshu-2'] } });
  expect(fixture.data.xiaohongshu.notes).toHaveLength(0);
});

test('删除仿写项目清除编辑器和关联稿，原始采集保留', async ({ page }) => {
  const fixture = await fixtures(page); await page.goto('/'); await page.getByRole('menuitem', { name: '仿写库', exact: false }).click();
  await page.getByRole('textbox', { name: '仿写标题', exact: false }).fill('未保存的编辑'); await page.locator('.rewrite-list').getByRole('button', { name: /^项目菜单/ }).first().click();
  await page.locator('.rewrite-project-menu').getByRole('menuitem', { name: '删除', exact: true }).click();
  await finalConfirm(page, '删除仿写项目'); await expect(page.getByRole('textbox', { name: '仿写标题', exact: false })).toHaveCount(0);
  expect(fixture.data.xiaohongshu.notes).toHaveLength(2); expect(fixture.data.xiaohongshu.publications).toHaveLength(0);
});

test('删除本地发布稿关闭详情，保留仿写与采集', async ({ page }) => {
  const fixture = await fixtures(page); await page.goto('/'); await page.getByRole('menuitem', { name: '发布管理', exact: false }).click();
  await page.getByRole('button', { name: /小红书发布稿/ }).first().click(); const detail = page.getByRole('dialog', { name: '发布稿详情', exact: false });
  await detail.getByRole('button', { name: '删除发布稿', exact: false }).click(); await finalConfirm(page, '删除发布稿'); await expect(detail).toBeHidden();
  expect(fixture.data.xiaohongshu.rewrites).toHaveLength(1); expect(fixture.data.xiaohongshu.notes).toHaveLength(2); expect(fixture.data.xiaohongshu.publications).toHaveLength(0);
});

test('删除唯一工作区后显示空状态，可再添加空白工作区', async ({ page }) => {
  const fixture = await fixtures(page); await page.goto('/'); await page.getByRole('textbox', { name: '小红书链接', exact: false }).fill('旧工作区未保存输入');
  await page.getByRole('menuitem', { name: '设置', exact: false }).click();
  const row = page.locator('.workspace-management-row').filter({ hasText: '小红书工作区' }); await row.getByRole('button', { name: '删除工作区', exact: false }).click();
  await finalConfirm(page, '删除工作区');
  await expect(page.getByRole('heading', { name: '添加你的第一个工作区', exact: false })).toBeVisible();
  expect(fixture.workspaces()).toEqual([]);
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await page.getByRole('button', { name: '添加小红书工作区', exact: false }).click(); await expect(page.getByRole('textbox', { name: '小红书链接', exact: true })).toBeVisible();
  await expect(page.getByRole('textbox', { name: '小红书链接', exact: false })).toHaveValue(''); expect(fixture.workspaces()).toEqual(['xiaohongshu']);
});

test('后台抽屉与两次删除确认遮罩覆盖顶部按钮，按钮不穿透弹窗', async ({ page }) => {
  await fixtures(page); await page.goto('/');
  const button = page.getByRole('button', { name: '工作台账户菜单', exact: true });
  async function covered() {
    const box = await button.boundingBox(); expect(box).toBeTruthy();
    expect(await page.evaluate(({ x, y }) => !!document.elementFromPoint(x, y)?.closest('.ant-drawer, .ant-modal-root'), { x: box!.x + box!.width / 2, y: box!.y + box!.height / 2 })).toBe(true);
  }
  await page.getByRole('button', { name: '后台任务' }).click();
  const jobs = page.getByRole('dialog', { name: '小红书后台任务', exact: true }); await expect(jobs).toBeVisible(); await covered();
  await jobs.locator('.ant-drawer-close').click(); await expect(jobs).toBeHidden();
  await page.getByRole('button', { name: '更多操作：小红书素材1', exact: true }).click();
  await page.getByRole('menuitem', { name: '查看详情', exact: false }).click();
  await page.getByRole('dialog', { name: '笔记阅读', exact: false }).getByRole('button', { name: '删除记录' }).click();
  const first = page.getByRole('dialog', { name: '删除采集记录', exact: true }); await expect(first).toBeVisible(); await covered();
  await first.getByRole('button', { name: '继续删除' }).click(); await expect(page.getByRole('dialog', { name: '再次确认：删除采集记录', exact: true })).toBeVisible(); await covered();
});

test('图片服务使用独立模型和密钥，配音和渲染设置已移除', async ({ page }) => {
  const fixture = await fixtures(page); await page.goto('/'); await page.getByRole('menuitem', { name: '设置' }).click();
  await page.getByRole('tab', { name: '共用 AI 服务', exact: true }).click();
  await expect(page.getByRole('textbox', { name: '历史视频生成 模型', exact: true })).toHaveCount(0);
  await page.getByRole('textbox', { name: '图片生成与仿写 API 地址', exact: true }).fill('https://images.example.com/v1');
  await page.getByRole('textbox', { name: '图片生成与仿写 模型', exact: true }).fill('fixture-image-model');
  await page.getByRole('textbox', { name: '图片生成与仿写 API Key', exact: true }).fill('fixture-image-key');
  await expect(page.getByRole('textbox', { name: '仿写文案 API Key', exact: true })).toHaveValue('');
  await page.getByRole('button', { name: '保存设置' }).first().click();
  await expect.poll(() => fixture.calls.filter(call => call.path === '/settings' && call.method === 'PATCH').length).toBe(1);
  const update = fixture.calls.find(call => call.path === '/settings' && call.method === 'PATCH')!;
  expect(update.platform).toBe('xiaohongshu');
  expect(update.body.providers).toEqual({ image: { base_url: 'https://images.example.com/v1', model: 'fixture-image-model', api_key: 'fixture-image-key', workspace_id: '' } });
});

test('图片任务和停用视频任务均不能通过通用入口重复付费', async ({ page }) => {
  const fixture = await fixtures(page, [
    { id: 'image-unknown', kind: 'image_rewrite', entity_id: 'image-project', state: 'submission_unknown', phase: 'submission_unknown', progress: 15, created_at: '2026-10-08T00:00:00Z' },
    { id: 'legacy-video-unknown', kind: 'video', entity_id: 'legacy-project', state: 'submission_unknown', phase: 'submission_unknown', progress: 15, created_at: '2026-10-08T00:00:00Z' },
  ]);
  await page.goto('/'); await page.getByRole('button', { name: '后台任务' }).click();
  const drawer = page.getByRole('dialog', { name: '小红书后台任务', exact: true });
  await expect(drawer.getByRole('button', { name: '重试此任务', exact: true })).toHaveCount(0);
  await expect(drawer.getByText('已停用的旧视频任务', { exact: true })).toBeVisible();
  await expect(page.getByPlaceholder('火山方舟任务 ID', { exact: true })).toHaveCount(0);
  expect(fixture.calls.filter(call => call.path.endsWith('/retry') && call.method === 'POST')).toHaveLength(0);
});
