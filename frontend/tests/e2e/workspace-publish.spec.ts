import { memberAuthResponse } from './member-auth';
import { expect, test, type Page } from '@playwright/test';

const png = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aJEkAAAAASUVORK5CYII=';
const xhs = { id: 'xhs-draft', rewrite_id: 'xhs-rewrite', title: '小红书独立发布稿', body: '小红书工作区文案', target_platform: 'xiaohongshu', status: 'ready', asset_ids: ['xhs-image'], created_at: '2026-10-08T03:00:00Z' };

async function fixture(page: Page, loggedIn = true, options: { workspaceEmpty?: boolean } = {}) {
  const requests: { path: string; platform: string | null; method: string; body?: any }[] = [];
  let draftWithdrawn = false;
  const config = { collector_url: 'http://127.0.0.1:5556', cookie_configured: true, cookie_mask: '已保存 Cookie',
    ffmpeg_path: 'ffmpeg.exe', ffprobe_path: 'ffprobe.exe', node_path: 'node.exe',
    providers: Object.fromEntries(['text', 'ocr', 'asr', 'video'].map(key => [key, { base_url: 'https://example.com/v1', model: `model-${key}`, configured: false }])) };
  await page.route('**/api/v1/**', async route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const request = route.request(); const url = new URL(request.url()); const path = url.pathname.replace('/api/v1', ''); const method = request.method(); const platform = url.searchParams.get('platform');
    const body = method !== 'GET' && request.postData() ? request.postDataJSON() : undefined; requests.push({ path, platform, method, body });
    const json = (value: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) });
    if (path === '/workspaces') return json({ items: [{ id: 'xiaohongshu', platform: 'xiaohongshu', name: '小红书' }], total: 1 });
    if (path === '/session') return json({ csrf_token: 'workspace-csrf' });
    if (path === '/xhs/account') return json({ status: loggedIn ? 'logged_in' : 'logged_out', dependency_ready: true, ...(loggedIn ? { account: { id: 'xhs-owner', nickname: '独立小红书账号' } } : {}) });
    if (path === '/xhs/login') return json({ login_id: 'xhs-login' }, 202);
    if (path === '/xhs/login/xhs-login') return json({ login_id: 'xhs-login', status: 'waiting_scan', qr_image: png, expires_at: new Date(Date.now() + 60000).toISOString() });
    if (path === '/publications') {
      const items = options.workspaceEmpty || draftWithdrawn ? [] : [xhs];
      return json({ items, total: items.length });
    }
    if (path === '/publications/xhs-draft') return !options.workspaceEmpty && !draftWithdrawn ? json(xhs) : json({ error: '工作区不匹配' }, 404);
    if (path.startsWith('/publications/legacy-cross-draft')) return json({ error: '来源和素材不属于当前工作区' }, 404);
    if (path === '/assets') {
      const items = options.workspaceEmpty || draftWithdrawn ? [] : [{ id: 'xhs-image', status: 'ready', kind: 'image', mime: 'image/png', position: 0 }];
      return json({ items, total: items.length });
    }
    if (path === '/assets/xhs-image/file') return route.fulfill({ contentType: 'image/png', body: Buffer.from(png.split(',')[1], 'base64') });
    if (['/collections', '/rewrites', '/jobs'].includes(path)) return json({ items: [], total: 0 });
    if (path === '/settings') {
      if (method === 'PATCH') {
        const { providers, ...other } = body; Object.assign(config, other);
        for (const [key, changes] of Object.entries(providers || {})) config.providers[key] = { ...config.providers[key], ...(changes as object) };
      }
      return json(config);
    }
    if (path === '/health') return json({ dependencies: { collector: { ok: true }, node: { ok: true }, ffmpeg: { ok: true }, database: { ok: true } } });
    return json({ error: `未模拟接口 ${method} ${path}` }, 404);
  });
  await page.goto('/');
  return { requests, withdrawDraft: () => { draftWithdrawn = true; } };
}

test('发布管理只显示小红书账号、稿件和创作入口', async ({ page }) => {
  const { requests } = await fixture(page); await page.getByRole('menuitem', { name: '发布管理' }).click();
  await expect(page.locator('.publication-page > .xhs-account-panel')).toHaveCSS('border-radius', '0px');
  await expect(page.locator('.publication-page .overview-card').first()).toHaveCSS('border-radius', '10px 10px 0px 0px');
  await expect(page.locator('.publication-page > .publication-table-panel')).toHaveCSS('border-radius', '0px 0px 11px 11px');
  await expect(page.getByRole('button', { name: '小红书独立发布稿', exact: false })).toBeVisible();
  await expect(page.getByText('独立小红书账号', { exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: '打开小红书创作平台' })).toBeVisible();
  await expect(page.getByRole('combobox', { name: '发布平台筛选' })).toHaveCount(0);
  expect(requests.some(r => r.path === '/publications' && r.platform === 'xiaohongshu')).toBe(true);
  await page.getByRole('button', { name: '小红书独立发布稿', exact: false }).click(); const detail = page.getByRole('dialog', { name: '发布稿详情' });
  await expect(detail.getByText('小红书工作区文案', { exact: true })).toBeVisible();
  await expect(detail.getByRole('img', { name: '素材 1', exact: true })).toHaveAttribute('src', '/api/v1/assets/xhs-image/file?platform=xiaohongshu');
  await detail.locator('button.ant-drawer-close').click(); await page.reload();
  await expect(page.getByRole('button', { name: '小红书独立发布稿', exact: false })).toBeVisible();
  expect(requests.filter(r => r.method !== 'GET')).toHaveLength(0);
});

test('发布管理操作列使用图标按钮并在悬停时显示操作文字', async ({ page }) => {
  await fixture(page); await page.getByRole('menuitem', { name: '发布管理' }).click();
  const row = page.locator('.publication-table-panel tr').filter({ hasText: xhs.title });
  const actions = row.locator('td').last();
  for (const label of ['发布到小红书', '复制文案', '导出发布包', '记录已发布', '删除发布稿']) {
    const button = actions.getByRole('button', { name: label, exact: true });
    await expect(button).toBeVisible();
    await expect(button).toHaveText('');
    await expect(button.locator('.anticon')).toBeVisible();
  }
  const copyColor = await actions.getByRole('button', { name: '复制文案', exact: true }).evaluate(button => getComputedStyle(button).color);
  await expect(actions.getByRole('button', { name: '删除发布稿', exact: true })).toHaveCSS('color', copyColor);
  await actions.getByRole('button', { name: '复制文案', exact: true }).hover();
  await expect(page.getByRole('tooltip')).toHaveText('复制文案');
});

test('历史跨平台稿件不进入工作区，显示空列表', async ({ page }) => {
  const { requests } = await fixture(page, true, { workspaceEmpty: true });
  await page.getByRole('menuitem', { name: '发布管理' }).click();
  await expect(page.getByText('确认一个仿写版本，创建你的第一篇发布稿', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: '小红书独立发布稿', exact: false })).not.toBeVisible();
  await expect(page.getByRole('button', { name: '小红书素材的历史抖音稿', exact: false })).not.toBeVisible();
  await expect(page.locator('.publication-page .overview-card').filter({ hasText: '可发布' }).locator('strong')).toHaveText('0');
  await expect(page.locator('.publication-page .publication-media img')).toHaveCount(0);
  expect(requests.some(r => r.path === '/publications' && r.platform === 'xiaohongshu')).toBe(true);
  expect(requests.some(r => r.platform === 'xiaohongshu' && (r.path.includes('legacy-cross-draft')))).toBe(false);
  expect(requests.filter(r => r.method !== 'GET')).toHaveLength(0);
});

test('列表移除已打开稿件后刷新关闭详情、图片预览和发布记录，不保留旧素材', async ({ page }) => {
  const { requests, withdrawDraft } = await fixture(page);
  await page.getByRole('menuitem', { name: '发布管理' }).click();
  await page.getByRole('button', { name: '小红书独立发布稿', exact: false }).click(); const detail = page.getByRole('dialog', { name: '发布稿详情' });
  await expect(detail.getByRole('img', { name: '素材 1', exact: true })).toBeVisible();
  await detail.getByRole('button', { name: '记录已发布' }).click(); const record = page.getByRole('dialog', { name: '记录发布结果' });
  await record.getByRole('textbox', { name: '已发布作品链接' }).fill('https://www.xiaohongshu.com/explore/pending-result');
  // Keep both overlays open to reproduce the keepalive cache boundary.
  await detail.getByRole('img', { name: '素材 1', exact: true }).dispatchEvent('click');
  const imagePreview = page.getByRole('dialog', { name: '素材 1', exact: true });
  await expect(imagePreview).toBeVisible();
  const listReadsBefore = requests.filter(r => r.path === '/publications' && r.platform === 'xiaohongshu').length;
  withdrawDraft();
  await page.locator('[data-workspace="xiaohongshu"] .data-panel .toolbar').getByRole('button', { name: 'reload', exact: true }).dispatchEvent('click');
  await expect.poll(() => requests.filter(r => r.path === '/publications' && r.platform === 'xiaohongshu').length).toBeGreaterThan(listReadsBefore);
  await expect(detail).not.toBeVisible(); await expect(record).not.toBeVisible(); await expect(imagePreview).not.toBeVisible();
  await expect(page.getByRole('button', { name: '小红书独立发布稿', exact: false })).not.toBeVisible();
  await expect(page.getByText('确认一个仿写版本，创建你的第一篇发布稿', { exact: true })).toBeVisible();
  await expect(page.locator('.publication-page .publication-media img')).not.toBeVisible();
  expect(requests.some(r => /\/publish$/.test(r.path) && r.method === 'POST')).toBe(false);
});

test('关闭登录后账号入口保持独立，不在后台重新发起扫码', async ({ page }) => {
  const { requests } = await fixture(page, false); await page.getByRole('menuitem', { name: '发布管理' }).click(); await page.getByRole('button', { name: '扫码登录小红书' }).click();
  const dialog = page.getByRole('dialog', { name: '扫码登录本人小红书账号' }); await expect(dialog.getByRole('img', { name: '小红书扫码登录二维码' })).toBeVisible();
  await dialog.getByRole('button', { name: '关闭登录', exact: true }).click();
  await expect(dialog).toBeHidden();
  await page.getByRole('menuitem', { name: /采集库/ }).click(); await page.getByRole('menuitem', { name: '发布管理' }).click(); await expect(page.getByRole('button', { name: '扫码登录小红书' })).toBeVisible();
  expect(requests.filter(r => r.path === '/xhs/login' && r.method === 'POST')).toHaveLength(1);
  expect(requests.some(r => /\/publish$/.test(r.path) && r.method === 'POST')).toBe(false);
});

test('取消的发布记录不提交，重新打开不保留原输入', async ({ page }) => {
  const { requests } = await fixture(page); await page.getByRole('menuitem', { name: '发布管理' }).click(); await page.getByRole('button', { name: '记录已发布', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '记录发布结果' }); await dialog.getByRole('textbox', { name: '已发布作品链接' }).fill('https://www.xiaohongshu.com/explore/my-note');
  await dialog.getByRole('button', { name: /取\s*消/ }).click();
  await page.getByRole('menuitem', { name: /采集库/ }).click(); await page.getByRole('menuitem', { name: '发布管理' }).click(); await page.getByRole('button', { name: '记录已发布', exact: true }).click();
  await expect(page.getByRole('dialog', { name: '记录发布结果' }).getByRole('textbox', { name: '已发布作品链接' })).toHaveValue('');
  await page.getByRole('dialog', { name: '记录发布结果' }).getByRole('button', { name: /取\s*消/ }).click();
  await page.getByRole('button', { name: '记录已发布', exact: true }).click();
  await expect(page.getByRole('dialog', { name: '记录发布结果' }).getByRole('textbox', { name: '已发布作品链接' })).toHaveValue('');
  expect(requests.filter(r => r.method !== 'GET')).toHaveLength(0);
});

test('设置显示小红书采集账号与依赖，Cookie 与共用 AI 配置正常保存', async ({ page }) => {
  const { requests } = await fixture(page); await page.getByRole('menuitem', { name: '设置' }).click();
  await expect(page.getByRole('textbox', { name: '小红书 Cookie' })).toBeVisible(); await expect(page.getByText('独立小红书账号', { exact: true })).toBeVisible();
  await page.getByRole('textbox', { name: '小红书 Cookie' }).fill('pending-cookie-kept-in-workspace');
  await page.getByRole('tab', { name: '本地依赖', exact: true }).click(); await expect(page.getByRole('cell', { name: '采集服务', exact: true })).toBeVisible(); await expect(page.getByRole('cell', { name: /抖音/ })).toHaveCount(0);
  await page.getByRole('tab', { name: '共用 AI 服务', exact: true }).click(); await expect(page.locator('.ant-alert-title').getByText('共用 AI 服务', { exact: true })).toBeVisible();
  await page.getByRole('textbox', { name: '仿写文案 模型', exact: true }).fill('my-shared-model'); await page.getByRole('button', { name: '保存设置' }).first().click();
  await expect.poll(() => requests.filter(r => r.path === '/settings' && r.method === 'PATCH').length).toBe(1);
  const saved = requests.find(r => r.path === '/settings' && r.method === 'PATCH')!; expect(saved.platform).toBe('xiaohongshu'); expect(saved.body.cookie).toBe('pending-cookie-kept-in-workspace'); expect(saved.body.providers.text.model).toBe('my-shared-model');
  expect(Object.keys(saved.body.providers)).toEqual(['text']); expect(saved.body).not.toHaveProperty('edge_path');
  await page.getByRole('tab', { name: '账号与采集', exact: true }).click();
  await expect(page.getByRole('textbox', { name: '小红书 Cookie' })).toHaveValue('');
  await page.getByRole('tab', { name: '共用 AI 服务', exact: true }).click();
  await expect(page.getByRole('textbox', { name: '仿写文案 模型', exact: true })).toHaveValue('my-shared-model');
});
