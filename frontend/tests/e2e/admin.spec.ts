import { expect, test, type Page } from '@playwright/test';
import path from 'node:path';

const when = '2026-10-18T10:00:00+08:00';
const admin = { id: 'admin-1', username: 'admin', display_name: '工作台管理员', role: 'admin', enabled: true, must_change_password: false, created_at: when, daily_request_limit: 0, max_concurrent: 2, daily_requests: 0 };
const member = { ...admin, id: 'user-1', username: 'creator', display_name: '内容创作员', role: 'member', daily_request_limit: 100, daily_requests: 3 };
const totals = { requests: 3, reported: 2, missing: 1, failed: 0, pending: 0, unknown: 0, input_tokens: 8000, output_tokens: 2000, total_tokens: 10000, cached_tokens: 1000, reasoning_tokens: 100, input_reported: 2, output_reported: 2, cached_reported: 1, reasoning_reported: 1, estimated_cny: .032, priced: 2, images: 0, video_seconds: 0 };
async function fixture(page: Page) {
  const writes: { path: string; method: string; body: Record<string, unknown> }[] = []; const paths: string[] = []; const queries: URLSearchParams[] = [];
  const users = [admin, member]; const model = { base_url: 'https://provider.example/v1', model: 'qwen-plus', workspace_id: 'team-a', configured: true, key_mask: '***1234' };
  const config = { providers: Object.fromEntries(['text', 'vision', 'ocr', 'asr', 'image', 'video'].map(key => [key, { ...model }])), collector_url: 'http://127.0.0.1:5556', ffmpeg_path: '', ffprobe_path: '', node_path: '' };
  const record = { id: 'usage-1', created_at: when, user_id: member.id, username: member.username, capability: 'text', operation: 'rewrite', provider: model.base_url, model: model.model, credential_id: 'masked-id', status: 'success', input_tokens: 8000, output_tokens: 2000, total_tokens: 10000, cached_tokens: 1000, reasoning_tokens: 100, estimated_cny: .032 };
  const billing = { summary: totals, timeline: [{ ...totals, key: '2026-10-18' }], models: [{ ...totals, key: model.model }], capabilities: [{ ...totals, key: 'text' }], users: [{ ...totals, key: member.username }], user_options: [member], items: [record], total: 1, page: 1, page_size: 20, hourly: false, started_at: when, model_options: [{ model: model.model, provider: model.base_url }], prices: [] as unknown[] };
  await page.route('**/api/v1/**', async route => {
    const request = route.request(); const url = new URL(request.url()); const endpoint = url.pathname.replace('/api/v1', ''); paths.push(endpoint);
    const json = (data: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) });
    if (request.method() !== 'GET') { expect(request.headers()['x-csrf-token']).toBe('admin-token'); writes.push({ path: endpoint, method: request.method(), body: request.postData() ? request.postDataJSON() : {} }); }
    if (endpoint === '/admin/auth/me') return json({ user: admin, csrf_token: 'admin-token' });
    if (endpoint === '/session') return json({ csrf_token: 'admin-token' });
    if (endpoint === '/admin/overview') return json({ users: 2, enabled_users: 2, requests: 3, tokens: 10000, estimated_cny: .032, pending_jobs: 2, failed_jobs: 1, unreported: 1, per_user: [{ user_id: member.id, username: member.username, display_name: member.display_name, requests: 3, tokens: 10000, estimated_cny: .032, daily_requests: 3, daily_request_limit: 100 }] });
    if (endpoint === '/admin/users' && request.method() === 'POST') { const data = request.postDataJSON(); const user = { ...member, ...data, id: 'new-user', daily_requests: 0, must_change_password: true }; users.push(user); return json({ user, temporary_password: 'Generated-pass123!' }); }
    if (endpoint === '/admin/users') return json({ items: users, total: users.length });
    if (/^\/admin\/users\/[^/]+\/reset-password$/.test(endpoint)) return json({ user: member, temporary_password: 'Reset-pass1234!' });
    if (/^\/admin\/users\/[^/]+$/.test(endpoint)) { Object.assign(users.find(u => u.id === endpoint.split('/').at(-1)) || {}, request.postDataJSON()); return json({ ok: true }); }
    if (endpoint === '/admin/models') { if (request.method() === 'PATCH') { const body = request.postDataJSON(); if (body.providers) for (const [key, changes] of Object.entries(body.providers)) Object.assign(config.providers[key], changes); } return json(config); }
    if (endpoint === '/admin/billing/prices') { billing.prices = request.postDataJSON().items; return json({ items: billing.prices }); }
    if (endpoint === '/admin/billing/export') return route.fulfill({ contentType: 'text/csv; charset=utf-8', headers: { 'Content-Disposition': 'attachment; filename="token-usage.csv"' }, body: '用户名,输入Token\ncreator,8000\n' });
    if (endpoint === '/admin/billing') { queries.push(url.searchParams); return json(billing); }
    if (endpoint === '/admin/jobs') return json({ items: [{ id: 'safe-job', kind: 'rewrite', state: 'failed', phase: 'generating', progress: 0, error: '连接失败', user_id: member.id, username: member.username, created_at: when, updated_at: when, attempts: 1 }, { id: 'unknown-job', kind: 'video_rewrite', state: 'submission_unknown', phase: 'submitting', progress: 30, user_id: member.id, username: member.username, created_at: when, updated_at: when, attempts: 1 }, { id: 'publish-job', kind: 'publish', state: 'failed', phase: 'uploading', progress: 0, user_id: member.id, username: member.username, created_at: when, updated_at: when, attempts: 1 }, { id: 'queued-job', kind: 'rewrite', state: 'queued', phase: 'queued', progress: 0, user_id: member.id, username: member.username, created_at: when, updated_at: when, attempts: 0 }], total: 4 });
    if (/\/jobs\/[^/]+\/(retry|cancel)$/.test(endpoint)) return json({ ok: true });
    if (endpoint === '/admin/audit') return json({ items: [{ id: 'audit-1', created_at: when, actor_name: admin.username, action: 'user.create', target: member.username, result: 'success' }], total: 1 });
    return json({ error: `Unexpected API ${endpoint}` }, 500);
  });
  await page.goto('/admin'); await expect(page.getByRole('heading', { name: '管理总览', exact: true })).toBeVisible();
  return { writes, paths, queries };
}

test('管理员独立布局、六个模块与窄屏布局可用', async ({ page }) => {
  const { paths } = await fixture(page);
  await expect(page.locator('.admin-stat').nth(2)).toContainText('10,000');
  await expect(page.locator('.admin-user-usage-row')).toContainText('内容创作员'); await page.screenshot({ path: path.resolve('../data/verification/admin-desktop.png'), fullPage: true });
  await page.getByRole('button', { name: '操作记录', exact: true }).click(); await expect(page.getByRole('cell', { name: '创建账号', exact: true })).toBeVisible();
  for (const width of [1280, 644, 390]) {
    await page.setViewportSize({ width, height: 860 }); await page.getByRole('button', { name: '管理总览', exact: true }).click();
    await expect(page.locator('.admin-stat')).toHaveCount(4);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  }
  await page.screenshot({ path: path.resolve('../data/verification/admin-mobile.png'), fullPage: true });
  expect(paths.every(value => value.startsWith('/admin/') || value === '/session')).toBe(true);
});

test('创建和编辑用户、一次性临时密码与自身保护', async ({ page }) => {
  const { writes } = await fixture(page); await page.getByRole('button', { name: '用户管理', exact: true }).click();
  const adminRow = page.locator('tr[data-row-key="admin-1"]'); await expect(adminRow.getByRole('switch')).toBeDisabled(); await expect(adminRow.getByRole('button', { name: '重置密码' })).toBeDisabled();
  await page.getByRole('button', { name: '创建账号', exact: true }).click();
  const dialog = page.getByRole('dialog'); await dialog.getByLabel('登录账号', { exact: true }).fill('creator02'); await dialog.getByLabel('显示名称', { exact: true }).fill('第二创作员');
  await dialog.getByLabel('每日模型调用上限', { exact: true }).fill('20'); await dialog.getByRole('button', { name: '创建账号', exact: true }).click();
  const credentials = page.getByRole('dialog', { name: '账号已准备就绪' }); await expect(credentials).toContainText('Generated-pass123!'); await expect(credentials).toContainText('临时密码只在此处显示一次');
  await credentials.getByRole('button', { name: '已妥善保存', exact: true }).click(); await expect(page.getByText('Generated-pass123!', { exact: true })).toBeHidden();
  expect(writes.find(w => w.path === '/admin/users')?.body).toMatchObject({ username: 'creator02', role: 'member', daily_request_limit: 20, max_concurrent: 2 });
  const memberRow = page.locator('tr[data-row-key="user-1"]'); await memberRow.getByRole('button', { name: '编辑', exact: true }).click();
  await dialog.getByLabel('每日模型调用上限', { exact: true }).fill('50'); await dialog.getByRole('button', { name: '保存设置', exact: true }).click(); await expect(dialog).toBeHidden();
  expect(writes.at(-1)?.body).toMatchObject({ daily_request_limit: 50 });
});

test('模型密钥保留和显式清除，不向页面暴露保存的密钥', async ({ page }) => {
  const { writes } = await fixture(page); await page.getByRole('button', { name: '模型配置', exact: true }).click();
  const input = page.getByRole('textbox', { name: '文案与提示词 API 地址', exact: true }); await input.fill('https://new.example/v1');
  await page.getByRole('button', { name: '保存配置', exact: true }).click(); await expect(page.getByText('模型与运行配置已更新', { exact: true })).toBeVisible();
  expect(writes.at(-1)?.body).toEqual({ providers: { text: { base_url: 'https://new.example/v1', workspace_id: 'team-a' } } });
  const card = page.locator('.admin-model-card').filter({ hasText: '文案与提示词' }); await card.getByRole('button', { name: '清除密钥', exact: true }).click(); await page.getByRole('button', { name: '标记清除', exact: true }).click();
  await expect(card).toContainText('待清除'); await page.getByRole('button', { name: '保存配置', exact: true }).click();
  await expect.poll(() => writes.at(-1)?.body).toEqual({ providers: { text: { api_key: '__CLEAR__', workspace_id: 'team-a' } } });
});

test('管理员按用户查看用量、编辑单价并导出，窄屏图表无页面溢出', async ({ page }) => {
  const { writes, queries } = await fixture(page); await page.getByRole('button', { name: '用量与计费', exact: true }).click();
  await expect(page.locator('tr[data-row-key="usage-1"]')).toContainText('creator'); await page.getByRole('combobox', { name: '筛选用户', exact: true }).click();
  await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content').filter({ hasText: '内容创作员' }).click(); await expect.poll(() => queries.at(-1)?.get('user_id')).toBe(member.id);
  await page.getByRole('radio', { name: '用户', exact: true }).locator('..').click(); await expect(page.locator('.billing-distribution-list')).toContainText('creator');
  await page.getByRole('button', { name: '单价设置', exact: true }).click(); const dialog = page.getByRole('dialog'); await dialog.getByRole('combobox', { name: '计费模型', exact: true }).click(); await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content').first().click();
  await dialog.getByRole('spinbutton', { name: '输入单价', exact: true }).fill('2'); await dialog.getByRole('spinbutton', { name: '输出单价', exact: true }).fill('8'); await dialog.getByRole('button', { name: '保存单价', exact: true }).click(); await expect(dialog).toBeHidden();
  expect(writes.at(-1)?.path).toBe('/admin/billing/prices');
  const pending = page.waitForEvent('download'); await page.getByRole('button', { name: '导出明细', exact: true }).click(); expect((await pending).suggestedFilename()).toBe('token-usage.csv');
  await page.setViewportSize({ width: 390, height: 850 }); await expect(page.locator('.billing-metric-primary')).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  await page.screenshot({ path: path.resolve('../data/verification/admin-billing-mobile.png'), fullPage: true });
});

test('只提供安全任务操作，不重试发布与结果不明任务', async ({ page }) => {
  const { writes } = await fixture(page); await page.getByRole('button', { name: '后台任务', exact: true }).click();
  await expect(page.locator('tr[data-row-key="user-1:unknown-job"]').getByRole('button', { name: '重试', exact: true })).toHaveCount(0);
  await expect(page.locator('tr[data-row-key="user-1:publish-job"]').getByRole('button', { name: '重试', exact: true })).toHaveCount(0);
  await page.locator('tr[data-row-key="user-1:safe-job"]').getByRole('button', { name: '重试', exact: true }).click(); await page.locator('.ant-popconfirm').getByRole('button', { name: /重\s*试/ }).click();
  await expect.poll(() => writes.at(-1)?.path).toBe('/admin/users/user-1/jobs/safe-job/retry');
  await page.locator('tr[data-row-key="user-1:queued-job"]').getByRole('button', { name: '取消', exact: true }).click(); await page.locator('.ant-popconfirm').getByRole('button', { name: '取消任务', exact: true }).click();
  await expect.poll(() => writes.at(-1)?.path).toBe('/admin/users/user-1/jobs/queued-job/cancel');
});
