import { expect, test } from '@playwright/test';

const member = {
  id: 'member-fixture', username: 'writer', display_name: '创作者', role: 'member', enabled: true,
  must_change_password: false, created_at: '2026-10-18T00:00:00Z', daily_request_limit: 100, max_concurrent: 2,
};

test('a member signs in with a rotated CSRF and no stored authentication token', async ({ page }) => {
  let loginBody: unknown;
  let loginCSRF: string | undefined;
  await page.route('**/api/v1/**', async route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/auth/me')) return route.fulfill({ status: 401, json: { error: '请先登录' } });
    if (path.endsWith('/session')) return route.fulfill({ json: { csrf_token: 'guest-csrf' } });
    if (path.endsWith('/auth/login')) {
      loginBody = request.postDataJSON(); loginCSRF = request.headers()['x-csrf-token'];
      return route.fulfill({ json: { user: member, csrf_token: 'signed-in-csrf' } });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/');
  await expect(page.getByRole('heading', { name: '登录你的工作台' })).toBeVisible();
  await page.getByLabel('用户名', { exact: true }).fill('writer');
  await page.getByLabel('密码', { exact: true }).fill('fixture-password-123');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('heading', { name: '添加你的第一个工作区' })).toBeVisible();
  expect(loginBody).toEqual({ username: 'writer', password: 'fixture-password-123' });
  expect(loginCSRF).toBe('guest-csrf');
  const storage = await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }));
  expect(storage).not.toContain('csrf'); expect(storage).not.toContain('fixture-password');
});

test('first login requires a valid new password before opening the workspace', async ({ page }) => {
  let changed: Record<string, string> | undefined;
  let mutationCSRF: string | undefined;
  await page.route('**/api/v1/**', async route => {
    const request = route.request(); const path = new URL(request.url()).pathname;
    if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { ...member, must_change_password: true }, csrf_token: 'signed-in-csrf' } });
    if (path.endsWith('/auth/password')) {
      changed = request.postDataJSON(); mutationCSRF = request.headers()['x-csrf-token'];
      return route.fulfill({ json: { user: member, csrf_token: 'rotated-csrf' } });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/');
  await expect(page.getByRole('heading', { name: '设置你的新密码' })).toBeVisible();
  await page.getByLabel('当前密码', { exact: true }).fill('initial-fixture-password');
  await page.getByLabel('新密码', { exact: true }).fill('short');
  await page.getByLabel('确认新密码', { exact: true }).fill('short');
  await page.getByRole('button', { name: '保存密码并进入' }).click();
  await expect(page.getByText('请使用至少 12 个字符的新密码')).toBeVisible();
  expect(changed).toBeUndefined();
  await page.getByLabel('新密码', { exact: true }).fill('new-fixture-password-123');
  await page.getByLabel('确认新密码', { exact: true }).fill('new-fixture-password-123');
  await page.getByRole('button', { name: '保存密码并进入' }).click();
  await expect(page.getByRole('heading', { name: '添加你的第一个工作区' })).toBeVisible();
  expect(changed).toEqual({ current_password: 'initial-fixture-password', new_password: 'new-fixture-password-123' });
  expect(mutationCSRF).toBe('signed-in-csrf');
});

test('management login uses its own realm and fits a narrow viewport', async ({ page }) => {
  const calls: string[] = [];
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()); calls.push(url.pathname + url.search);
    if (url.pathname.endsWith('/auth/me')) return route.fulfill({ status: 401, json: { error: '请先登录' } });
    if (url.pathname.endsWith('/session')) return route.fulfill({ json: { csrf_token: 'admin-guest-csrf' } });
    return route.fulfill({ status: 401, json: { error: '用户名或密码错误' } });
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/admin');
  await expect(page.getByRole('heading', { name: '登录管理控制台' })).toBeVisible();
  await page.getByLabel('用户名', { exact: true }).fill('administrator');
  await page.getByLabel('密码', { exact: true }).fill('fixture-password-123');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('用户名或密码错误');
  expect(calls).toContain('/api/v1/session?realm=admin');
  expect(calls).toContain('/api/v1/admin/auth/login');
  expect(calls).not.toContain('/api/v1/auth/me');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
});

test('member settings expose the personal cookie and keep platform model configuration read only', async ({ page }) => {
  const writes: Record<string, unknown>[] = [];
  let configured = false;
  await page.addInitScript(() => localStorage.setItem('workbench.xiaohongshu.page', 'settings'));
  await page.route('**/api/v1/**', async route => {
    const request = route.request(); const endpoint = new URL(request.url()).pathname.replace('/api/v1', '');
    if (endpoint === '/auth/me') return route.fulfill({ json: { user: member, csrf_token: 'member-csrf' } });
    if (endpoint === '/auth/logout') return route.fulfill({ json: {} });
    if (endpoint === '/workspaces') return route.fulfill({ json: { items: [{ platform: 'xiaohongshu' }] } });
    if (endpoint === '/xhs/account') return route.fulfill({ json: { status: 'logged_out', dependency_ready: false } });
    if (endpoint === '/settings') {
      if (request.method() === 'PATCH') {
        expect(request.headers()['x-csrf-token']).toBe('member-csrf');
        const body = request.postDataJSON(); writes.push(body); configured = !!body.cookie;
      }
      const provider = { model: 'configured-model', configured: true, api_key: 'provider-secret-must-never-be-rendered', base_url: 'https://private-endpoint.invalid' };
      return route.fulfill({ json: { cookie_configured: configured, providers: Object.fromEntries(['text', 'vision', 'image', 'video', 'ocr', 'asr'].map(key => [key, provider])) } });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/');
  await expect(page.getByRole('heading', { name: '账户设置', exact: true })).toBeVisible();
  await expect(page.getByText('由管理员统一配置', { exact: true })).toBeVisible();
  await expect(page.getByText('模型请求并发上限', { exact: true })).toBeVisible();
  expect(await page.locator('body').innerText()).not.toContain('provider-secret-must-never-be-rendered');
  expect(await page.locator('body').innerText()).not.toContain('private-endpoint.invalid');
  await expect(page.getByRole('textbox', { name: /API 地址|FFmpeg|采集服务地址/ })).toHaveCount(0);
  await page.getByLabel('小红书 Cookie', { exact: true }).fill('personal-cookie-fixture');
  await page.getByRole('button', { name: /保存 Cookie/ }).click();
  await expect.poll(() => writes.length).toBe(1);
  await expect(page.getByLabel('小红书 Cookie', { exact: true })).toHaveValue('');
  await page.getByRole('button', { name: '清除 Cookie', exact: true }).click();
  await page.getByRole('button', { name: '确认清除采集 Cookie', exact: true }).click();
  await expect.poll(() => writes.length).toBe(2);
  expect(writes).toEqual([{ cookie: 'personal-cookie-fixture' }, { cookie: '' }]);
  await page.getByRole('button', { name: '工作台账户菜单', exact: true }).click();
  await page.getByRole('menuitem', { name: /退出登录/ }).click();
  await expect(page.getByRole('heading', { name: '登录你的工作台' })).toBeVisible();
});

test('an expired member session returns to login without repeated anonymous failures', async ({ page }) => {
  let initialChecks = 0;
  await page.route('**/api/v1/**', async route => {
    const endpoint = new URL(route.request().url()).pathname.replace('/api/v1', '');
    if (endpoint === '/auth/me') { initialChecks++; return route.fulfill({ json: { user: member, csrf_token: 'expired-csrf' } }); }
    return route.fulfill({ status: 401, json: { error: '会话已失效' } });
  });
  await page.goto('/');
  await expect(page.getByRole('heading', { name: '登录你的工作台' })).toBeVisible();
  await expect(page.getByText('会话已失效，请重新登录', { exact: true })).toBeVisible();
  expect(initialChecks).toBeLessThanOrEqual(2);
});
