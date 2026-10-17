import { chromium } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

// Retries only explicitly selected collection jobs, never publishing or AI jobs.
const ids = (process.env.COLLECTION_JOB_IDS || '').split(',').filter(Boolean);
assert(ids.length > 0 && ids.length <= 2, 'Select one or two failed collection jobs explicitly');
const base = process.env.XHS_LIVE_URL || 'http://127.0.0.1:8080';
const output = path.resolve(process.env.VERIFICATION_DIR || '../data/verification/edge-collection-recovery-20261011');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ channel: 'msedge', headless: true });
try {
  const context = await browser.newContext({ viewport: { width: 1536, height: 1000 } });
  await context.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'collections');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true');
  });
  const page = await context.newPage(); const errors = []; const writes = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/v1/**', async route => {
    const request = route.request(); const pathname = new URL(request.url()).pathname;
    if (!['GET', 'HEAD'].includes(request.method())) {
      writes.push(pathname);
      const collectionRetry = ids.some(id => pathname === `/api/v1/jobs/${id}/retry`);
      const collectorCheck = pathname === '/api/v1/settings/test' && request.postDataJSON()?.capability === 'collector';
      if (request.method() !== 'POST' || !collectionRetry && !collectorCheck) return route.abort();
    }
    return route.continue();
  });
  const before = await context.request.get(`${base}/api/v1/jobs?platform=xiaohongshu`);
  assert(before.ok()); const initial = (await before.json()).items;
  const chosen = ids.map(id => initial.find(job => job.id === id));
  assert(chosen.every(job => job?.kind === 'collect' && ['failed', 'succeeded'].includes(job.state)), 'Select failed collection jobs or completed jobs for read-only rechecking');
  assert.equal(new Set(chosen.map(job => job.entity_id)).size, ids.length, 'Do not retry duplicate attempts for one note');
  const retryIDs = chosen.filter(job => job.state === 'failed').map(job => job.id);
  await page.goto(base);
  await page.getByRole('button', { name: /后台任务/ }).click();
  const drawer = page.getByRole('dialog', { name: '小红书后台任务' });
  for (const id of retryIDs) {
    const card = drawer.locator('.job-card').filter({ has: page.getByText(`任务 ${id}`, { exact: true }) });
    await card.getByRole('button', { name: '重试此任务', exact: true }).click();
    await page.waitForTimeout(500);
  }
  const deadline = Date.now() + 180_000; let jobs = [];
  let last = '';
  while (Date.now() < deadline) {
    const response = await context.request.get(`${base}/api/v1/jobs?platform=xiaohongshu`);
    assert(response.ok()); const all = (await response.json()).items;
    jobs = ids.map(id => all.find(job => job.id === id));
    const state = jobs.map(job => `${job.id.slice(0, 8)}:${job.state}:${job.phase}`).join(',');
    if (state !== last) { console.log(state); last = state; }
    if (jobs.every(job => !['queued', 'running'].includes(job.state))) break;
    await page.waitForTimeout(2000);
  }
  const notes = [];
  for (const job of jobs) {
    const response = await context.request.get(`${base}/api/v1/collections/${job.entity_id}?platform=xiaohongshu`);
    assert(response.ok()); const detail = await response.json();
    const note = detail.note;
    let previews = 0;
    for (const asset of detail.assets || []) {
      if (asset.status !== 'ready') continue;
      const file = await context.request.get(`${base}/api/v1/assets/${asset.id}/file?platform=xiaohongshu`);
      assert(file.ok(), 'Persisted media cannot be previewed');
      assert((await file.body()).length > 0, 'Media file is empty'); previews++;
    }
    notes.push({ id: job.entity_id, jobState: job.state, phase: job.phase, error: job.error || '', titlePresent: !!note?.title,
      bodyPresent: !!note?.body, metadata: note?.metadata_status, media: note?.media_status, previews });
  }
  await drawer.locator('.ant-drawer-close').click();
  await page.screenshot({ path: path.join(output, 'collections-recovered-edge.png') });
  await page.getByRole('menuitem', { name: /设置/ }).click();
  const [tested] = await Promise.all([
    page.waitForResponse(response => response.url().includes('/api/v1/settings/test') && response.request().method() === 'POST'),
    page.getByRole('button', { name: /检测采集服务/ }).click(),
  ]);
  assert(tested.ok() && (await tested.json()).ok === true, 'Collector connection check failed');
  await page.screenshot({ path: path.join(output, 'collector-connection-edge.png') });
  const checks = { browser: 'Microsoft Edge', notes, retryRequests: writes.filter(item => item.endsWith('/retry')).length, collectorConnection: true, pageErrors: errors };
  await writeFile(path.join(output, 'live-checks.json'), JSON.stringify(checks, null, 2));
  console.log(JSON.stringify(checks));
  assert(jobs.every(job => job.state === 'succeeded'), 'Some collection jobs remain unsuccessful');
  assert(notes.every(note => note.metadata === 'succeeded' && note.media === 'succeeded' && note.previews > 0));
  assert.equal(writes.length, retryIDs.length + 1); assert.deepEqual(errors, []);
  await context.close();
} finally { await browser.close(); }
