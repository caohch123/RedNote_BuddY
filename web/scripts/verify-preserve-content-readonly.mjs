import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';

// Read-only checks against the deployed local service. Every project mutation
// is blocked; mock end-to-end tests exercise the actual editing workflow.
const base = 'http://127.0.0.1:8080';
const project = '3e15dd3277739627464574280ad8981c';
const schemeID = 'dca73bfb92a92807939c5e9ead0235de';
const folder = path.resolve('../data/verification/preserve-content-live-20261012');
await mkdir(folder, { recursive: true });
const browser = await chromium.launch({ channel: 'msedge', headless: true });
try {
  const context = await browser.newContext();
  await context.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'rewrites');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true');
  });
  const detail = async () => {
    const response = await context.request.get(`${base}/api/v1/rewrites/${project}?platform=xiaohongshu`);
    assert(response.ok()); return response.json();
  };
  const before = await detail();
  const unit = before.rewrite.image_rewrites.find(u => u.id === schemeID);
  assert(unit);
  const index = before.rewrite.image_rewrites.indexOf(unit);
  const capabilities = await context.request.get(`${base}/api/v1/image-capabilities?platform=xiaohongshu`).then(r => r.json());
  assert.equal(capabilities.prompt_rules_version, 5);
  const page = await context.newPage();
  const blocked = [], errors = [], checks = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/v1/**', async route => {
    if (['GET', 'HEAD'].includes(route.request().method())) return route.continue();
    blocked.push(new URL(route.request().url()).pathname);
    return route.abort();
  });
  await page.goto(base);
  const workspace = page.locator('[data-workspace="xiaohongshu"]');
  await workspace.getByRole('menuitem', { name: /仿写库/ }).click();
  await workspace.locator('.rewrite-list-item').filter({ hasText: before.rewrite.title }).first().click();
  await workspace.getByRole('tab', { name: '图片仿写', exact: true }).click();
  await workspace.getByRole('combobox', { name: '图片方案' }).click();
  await page.getByRole('option', { name: `方案 ${index + 1}`, exact: true }).click();
  await workspace.getByRole('button', { name: '1. 选图与特征', exact: true }).click();
  for (const width of [1536, 825, 597]) {
    await page.setViewportSize({ width, height: 1000 });
    await workspace.getByRole('button', { name: '保留主图内容', exact: true }).scrollIntoViewIfNeeded();
    assert(await workspace.getByRole('button', { name: '保留主图内容', exact: true }).isVisible());
    assert(await workspace.getByRole('button', { name: '自由组合特征', exact: true }).isVisible());
    const metrics = await workspace.evaluate(el => {
      const rect = el.getBoundingClientRect();
      const footer = el.querySelector('.image-workflow-actions')?.getBoundingClientRect();
      return { width: rect.width, scrollWidth: el.scrollWidth, footerBottom: footer?.bottom, viewportHeight: innerHeight };
    });
    assert(metrics.scrollWidth <= metrics.width + 2, `workspace overflow at ${width}`);
    assert(Math.abs(metrics.footerBottom - metrics.viewportHeight) <= 2, `action dock not aligned at ${width}`);
    await page.screenshot({ path: path.join(folder, `edge-${width}.png`) });
    checks.push({ width, ...metrics });
  }
  await workspace.getByRole('button', { name: '2. 审阅提示词', exact: true }).click();
  assert(await workspace.getByRole('button', { name: `确认并生成 ${unit.output.count} 张` }).isDisabled());
  assert(await workspace.getByText('旧提示词需按当前特征重新编排').isVisible());
  const after = await detail();
  assert.deepEqual(after.rewrite, before.rewrite);
  assert.equal(blocked.length, 0, 'opening or navigation attempted a mutation');
  assert.equal(errors.length, 0);
  const record = { checked_at: new Date().toISOString(), browser: 'Microsoft Edge', project_id: project, scheme_id: schemeID, revision: unit.revision, rules_version: capabilities.prompt_rules_version, historical_results: unit.results.length, unchanged_project: true, blocked_writes: blocked, page_errors: errors, checks };
  await writeFile(path.join(folder, 'checks.json'), JSON.stringify(record, null, 2));
  process.stdout.write(JSON.stringify(record, null, 2));
} finally { await browser.close(); }
