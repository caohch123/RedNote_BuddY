import { chromium, expect } from '@playwright/test';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createFrontendServer } from '../server.mjs';

const root = fileURLToPath(new URL('../../', import.meta.url));
const directory = await mkdtemp(path.join(tmpdir(), 'xhs-separated-'));
const reservation = http.createServer(); await new Promise(resolve => reservation.listen(0, '127.0.0.1', resolve));
const apiAddress = `127.0.0.1:${reservation.address().port}`; await new Promise(resolve => reservation.close(resolve));
const frontend = await createFrontendServer({ apiTarget: `http://${apiAddress}` });
await new Promise(resolve => frontend.listen(0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${frontend.address().port}`;
const executable = path.join(root, 'bin', process.platform === 'win32' ? 'xhs-workbench.exe' : 'xhs-workbench');
const backend = spawn(executable, ['--root', root, '--data-dir', directory, '--addr', apiAddress, '--allowed-origins', base], { windowsHide: true, stdio: ['ignore', 'ignore', 'pipe'] });
let startupError = ''; backend.on('error', error => { startupError = error.message; }); backend.stderr.on('data', () => {});
let browser;
const checks = [];
try {
  let ready = false;
  for (let attempt = 0; attempt < 100; attempt++) {
    if (startupError || backend.exitCode !== null) throw new Error(startupError || 'Temporary API exited');
    try { ready = (await fetch(`${base}/api/v1/session`)).ok; } catch {}
    if (ready) break;
    await new Promise(resolve => setTimeout(resolve, 150));
  }
  if (!ready) throw new Error('Temporary API did not start');
  const apiRoot = await fetch(`http://${apiAddress}/`); if (apiRoot.status !== 404 || !(apiRoot.headers.get('content-type') || '').includes('application/json')) throw new Error('API still serves frontend HTML');
  checks.push('API serves JSON only; frontend serves its own build');
  browser = await chromium.launch({ channel: 'msedge', headless: true });
  const page = await browser.newPage({ viewport: { width: 1365, height: 900 } });
  await page.addInitScript(() => { localStorage.setItem('workbench.workspace', 'xiaohongshu'); localStorage.setItem('workbench.xiaohongshu.page', 'rewrites'); });
  await page.goto(base);
  await page.getByRole('button', { name: '新增空白项目', exact: true }).click();
  await expect(page.locator('.rewrite-list-item.selected')).toContainText('未命名项目');
  await expect(page.getByRole('textbox', { name: '仿写标题', exact: true })).toHaveValue('');
  await page.getByRole('textbox', { name: '仿写标题', exact: true }).fill('独立服务验证');
  await page.getByRole('textbox', { name: '仿写正文', exact: true }).fill('仅存在于临时数据库中的前后端联调内容。');
  await page.getByRole('button', { name: '保存新版本', exact: true }).click();
  await expect(page.getByText(/当前版本 2 ·/)).toBeVisible();
  await page.getByRole('button', { name: '确认此版本', exact: true }).click();
  await expect(page.locator('.rewrite-tab-actions')).toContainText('已确认');
  checks.push('Real frontend → proxy → API → temporary SQLite: create, save, confirm');
  const cors = await page.evaluate(async address => {
    const base = `http://${address}/api/v1`;
    const session = await (await fetch(`${base}/session`)).json();
    const denied = await fetch(`${base}/rewrites?platform=xiaohongshu`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ blank: true }) });
    const checked = await fetch(`${base}/rewrites?platform=xiaohongshu`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf_token }, body: JSON.stringify({ blank: true, note_id: 'invalid-source' }) });
    return { tokenPresent: !!session.csrf_token, denied: denied.status, checked: checked.status };
  }, apiAddress);
  if (!cors.tokenPresent || cors.denied !== 403 || cors.checked !== 400) throw new Error(`Cross-origin/CSRF check failed: ${JSON.stringify(cors)}`);
  checks.push('Browser cross-origin API access, CORS preflight and CSRF enforcement');
  await page.getByRole('tab', { name: '图片仿写', exact: true }).click();
  const primary = page.locator('[data-reference-title="被仿写图"]');
  await primary.getByRole('button', { name: '被仿写图菜单', exact: true }).click();
  const chooser = page.waitForEvent('filechooser');
  await page.locator('.reference-slot-menu').getByRole('menuitem', { name: '上传图片', exact: true }).click();
  await (await chooser).setFiles({ name: 'separation.png', mimeType: 'image/png', buffer: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a2ioAAAAASUVORK5CYII=', 'base64') });
  await expect(primary).toHaveClass(/has-media/);
  await expect.poll(() => primary.locator('img').first().evaluate(image => image.naturalWidth)).toBeGreaterThan(0);
  const mediaReply = await fetch(await primary.locator('img').first().getAttribute('src').then(src => new URL(src, base)));
  if (!mediaReply.ok || !mediaReply.headers.get('content-type')?.startsWith('image/')) throw new Error('Uploaded media could not be fetched through frontend proxy');
  checks.push('Real multipart upload, reference autosave and image loading through proxy');
  await page.reload(); await expect(page.locator('.rewrite-list-item.selected')).toContainText('独立服务验证');
  await expect(page.getByRole('textbox', { name: '仿写正文', exact: true })).toHaveValue('仅存在于临时数据库中的前后端联调内容。');
  await page.screenshot({ path: path.join(root, 'data/verification/separation-ui-20261017.png'), fullPage: true });
  const jobs = await (await fetch(`${base}/api/v1/jobs?platform=xiaohongshu`)).json();
  if (jobs.items.length) throw new Error('Unexpected model or publishing work enqueued');
  checks.push('Reload restores saved content; no generation or publication tasks');
  await writeFile(path.join(root, 'data/verification/separation-live-20261017.json'), JSON.stringify({ passed: true, isolatedDatabase: true, checks }, null, 2));
  console.log(JSON.stringify({ passed: true, checks }, null, 2));
} finally {
  await browser?.close(); frontend.closeAllConnections(); await new Promise(resolve => frontend.close(resolve));
  if (backend.exitCode === null) {
    const exited = once(backend, 'exit');
    if (process.platform === 'win32') { const stop = spawn('taskkill.exe', ['/PID', String(backend.pid), '/T', '/F'], { windowsHide: true, stdio: 'ignore' }); await once(stop, 'exit'); }
    else backend.kill('SIGTERM');
    await exited;
  }
  const resolved = path.resolve(directory);
  if (!resolved.startsWith(path.join(path.resolve(tmpdir()), 'xhs-separated-'))) throw new Error('Cleanup target outside temporary test directory');
  await rm(resolved, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
}
