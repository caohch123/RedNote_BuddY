import { memberAuthResponse } from './member-auth';
import { expect, test, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import path from 'node:path';

async function libraryMenu(page: Page, id: string) {
  await page.locator(`.library-media-card[data-asset-id="${id}"]`).getByRole('button', { name: /^素材操作：/ }).click();
  const menu = page.locator('.library-media-menu').getByRole('menu');
  await expect(menu).toBeVisible();
  return menu;
}
async function fixture(page: Page) {
  const note = { id: 'note', platform: 'xiaohongshu', title: '来源笔记', body: '原正文', author: {}, asset_ids: ['main', 'other'], topics: [], tags: [] };
  const v = { id: 'v1', number: 1, title: '测试项目', body: '保存的正文', topics: [], script: '', asset_ids: ['main', 'other'], confirmed: true, source_snapshot: note };
  const result = { asset_id: 'generated', revision: 1, primary_asset_id: 'main', source_asset_ids: ['main'], prompt: '测试提示词', model: 'mock', width: 960, height: 1280 };
  const scheme = { id: 'scheme', version_id: 'v2', workflow_version: 2, edit_intent: { mode: 'flexible' }, primary_asset_id: 'main', references: [], feature_controls: Object.fromEntries(['subject', 'action', 'scene', 'composition', 'palette', 'lighting', 'texture', 'text'].map(key => [key, { mode: 'image', asset_id: 'main' }])), source_asset_ids: ['main'], analyses: [], output: { ratio: '3:4', clarity: 'standard', count: 1 }, style: { ratio: '3:4' }, prompt: result.prompt, revision: 1, status: 'ready', results: [result], batches: [] };
  const rewrite: any = { id: 'project', note_id: note.id, title: v.title, confirmed_version_id: 'v2', versions: [v, { ...v, id: 'v2', number: 2 }], image_rewrites: [scheme], options: {} };
  const assets: any[] = ['main', 'other', 'generated', 'second-result', 'upload', 'scene'].map((id, position) => ({ id, position, owner_id: position < 2 ? note.id : id === 'scene' ? 'video-project' : rewrite.id, kind: 'image', mime: 'image/png', status: 'ready', width: 896, height: 1200, extraction: { text: id === 'main' ? '养生饮食' : id === 'other' ? '睡眠运动' : '' }, ...(id === 'upload' ? { name: 'Sunset.png' } : {}) }));
  Object.assign(assets.find(a => a.id === 'second-result'), { width: 1200, height: 675 });
  Object.assign(assets.find(a => a.id === 'upload'), { width: 720, height: 720 });
  const library = () => assets.filter(a => a.owner_id !== note.id && !a.library_deleted_at).map(asset => ({ id: asset.id, asset, rewrite_id: rewrite.id, project_title: rewrite.title, origin: asset.id === 'upload' ? 'upload' : asset.id === 'scene' ? 'video_image' : 'image_rewrite', revision: 1, stale: false }));
  const writes: { path: string; body: any }[] = []; let failDelete = false;
  await page.route('**/api/v1/**', async route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const request = route.request(); const url = new URL(request.url()); const p = url.pathname.replace('/api/v1', ''); const method = request.method();
    const json = (value: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) });
    if (method !== 'GET') { expect(request.headers()['x-csrf-token']).toBe('library-csrf'); expect(url.searchParams.get('platform')).toBe('xiaohongshu'); writes.push({ path: p, body: request.postDataJSON() }); }
    if (p === '/session') return json({ csrf_token: 'library-csrf' });
    if (p === '/workspaces') return json({ items: [{ platform: 'xiaohongshu' }], total: 1 });
    if (p === '/rewrites') return json({ items: [rewrite], total: 1 });
    if (p === '/rewrites/project') return json({ rewrite, note, assets, videos: [] });
    if (p === '/media-library') return json({ items: library(), total: library().length });
    if (p === '/image-capabilities') return json({ prompt_rules_version: 6, vision_configured: true, text_configured: true, image_configured: true, supported: true, sizes: [{ ratio: '3:4', clarity: 'standard', width: 960, height: 1280 }] });
    if (p === '/rewrites/project/images/scheme/apply') {
      const b = request.postDataJSON(); const old = rewrite.versions.find((item: any) => item.id === b.version_id);
      const next = { ...old, id: 'v3', number: 3, confirmed: false, parent_version_id: old.id, asset_ids: old.asset_ids.map((id: string) => id === 'main' ? b.asset_id : id) };
      rewrite.versions.push(next); return json(next, 201);
    }
    if (p === '/rewrites/project/versions') { const next = { ...request.postDataJSON(), id: `v${rewrite.versions.length + 1}`, number: rewrite.versions.length + 1, confirmed: false }; rewrite.versions.push(next); return json(next, 201); }
    if (p === '/rewrites/project/confirm') { rewrite.confirmed_version_id = request.postDataJSON().version_id; rewrite.versions.at(-1).confirmed = true; return json(rewrite); }
    if (p.startsWith('/rewrites/project/media-library/')) {
      expect(method).toBe('DELETE'); expect(request.postDataJSON()).toEqual({ confirm: true });
      if (failDelete) { failDelete = false; return json({ error: '模拟删除失败，未提交' }, 500); }
      const asset = assets.find(a => a.id === p.split('/').at(-1)); asset.library_deleted_at = '2026-10-12T00:00:00Z'; return json({ asset_id: asset.id, history_preserved: true });
    }
    if (p.startsWith('/assets/')) {
      const asset = assets.find(a => a.id === p.split('/')[2]);
      return route.fulfill({ contentType: 'image/svg+xml', body: `<svg xmlns="http://www.w3.org/2000/svg" width="${asset?.width || 896}" height="${asset?.height || 1200}"><rect width="100%" height="100%" fill="#eef1f5"/></svg>` });
    }
    if (p === '/publications' && method === 'POST') return json({ ...rewrite.versions.at(-1), ...request.postDataJSON(), id: 'publication', status: 'ready' }, 201);
    return json({ items: [], total: 0 });
  });
  await page.addInitScript(() => { localStorage.setItem('workbench.workspace', 'xiaohongshu'); localStorage.setItem('workbench.xiaohongshu.page', 'rewrites'); localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true'); });
  await page.goto('/'); await expect(page.getByRole('textbox', { name: '仿写正文' })).toBeVisible();
  return { writes, rewrite, assets, failNextDelete: () => { failDelete = true; } };
}

test('未采用结果可选但不自动选用，采用后仅替换主图并保留取消勾选和文案草稿', async ({ page }) => {
  const f = await fixture(page);
  await page.getByRole('textbox', { name: '仿写正文' }).fill('未保存的独立文案');
  await page.getByRole('tab', { name: /发布素材/ }).click();
  await expect(page.locator('.media-selection-card')).toHaveCount(6);
  await expect(page.locator('.media-selection-card[data-asset-id="generated"]').getByRole('checkbox')).not.toBeChecked();
  await page.locator('.media-selection-card[data-asset-id="other"]').getByRole('checkbox').uncheck();
  await page.getByRole('tab', { name: '图片仿写', exact: true }).click();
  await expect(page.getByRole('button', { name: '采用此图', exact: true })).toBeVisible();
  expect(f.writes).toEqual([]);
  await page.getByRole('button', { name: '采用此图', exact: true }).click();
  await expect.poll(() => f.writes.filter(w => w.path.endsWith('/apply')).length).toBe(1);
  await page.getByRole('tab', { name: /发布素材/ }).click();
  await expect(page.locator('.media-selection-card[data-asset-id="generated"]').getByRole('checkbox')).toBeChecked();
  await expect(page.locator('.media-selection-card[data-asset-id="other"]').getByRole('checkbox')).not.toBeChecked();
  await expect(page.locator('.media-selection-card[data-asset-id="second-result"]').getByRole('checkbox')).not.toBeChecked();
  await page.getByRole('button', { name: '保存新版本', exact: true }).click();
  await expect.poll(() => f.writes.find(w => w.path.endsWith('/versions'))?.body.asset_ids).toEqual(['generated']);
  expect(f.writes.find(w => w.path.endsWith('/versions'))?.body.body).toBe('未保存的独立文案');
  await page.getByRole('button', { name: '确认此版本', exact: true }).click();
  await expect(page.getByRole('button', { name: '创建发布稿', exact: true })).toBeEnabled();
  await page.getByRole('button', { name: '创建发布稿', exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: '创建发布稿', exact: true }).click();
  await expect.poll(() => f.writes.find(w => w.path === '/publications')?.body.asset_ids).toEqual(['generated']);
});

test('素材库显式追加、取消勾选与删除失败重试，不改变历史和已有发布选择', async ({ page }) => {
  const f = await fixture(page); await page.getByRole('textbox', { name: '仿写正文' }).fill('保留文案草稿');
  await page.getByRole('tab', { name: '素材库', exact: true }).click();
  const card = (id: string) => page.locator(`.library-media-card[data-asset-id="${id}"]`);
  await (await libraryMenu(page, 'second-result')).getByRole('menuitem', { name: '加入发布素材', exact: true }).click();
  await expect((await libraryMenu(page, 'second-result')).getByRole('menuitem', { name: '已加入发布素材', exact: true })).toHaveAttribute('aria-disabled', 'true');
  await card('second-result').getByRole('button', { name: /^素材操作：/ }).click();
  await (await libraryMenu(page, 'scene')).getByRole('menuitem', { name: '加入发布素材', exact: true }).click();
  expect(f.writes).toEqual([]);
  await page.getByRole('tab', { name: /发布素材/ }).click();
  const added = page.locator('.media-selection-card[data-asset-id="second-result"]');
  await added.getByRole('checkbox').scrollIntoViewIfNeeded();
  const position = await added.boundingBox();
  await added.getByRole('checkbox').uncheck(); await expect(added).toBeVisible(); await expect(added.getByRole('checkbox')).not.toBeChecked();
  expect((await added.boundingBox())?.y).toBe(position?.y);
  await added.getByRole('checkbox').check();
  await page.getByRole('tab', { name: '素材库', exact: true }).click();
  await (await libraryMenu(page, 'generated')).getByRole('menuitem', { name: '删除素材' }).click();
  await page.getByRole('dialog', { name: '删除素材' }).getByRole('button', { name: '取消', exact: true }).click();
  expect(f.writes).toEqual([]);
  f.failNextDelete(); await (await libraryMenu(page, 'generated')).getByRole('menuitem', { name: '删除素材' }).click();
  const dialog = page.getByRole('dialog', { name: '删除素材' }); await dialog.getByRole('button', { name: '确认删除' }).click();
  await expect(dialog).toContainText('模拟删除失败'); await expect(card('generated')).toBeVisible();
  await dialog.getByRole('button', { name: '确认删除' }).click(); await expect(dialog).toBeHidden(); await expect(card('generated')).toHaveCount(0);
  expect(f.rewrite.image_rewrites[0].results).toHaveLength(1);
  await (await libraryMenu(page, 'second-result')).getByRole('menuitem', { name: '删除素材' }).click(); await page.getByRole('dialog', { name: '删除素材' }).getByRole('button', { name: '确认删除' }).click();
  await expect(card('second-result')).toHaveCount(0);
  await page.getByRole('tab', { name: /发布素材/ }).click();
  await expect(page.locator('.media-selection-card[data-asset-id="scene"]').getByRole('checkbox')).toBeChecked();
  await expect(page.locator('.media-selection-card[data-asset-id="second-result"]').getByRole('checkbox')).toBeChecked();
  await page.locator('.media-selection-card[data-asset-id="other"]').getByRole('checkbox').uncheck();
  await page.getByRole('tab', { name: '文案编辑', exact: true }).click();
  await expect(page.getByRole('textbox', { name: '仿写正文' })).toHaveValue('保留文案草稿');
  await page.getByRole('button', { name: '保存新版本', exact: true }).click();
  await expect.poll(() => f.writes.find(w => w.path.endsWith('/versions'))?.body.asset_ids).toEqual(['main', 'second-result', 'scene']);
});

test('选图搜索按名称、文字、关键词筛选，数字不会命中分辨率，筛选不丢选择', async ({ page }) => {
  const f = await fixture(page); await page.getByRole('tab', { name: '图片仿写', exact: true }).click();
  await page.getByRole('button', { name: '1. 选图与方案' }).click();
  await page.getByRole('button', { name: '选择', exact: true }).first().click();
  const dialog = page.getByRole('dialog', { name: '选择辅助参考图' }); const search = dialog.getByRole('searchbox');
  await search.fill('2'); await search.press('Enter');
  await expect(dialog.locator('.picture-choice')).toHaveCount(1); await expect(dialog.getByRole('checkbox', { name: '选择图片 2', exact: true })).toBeVisible();
  await dialog.getByRole('checkbox', { name: '选择图片 2', exact: true }).check();
  await search.fill('不存在的关键字'); await expect(dialog.locator('.picture-choice')).toHaveCount(0); await expect(dialog).toContainText('没有符合条件的图片'); await expect(dialog).toContainText('已选 1 / 1');
  await search.fill('睡眠'); await expect(dialog.locator('.picture-choice')).toHaveCount(1);
  await search.fill('SUNSET'); await expect(dialog.getByRole('checkbox', { name: '选择Sunset.png' })).toBeVisible();
  await dialog.getByRole('button', { name: '取消', exact: true }).click();
  expect(f.writes).toEqual([]); expect(f.rewrite.image_rewrites[0].references).toEqual([]);
});

test('移除版本对比入口，素材库保留原比例且宽窄窗口均可打开操作菜单', async ({ page }) => {
  await fixture(page); await expect(page.getByRole('button', { name: '版本对比', exact: true })).toHaveCount(0);
  await expect(page.getByRole('dialog', { name: '版本对比' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'AI 生成新版本', exact: true })).toBeVisible();
  await expect(page.getByRole('combobox', { name: '文案模型' })).toBeVisible();
  for (const name of ['图片仿写', '视频仿写', '素材库', '发布素材 (2)']) { await page.getByRole('tab', { name, exact: true }).click(); await expect(page.getByRole('button', { name: '版本对比', exact: true })).toHaveCount(0); }
  await page.getByRole('tab', { name: '素材库', exact: true }).click();
  const proof = path.resolve('../data/verification/media-library-actions-20261012'); mkdirSync(proof, { recursive: true });
  for (const width of [1536, 825, 593]) {
    await page.setViewportSize({ width, height: 751 });
    const card = page.locator('.library-media-card').first(); await card.scrollIntoViewIfNeeded();
    await expect(page.locator('.library-media-title, .library-media-tags, .library-media-meta, .library-media-actions')).toHaveCount(0);
    const id = (await card.getAttribute('data-asset-id'))!;
    const menu = await libraryMenu(page, id);
    await expect(menu.getByRole('menuitem', { name: '加入发布素材', exact: true })).toBeVisible();
    await expect(menu.getByRole('link', { name: '下载素材', exact: true })).toHaveAttribute('download', '');
    await expect(menu.getByRole('menuitem', { name: '删除素材', exact: true })).toBeVisible();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await card.getByRole('button', { name: /^素材操作：/ }).click();
    for (const assetID of ['generated', 'second-result', 'upload']) {
      const image = page.locator(`.library-media-card[data-asset-id="${assetID}"] img`);
      await image.scrollIntoViewIfNeeded();
      await expect.poll(() => image.evaluate(node => (node as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
      const sizes = await image.evaluate(node => {
        const image = node as HTMLImageElement, box = image.getBoundingClientRect();
        return { width: box.width, height: box.height, ratio: image.naturalWidth / image.naturalHeight };
      });
      expect(Math.abs(sizes.height - sizes.width / sizes.ratio)).toBeLessThanOrEqual(1);
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
    await page.screenshot({ path: path.join(proof, `library-edge-${width}.png`) });
  }
});
