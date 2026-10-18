import { memberAuthResponse } from './member-auth';
import { expect, test, type Page } from '@playwright/test';

const featureKeys = ['subject', 'action', 'scene', 'composition', 'palette', 'lighting', 'texture', 'text'];

async function fixture(page: Page, options: { empty?: boolean; failInitialization?: boolean } = {}) {
  const writes: string[] = [];
  const note = { id: 'scheme-note', platform: 'xiaohongshu', title: '十四张原图的方案初始化', body: '原笔记正文', author: { name: '测试作者' }, asset_ids: Array.from({ length: 14 }, (_, i) => `source-${i + 1}`), topics: [], tags: [] };
  const version = { id: 'scheme-version', number: 1, title: note.title, body: note.body, script: '', topics: [], asset_ids: [...note.asset_ids], confirmed: false, source_snapshot: note };
  function makeUnit(id: string, primary: string) {
    return { id, workflow_version: 2, version_id: version.id, primary_asset_id: primary, source_asset_ids: [primary], references: [], edit_intent: { mode: 'flexible' }, feature_controls: Object.fromEntries(featureKeys.map(key => [key, { mode: 'image', asset_id: primary }])), feature_sources: Object.fromEntries(featureKeys.map(key => [key, primary])), output: { ratio: '3:4', clarity: 'standard', count: 1 }, text_mode: 'none', prompt: '', analyses: [] as any[], batches: [], results: [], revision: 1, status: 'draft', style: { ratio: '3:4' } };
  }
  const schemes = note.asset_ids.map((id, i) => makeUnit(`scheme-${i + 1}`, id));
  const rewrite = { id: 'scheme-rewrite', note_id: note.id, title: note.title, versions: [version], options: { include_body: true }, image_rewrites: options.empty ? [] as typeof schemes : schemes };
  const assets = note.asset_ids.map((id, position) => ({ id, position, kind: 'image', mime: 'image/png', status: 'ready', owner_id: note.id, width: 960, height: 1280 }));
  let releaseInitialization!: () => void;
  const initializationReady = new Promise<void>(resolve => { releaseInitialization = resolve; });
  let detailReads = 0;
  let initializationFailed = false;
  let manualUnit: ReturnType<typeof makeUnit> | undefined;
  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a3ioAAAAASUVORK5CYII=', 'base64');
  await page.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    for (const platform of ['xiaohongshu']) {
      localStorage.setItem(`workbench.${platform}.page`, 'rewrites');
      localStorage.setItem(`workbench.${platform}.sidebarCollapsed`, 'true');
    }
  });
  await page.route('**/api/v1/**', async route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const request = route.request(); const url = new URL(request.url()); const path = url.pathname.replace('/api/v1', '');
    const json = (value: unknown, status = 200) => route.fulfill({ status, json: value });
    if (request.method() !== 'GET') {
      writes.push(path); expect(request.headers()['x-csrf-token']).toBe('test-csrf');
      if (path === `/rewrites/${rewrite.id}/images/initialize`) {
        if (options.failInitialization && !initializationFailed) { initializationFailed = true; return json({ error: '模拟方案初始化失败' }, 503); }
        await initializationReady; rewrite.image_rewrites = schemes; return json(schemes);
      }
      if (path === `/rewrites/${rewrite.id}/images`) {
        const body = request.postDataJSON();
        expect(body.workflow_version).toBe(2); expect(body.edit_intent.mode).toBe('flexible');
        manualUnit = makeUnit('scheme-manual', body.primary_asset_id);
        return json(manualUnit, 201);
      }
      return json({ error: '方案选择测试不允许其他写入' }, 500);
    }
    if (path === '/session') return json({ csrf_token: 'test-csrf' });
    if (path === '/workspaces') return json({ items: [{ platform: 'xiaohongshu' }], total: 1 });
    if (path === '/rewrites') return json({ items: [rewrite], total: 1 });
    if (path === `/rewrites/${rewrite.id}`) { detailReads++; return json({ rewrite, note, assets, videos: [] }); }
    if (path === '/image-capabilities') return json({ image_configured: true, vision_configured: true, text_configured: true, supported: true, max_count: 4, sizes: [{ ratio: '3:4', clarity: 'standard', width: 960, height: 1280 }] });
    if (path.endsWith('/file')) return route.fulfill({ contentType: 'image/png', body: png });
    if (path === '/style-library') return json({ items: [], categories: [], total: 0 });
    return json({ items: [], total: 0 });
  });
  await page.goto('/');
  await page.getByRole('tab', { name: '图片仿写', exact: true }).click();
  return { writes, schemes, rewrite, releaseInitialization, detailReads: () => detailReads, persistManual: () => { if (manualUnit) rewrite.image_rewrites = [...rewrite.image_rewrites, manualUnit]; } };
}

async function expectPrimary(page: Page, number: number) {
  await expect(page.getByRole('img', { name: '被仿写图', exact: true })).toHaveAttribute('src', new RegExp(`/assets/source-${number}/file(?:\\?|$)`));
}

async function selectScheme(page: Page, number: number) {
  await page.getByRole('combobox', { name: '图片方案', exact: true }).click();
  await page.getByRole('option', { name: `方案 ${number}`, exact: true }).click();
}

for (const width of [1536, 825, 597]) test(`Microsoft Edge ${width}px 初次打开默认方案 1，十四张原图按顺序对应方案`, async ({ page }) => {
  await page.setViewportSize({ width, height: 751 });
  const f = await fixture(page);
  await expectPrimary(page, 1);
  await expect(page.locator('.image-scheme-toolbar').getByTitle('方案 1', { exact: true })).toHaveText('方案 1');
  await page.getByRole('combobox', { name: '图片方案', exact: true }).click();
  await expect(page.getByRole('option')).toHaveText(Array.from({ length: 14 }, (_, i) => `方案 ${i + 1}`));
  await page.getByRole('option', { name: '方案 14', exact: true }).click();
  await expectPrimary(page, 14);
  await selectScheme(page, 2); await expectPrimary(page, 2);
  await page.reload(); await page.getByRole('tab', { name: '图片仿写', exact: true }).click();
  await expectPrimary(page, 1);
  expect(f.writes).toEqual([]);
});

test('Microsoft Edge 空项目异步初始化后直接打开方案 1，切换模块不会重复初始化', async ({ page }) => {
  const f = await fixture(page, { empty: true });
  await expect.poll(() => f.writes).toEqual(['/rewrites/scheme-rewrite/images/initialize']);
  await expect(page.getByRole('button', { name: /正在准备图片方案$/ })).toBeVisible();
  await expect(page.getByRole('button', { name: /新方案$/ })).toBeDisabled();
  f.releaseInitialization();
  await expectPrimary(page, 1);
  await expect(page.locator('.image-scheme-toolbar').getByTitle('方案 1', { exact: true })).toHaveText('方案 1');
  await page.getByRole('tab', { name: '文案编辑', exact: true }).click();
  await page.getByRole('tab', { name: '图片仿写', exact: true }).click();
  await expectPrimary(page, 1);
  expect(f.writes).toEqual(['/rewrites/scheme-rewrite/images/initialize']);
});

test('Microsoft Edge 轮询更新后保留用户选择的方案，不跳回第一张', async ({ page }) => {
  const f = await fixture(page);
  await selectScheme(page, 14); await expectPrimary(page, 14);
  const beforePoll = f.detailReads();
  f.schemes[13].revision++;
  f.schemes[13].analyses = [{ asset_id: 'source-14', width: 960, height: 1280, features: { subject: '轮询后的主体分析', action: '静置', scene: '纯色背景', composition: '居中', palette: '红白', lighting: '平光', texture: '插画', text: '无文字' } }];
  await expect.poll(f.detailReads).toBeGreaterThan(beforePoll);
  await expectPrimary(page, 14);
  await expect(page.locator('.image-scheme-toolbar').getByTitle('方案 14', { exact: true })).toHaveText('方案 14');
  // 轮询送达的新分析进入选图弹窗的搜索关键词，方案选择不跳回第一张
  await page.getByRole('button', { name: '更换', exact: true }).first().click();
  const picker = page.getByRole('dialog', { name: '选择被仿写图', exact: true });
  await picker.getByRole('searchbox').fill('轮询后的主体分析');
  await expect(picker.getByRole('checkbox', { name: '选择图片 14', exact: true })).toBeVisible();
  await expect(picker.getByRole('checkbox', { name: '选择图片 13', exact: true })).toHaveCount(0);
  expect(f.writes).toEqual([]);
});

test('Microsoft Edge 初始化失败保留明确重试入口，轮询不自动重复提交', async ({ page }) => {
  const f = await fixture(page, { empty: true, failInitialization: true });
  await expect(page.getByText('图片方案初始化失败', { exact: true })).toBeVisible();
  await expect(page.getByText('模拟方案初始化失败', { exact: true })).toBeVisible();
  const beforePoll = f.detailReads();
  await expect.poll(f.detailReads).toBeGreaterThan(beforePoll);
  expect(f.writes).toEqual(['/rewrites/scheme-rewrite/images/initialize']);
  f.releaseInitialization();
  await page.getByRole('button', { name: /^重\s*试$/ }).click();
  await expectPrimary(page, 1);
  await expect(page.getByText('图片方案初始化失败', { exact: true })).not.toBeVisible();
  expect(f.writes).toEqual(['/rewrites/scheme-rewrite/images/initialize', '/rewrites/scheme-rewrite/images/initialize']);
});

test('Microsoft Edge 手动新建方案立即选中，旧轮询响应和后续持久化均不丢失选择', async ({ page }) => {
  const f = await fixture(page);
  await page.getByRole('button', { name: /新方案$/ }).click();
  const dialog = page.getByRole('dialog', { name: '选择被仿写图', exact: true });
  await dialog.getByRole('checkbox', { name: '选择图片 2', exact: true }).check();
  await dialog.getByRole('button', { name: '确认选择', exact: true }).click();
  await expect(dialog).not.toBeVisible(); await expectPrimary(page, 2);
  await expect(page.locator('.image-scheme-toolbar').getByTitle('方案 15', { exact: true })).toHaveText('方案 15');
  const beforePoll = f.detailReads();
  await expect.poll(f.detailReads).toBeGreaterThan(beforePoll);
  await expect(page.locator('.image-scheme-toolbar').getByTitle('方案 15', { exact: true })).toHaveText('方案 15');
  f.persistManual();
  const beforePersist = f.detailReads();
  await expect.poll(f.detailReads).toBeGreaterThan(beforePersist);
  await expectPrimary(page, 2);
  await expect(page.locator('.image-scheme-toolbar').getByTitle('方案 15', { exact: true })).toHaveText('方案 15');
  await selectScheme(page, 1); await expectPrimary(page, 1);
  expect(f.writes).toEqual(['/rewrites/scheme-rewrite/images']);
});

test('Microsoft Edge 初始化错误在方案被其他页面补齐后自动清除', async ({ page }) => {
  const f = await fixture(page, { empty: true, failInitialization: true });
  await expect(page.getByText('图片方案初始化失败', { exact: true })).toBeVisible();
  f.rewrite.image_rewrites = f.schemes;
  await expectPrimary(page, 1);
  await expect(page.getByText('图片方案初始化失败', { exact: true })).not.toBeVisible();
  expect(f.writes).toEqual(['/rewrites/scheme-rewrite/images/initialize']);
});
