import { expect, test, type Page } from '@playwright/test';

const qr = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aJEkAAAAASUVORK5CYII=';
const account = { id: 'owner-account', nickname: '本人测试账号', red_id: 'test-owner' };
async function fixtures(page: Page, options: { loggedIn?: boolean; loginStates?: string[]; publishStatus?: string; expiryMs?: number; publishJobState?: string } = {}) {
  let loggedIn = !!options.loggedIn; let loginPOSTs = 0; let loginGETs = 0; let previews = 0; let publishPOSTs = 0;
  const publication = { id: 'p1', rewrite_id: 'r1', version_id: 'v1', title: '待发布的确认笔记', body: '已经人工确认的完整正文。', topics: ['学习'],
    asset_ids: ['image-second', 'image-first'], status: options.publishStatus || 'ready', created_at: '2026-10-07T12:00:00Z' };
  const mutations: { path: string; body: any; csrf?: string }[] = [];
  const assets = ['image-second', 'image-first'].map((id, position) => ({ id, kind: 'image', mime: 'image/png', status: 'ready', position, width: 1200, height: 1600, duration: 0, size: 100, sha256: 'fixture-sha' }));
  await page.route('**/api/v1/**', async route => {
    const req = route.request(); const path = new URL(req.url()).pathname.replace('/api/v1', ''); const method = req.method(); const csrf = req.headers()['x-csrf-token'];
    const json = (value: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) });
    if (method !== 'GET') { mutations.push({ path, body: req.postDataJSON(), csrf }); if (csrf !== 'csrf-fixture') return json({ error: '缺少 CSRF' }, 403); }
    if (path === '/workspaces') return json({ items: [{ id: 'xiaohongshu', platform: 'xiaohongshu', name: '小红书' }], total: 1 });
    if (path === '/session') return json({ csrf_token: 'csrf-fixture' });
    if (path === '/xhs/account' && method === 'DELETE') { loggedIn = false; return route.fulfill({ status: 204 }); }
    if (path === '/xhs/account') return json({ status: loggedIn ? 'logged_in' : 'logged_out', dependency_ready: true, ...(loggedIn ? { account } : {}) });
    if (path === '/xhs/login' && method === 'POST') { loginPOSTs++; return json({ login_id: `login-${loginPOSTs}` }, 202); }
    if (path.startsWith('/xhs/login/')) {
      const states = options.loginStates || ['waiting_scan', 'scanned', 'logged_in']; const status = states[Math.min(loginGETs++, states.length - 1)];
      if (status === 'logged_in') loggedIn = true;
      return json({ login_id: path.split('/').at(-1), status, qr_image: qr, expires_at: new Date(Date.now() + 60000).toISOString(), ...(status === 'logged_in' ? { account } : {}) });
    }
    if (path === '/publications/p1/publish-preview') {
      previews++; const visibility = req.postDataJSON().visibility;
      return json({ confirmation_token: `confirm-${previews}`, expires_at: new Date(Date.now() + (options.expiryMs || 60000)).toISOString(),
        account, publication_id: 'p1', title: publication.title, body: publication.body, topics: publication.topics, assets, media_type: 'image', visibility });
    }
    if (path === '/publications/p1/publish') { publishPOSTs++; publication.status = 'publishing'; return json({ job_id: 'publish-job-1' }, 202); }
    if (path === '/publications') return json({ items: [publication], total: 1 });
    if (path === '/publications/p1') return json(publication);
    if (path === '/assets') return json({ items: assets, total: 2 });
    if (path.startsWith('/assets/') && path.endsWith('/file')) return route.fulfill({ contentType: 'image/png', body: Buffer.from(qr.split(',')[1], 'base64') });
    if (path === '/jobs' && options.publishJobState) return json({ items: [{ id: 'publishing-job', kind: 'publish', entity_id: 'p1', state: options.publishJobState,
      phase: 'publish', progress: 85, created_at: '2026-10-07T12:00:00Z', updated_at: '2026-10-07T12:00:01Z', attempts: 1 }], total: 1 });
    if (path === '/jobs' || path === '/collections') return json({ items: [], total: 0 });
    return json({ error: `未模拟接口 ${method} ${path}` }, 404);
  });
  await page.goto('/'); await page.getByRole('menuitem', { name: '发布管理' }).click();
  await expect(page.getByRole('button', { name: '待发布的确认笔记', exact: false })).toBeVisible();
  return { counts: () => ({ loginPOSTs, loginGETs, previews, publishPOSTs }), mutations };
}

test('扫码显示真实会话状态，成功后仅本人账号可进入发布确认', async ({ page }) => {
  const fixture = await fixtures(page);
  await expect(page.getByRole('button', { name: '发布到小红书', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: '扫码登录小红书' }).click();
  const dialog = page.getByRole('dialog', { name: '扫码登录本人小红书账号' });
  await expect(dialog.getByRole('img', { name: '小红书扫码登录二维码' })).toBeVisible();
  await expect(dialog.getByText('已扫码，请在手机上确认登录', { exact: true })).toBeVisible();
  await expect(dialog.getByText('登录成功', { exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: '完成登录', exact: true }).click();
  await expect(page.locator('.xhs-account-panel').getByText('本人测试账号', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: '发布到小红书', exact: true })).toBeEnabled();
  expect(fixture.counts().loginPOSTs).toBe(1); expect(fixture.counts().publishPOSTs).toBe(0);
  expect(fixture.mutations.every(m => m.csrf === 'csrf-fixture')).toBe(true);
});

test('过期二维码只在用户刷新时重新创建，关闭弹窗停止轮询', async ({ page }) => {
  const fixture = await fixtures(page, { loginStates: ['waiting_scan', 'expired'] });
  await page.getByRole('button', { name: '扫码登录小红书' }).click();
  const dialog = page.getByRole('dialog', { name: '扫码登录本人小红书账号' });
  await expect(dialog.getByText('二维码已过期，请重新获取', { exact: true })).toBeVisible();
  await page.waitForTimeout(2200); expect(fixture.counts().loginPOSTs).toBe(1);
  await dialog.getByRole('button', { name: '重新获取二维码' }).click();
  await expect.poll(() => fixture.counts().loginPOSTs).toBe(2);
  await dialog.getByRole('button', { name: '关闭登录', exact: true }).click();
  const afterClose = fixture.counts().loginGETs; await page.waitForTimeout(2300);
  expect(fixture.counts().loginGETs).toBe(afterClose); expect(fixture.counts().publishPOSTs).toBe(0);
});

test('发布前核对快照、素材顺序、可见性；勾选且显式确认后只提交一次', async ({ page }) => {
  const fixture = await fixtures(page, { loggedIn: true });
  await page.getByRole('button', { name: '发布到小红书', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '确认发布到小红书' });
  const publish = dialog.getByRole('button', { name: '确认发布到小红书', exact: true });
  await expect(dialog.getByText('本人测试账号', { exact: true })).toBeVisible();
  await expect(dialog.getByText('已经人工确认的完整正文。', { exact: true })).toBeVisible();
  await expect(dialog.getByRole('img', { name: '待发布素材 1', exact: true })).toHaveAttribute('src', '/api/v1/assets/image-second/file?platform=xiaohongshu');
  await expect(dialog.getByRole('img', { name: '待发布素材 2', exact: true })).toHaveAttribute('src', '/api/v1/assets/image-first/file?platform=xiaohongshu');
  await expect(publish).toBeDisabled(); expect(fixture.counts().publishPOSTs).toBe(0);
  await dialog.getByRole('checkbox').check();
  const visibility = dialog.getByRole('combobox', { name: '小红书发布可见性' });
  await visibility.click(); await visibility.press('ArrowDown'); await visibility.press('Enter');
  await expect(dialog.getByRole('checkbox')).not.toBeChecked(); await expect(publish).toBeDisabled();
  await expect(dialog.getByText('仅自己可见', { exact: true }).last()).toBeVisible();
  await page.screenshot({ path: 'test-results/xhs-publish-confirmation.png', fullPage: true });
  await dialog.getByRole('checkbox').check(); await publish.click();
  await expect(dialog).not.toBeVisible(); await expect(page.getByText('发布中', { exact: true })).toBeVisible();
  expect(fixture.counts().publishPOSTs).toBe(1);
  const submit = fixture.mutations.find(m => m.path.endsWith('/publish'))!;
  expect(submit.body).toEqual({ confirmation_token: 'confirm-2' }); expect(submit.csrf).toBe('csrf-fixture');
  expect(fixture.mutations.filter(m => m.path.endsWith('/publish-preview')).map(m => m.body.visibility)).toEqual(['public', 'private']);
  await page.screenshot({ path: 'test-results/xhs-publication.png', fullPage: true });
});

test('发布确认过期禁用提交', async ({ page }) => {
  const fixture = await fixtures(page, { loggedIn: true, expiryMs: 1400 });
  await page.getByRole('button', { name: '发布到小红书', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '确认发布到小红书' });
  await dialog.getByRole('checkbox').check();
  await expect(dialog.getByText('发布确认已过期，请重新检查发布内容')).toBeVisible();
  await expect(dialog.getByRole('button', { name: '确认发布到小红书', exact: true })).toBeDisabled();
  expect(fixture.counts().publishPOSTs).toBe(0);
  await dialog.getByRole('button', { name: /^取\s*消$/ }).click();
});

test('发布结果不明时，任务和稿件均要求人工核对且禁止重复提交', async ({ page }) => {
  const fixture = await fixtures(page, { loggedIn: true, publishStatus: 'submission_unknown', publishJobState: 'submission_unknown' });
  await page.getByRole('button', { name: '后台任务', exact: false }).click();
  const drawer = page.getByRole('dialog', { name: '后台任务' });
  await expect(drawer.getByText('作品发布', { exact: true })).toBeVisible();
  await expect(drawer.getByText('请先在目标平台创作中心核对结果，再到发布管理记录；此任务不会自动重复提交。')).toBeVisible();
  await expect(drawer.getByRole('button', { name: '重试此任务' })).toHaveCount(0);
  await expect(page.getByRole('dialog', { name: '核实视频提交结果' })).not.toBeVisible();
  await drawer.locator('button.ant-drawer-close').click();
  await expect(page.getByText('结果待核实', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: '发布到小红书', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: '待发布的确认笔记', exact: false }).click();
  const detail = page.getByRole('dialog', { name: '发布稿详情' });
  await expect(detail.getByText('提交后未能确认平台结果，已停止重复发布。请先到小红书创作平台查看笔记；确认成功后手动记录作品链接。')).toBeVisible();
  await expect(detail.getByRole('button', { name: '发布到小红书', exact: true })).toBeDisabled();
  expect(fixture.counts().publishPOSTs).toBe(0);
});
