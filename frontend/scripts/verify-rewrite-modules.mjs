import { chromium } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

// Exercise local drafts only; no API writes or paid model requests are allowed.
const base = process.env.XHS_LIVE_URL || 'http://127.0.0.1:8080';
const output = path.resolve('../data/verification/edge-rewrite-modules-20261010');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ channel: 'msedge', headless: true });
try {
  const context = await browser.newContext({ viewport: { width: 1536, height: 1000 } });
  await context.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'rewrites');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true');
  });
  const page = await context.newPage(); const errors = []; const writes = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/v1/**', route => {
    if (!['GET', 'HEAD'].includes(route.request().method())) {
      writes.push(new URL(route.request().url()).pathname); return route.abort();
    }
    return route.continue();
  });
  await page.goto(base);
  const workspace = page.locator('[data-workspace="xiaohongshu"]');
  await workspace.getByRole('textbox', { name: '仿写正文' }).waitFor();
  await workspace.getByRole('tab', { name: '视频仿写', exact: true }).click();
  const original = await workspace.getByRole('textbox', { name: '视频口播脚本' }).inputValue();
  assert(original.trim(), 'Missing default narration');
  await workspace.getByRole('tab', { name: '文案编辑', exact: true }).click();
  await workspace.getByRole('textbox', { name: '仿写标题' }).fill('独立模块验证');
  await workspace.getByRole('textbox', { name: '仿写正文' }).fill('仅在当前测试浏览器中编辑，不保存到数据库。');
  await workspace.getByRole('tab', { name: '图片仿写', exact: true }).click();
  assert(await workspace.getByRole('button', { name: '新建图片方案', exact: true }).isEnabled(), 'Copy edits block images');
  assert.equal(await workspace.getByText('请先保存文案编辑，再新建或应用图片仿写').count(), 0);
  for (const width of [1536, 825, 597]) {
    await page.setViewportSize({ width, height: 751 });
    await page.screenshot({ path: path.join(output, `independent-images-${width}.png`) });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `Overflow at ${width}`);
  }
  await workspace.getByRole('tab', { name: '视频仿写', exact: true }).click();
  assert.equal(await workspace.getByRole('textbox', { name: '视频口播脚本' }).inputValue(), original);
  await workspace.getByRole('textbox', { name: '视频口播脚本' }).fill('独立视频口播编辑，不调用生成服务。');
  assert(await workspace.getByRole('button', { name: '打开视频仿写', exact: true }).isEnabled(), 'Copy edits block video');
  await workspace.getByRole('button', { name: '打开视频仿写', exact: true }).click();
  await page.getByRole('button', { name: '新建制作', exact: true }).waitFor();
  await page.getByRole('button', { name: '完成', exact: true }).click();
  await workspace.getByRole('tab', { name: '文案编辑', exact: true }).click();
  assert.equal(await workspace.getByRole('textbox', { name: '仿写标题' }).inputValue(), '独立模块验证');
  assert.equal(await workspace.getByRole('textbox', { name: '仿写正文' }).inputValue(), '仅在当前测试浏览器中编辑，不保存到数据库。');
  await workspace.getByRole('tab', { name: '视频仿写', exact: true }).click();
  assert.equal(await workspace.getByRole('textbox', { name: '视频口播脚本' }).inputValue(), '独立视频口播编辑，不调用生成服务。');
  await page.screenshot({ path: path.join(output, 'independent-video-597.png') });
  assert.equal(writes.length, 0); assert.deepEqual(errors, []);
  const checks = { browser: 'Microsoft Edge', widths: [1536, 825, 597], originalNarrationPresent: true, copyAndVideoDraftsPreserved: true, imagesEnabledWithUnsavedCopy: true, writes: writes.length, pageErrors: errors.length };
  await writeFile(path.join(output, 'live-checks.json'), JSON.stringify(checks, null, 2));
  console.log(JSON.stringify(checks));
  await context.close();
} finally { await browser.close(); }
