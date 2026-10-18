import { memberAuthResponse } from './member-auth';
import { expect, test, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import path from 'node:path';

const proof = path.resolve('../data/verification/edge-media-selection-20261015');
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jxdoAAAAASUVORK5CYII=', 'base64');

async function fixture(page: Page, platform = 'xiaohongshu', duplicateAssets = false) {
  await page.addInitScript(value => localStorage.setItem('workbench.workspace', value), platform);
  const original = { id: 'v1', number: 1, title: '默认素材顺序示例', body: '顺序随新版本保存。', script: '', topics: [], asset_ids: ['a2', 'a1', 'a3'], confirmed: true };
  const note = { id: 'source', platform, title: '原始笔记', body: '原始正文', author: { name: '测试作者' }, asset_ids: ['a1', 'a2', 'a3', 'a4'], topics: [] };
  const rewrite: any = { id: 'rewrite', note_id: note.id, title: original.title, versions: [original], confirmed_version_id: original.id, options: {} };
  const assets = ['a1', 'a2', 'a3', 'a4'].map((id, position) => ({ id, position, owner_id: note.id, kind: 'image', mime: 'image/png', status: 'ready', width: 240, height: [360, 135, 240, 420][position] }));
  if (duplicateAssets) assets.push({ ...assets[0] }, { ...assets[1] });
  const writes: { path: string; body: any }[] = [];
  let publication: any;
  await page.route('**/api/v1/**', async route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const request = route.request(); const url = new URL(request.url()); const routePath = url.pathname.replace('/api/v1', '');
    const json = (value: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) });
    if (request.method() !== 'GET') {
      expect(request.headers()['x-csrf-token']).toBe('media-csrf');
      expect(url.searchParams.get('platform')).toBe(platform);
      if (routePath !== '/assets') writes.push({ path: routePath, body: request.postDataJSON() });
    }
    if (routePath === '/session') return json({ csrf_token: 'media-csrf' });
    if (routePath === '/workspaces') return json({ items: [{ id: 'xiaohongshu', platform: 'xiaohongshu' }], total: 1 });
    if (routePath === '/rewrites') return json({ items: [rewrite], total: 1 });
    if (routePath === '/rewrites/rewrite') return json({ rewrite, note, assets, videos: [] });
    if (routePath === '/rewrites/rewrite/versions') {
      const version = { ...request.postDataJSON(), id: `v${rewrite.versions.length + 1}`, number: rewrite.versions.length + 1, confirmed: false };
      rewrite.versions.push(version); return json(version, 201);
    }
    if (routePath === '/rewrites/rewrite/confirm') { rewrite.confirmed_version_id = request.postDataJSON().version_id; return json(rewrite); }
    if (routePath === '/assets' && request.method() === 'POST') {
      const asset = { ...assets[0], id: 'a5', position: 4, owner_id: rewrite.id }; assets.push(asset); return json(asset, 201);
    }
    if (routePath === '/assets') return json({ items: assets, total: assets.length });
    if (routePath.startsWith('/assets/')) {
      const asset = assets.find(asset => asset.id === routePath.split('/')[2]);
      return route.fulfill({ contentType: 'image/svg+xml', body: `<svg xmlns="http://www.w3.org/2000/svg" width="${asset?.width || 240}" height="${asset?.height || 360}"><rect width="100%" height="100%" fill="#eef1f5"/></svg>` });
    }
    if (routePath === '/publications' && request.method() === 'POST') {
      const body = request.postDataJSON();
      const version = rewrite.versions.find((v: any) => v.id === body.version_id);
      publication = { ...version, ...body, id: 'publication', status: version.asset_ids.length ? 'ready' : 'preparing', asset_ids: [...version.asset_ids] };
      return json(publication, 201);
    }
    if (routePath === '/publications') return json({ items: publication ? [publication] : [], total: publication ? 1 : 0 });
    if (routePath === '/publications/publication') return json(publication);
    if (routePath.endsWith('/account')) return json({ status: 'logged_out', dependency_ready: true });
    return json({ items: [], total: 0 });
  });
  await page.goto('/'); await page.getByRole('menuitem', { name: /仿写库/ }).click();
  await page.getByRole('tab', { name: /发布素材/ }).click();
  return { original, rewrite, writes, note, getPublication: () => publication };
}

async function order(page: Page, ids: string[]) {
  await expect.poll(() => page.locator('.publication-media-grid .media-selection-card').evaluateAll(cards => cards.map(card => card.getAttribute('data-asset-id')))).toEqual(ids);
  await expect(page.locator('.publication-media-grid .media-publish-position, .publication-media-grid .ant-card-head')).toHaveCount(0);

}

test('取消勾选后卡片原位不跳动，连续点击同一勾选框只切换该素材', async ({ page }) => {
  await fixture(page);
  const card = page.locator('[data-asset-id="a2"]');
  const checkbox = card.getByRole('checkbox');
  const before = (await checkbox.boundingBox())!;
  await checkbox.click();
  await expect(checkbox).not.toBeChecked();
  const after = (await checkbox.boundingBox())!;
  expect(Math.abs(after.x - before.x)).toBeLessThanOrEqual(1);
  expect(Math.abs(after.y - before.y)).toBeLessThanOrEqual(1);
  await page.mouse.click(before.x + before.width / 2, before.y + before.height / 2);
  await expect(checkbox).toBeChecked();
  await expect(page.locator('[data-asset-id="a1"]').getByRole('checkbox')).toBeChecked();
  await expect(page.locator('[data-asset-id="a3"]').getByRole('checkbox')).toBeChecked();
  await order(page, ['a2', 'a1', 'a3', 'a4']);
  await expect(page.getByText('素材有未保存编辑', { exact: true })).toHaveCount(0);
});

test('直接拖拽已选卡片，新版本和发布快照沿用排序', async ({ page, browser }) => {
  const f = await fixture(page, 'xiaohongshu');
  await order(page, ['a2', 'a1', 'a3', 'a4']);
  await expect(page.locator('.selected-media-list')).toHaveCount(0);
  await expect(page.getByText('发布顺序', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /素材.*(上移|下移)/ })).toHaveCount(0);
  await page.locator('[data-asset-id="a2"]').getByRole('checkbox').uncheck();
  await page.locator('[data-asset-id="a4"]').getByRole('checkbox').check();
  await order(page, ['a2', 'a1', 'a3', 'a4']);
  await page.locator('[data-asset-id="a4"] .publication-media-drag-handle').dragTo(page.locator('[data-asset-id="a1"]'));
  await order(page, ['a2', 'a4', 'a1', 'a3']);
  await expect(page.getByText('素材有未保存编辑', { exact: true })).toBeVisible();
  const publishButton = page.locator('.publication-material-actions').getByRole('button', { name: '创建发布稿' });
  await expect(publishButton).toBeDisabled();
  await page.getByRole('button', { name: '保存新版本' }).click();
  await expect(page.getByText(/当前版本 2 ·/)).toBeVisible();
  await order(page, ['a4', 'a1', 'a3', 'a2']);
  expect(f.writes.find(write => write.path.endsWith('/versions'))!.body.asset_ids).toEqual(['a4', 'a1', 'a3']);
  expect(f.original.asset_ids).toEqual(['a2', 'a1', 'a3']);
  expect(f.note.asset_ids).toEqual(['a1', 'a2', 'a3', 'a4']);
  await page.getByRole('button', { name: '确认此版本' }).click();
  await expect(publishButton).toBeEnabled();
  await publishButton.click();
  await page.getByRole('dialog').getByRole('button', { name: '创建发布稿', exact: true }).click();
  await expect.poll(() => f.getPublication()?.asset_ids).toEqual(['a4', 'a1', 'a3']);
  expect(f.getPublication().target_platform).toBe('xiaohongshu');
  expect(f.getPublication().version_id).toBe('v2');
  test.info().annotations.push({ type: 'browser', description: `Microsoft Edge ${browser.version()}` });
});

test('勾选保留卡片位置，上传素材按默认方式追加到末尾', async ({ page }) => {
  const f = await fixture(page);
  await page.getByRole('checkbox', { name: '选用素材 1', exact: true }).uncheck();
  await order(page, ['a2', 'a1', 'a3', 'a4']);
  await page.getByRole('checkbox', { name: '选用素材 1', exact: true }).check();
  await order(page, ['a2', 'a1', 'a3', 'a4']);
  await page.getByRole('checkbox', { name: '选用素材 4', exact: true }).check();
  await page.locator('input[type="file"]').setInputFiles({ name: 'upload.png', mimeType: 'image/png', buffer: png });
  await order(page, ['a2', 'a1', 'a3', 'a4', 'a5']);
  await page.getByRole('button', { name: '保存新版本' }).click();
  await expect.poll(() => f.writes.find(write => write.path.endsWith('/versions'))).toBeTruthy();
  expect(f.writes.find(write => write.path.endsWith('/versions'))!.body.asset_ids).toEqual(['a2', 'a1', 'a3', 'a4', 'a5']);
});

for (const keepOne of [true, false]) {
  test(`重复候选素材取消勾选、保存、刷新与发布快照一致（${keepOne ? '保留一项' : '全部取消'}）`, async ({ page }) => {
    const errors: string[] = [];
    page.on('console', event => { if (event.type() === 'error' && /same key|duplicate/i.test(event.text())) errors.push(event.text()); });
    const f = await fixture(page, 'xiaohongshu', true);
    await expect(page.locator('.media-selection-card')).toHaveCount(4);
    const cancel = keepOne ? ['a2', 'a1'] : ['a2', 'a1', 'a3'];
    for (const id of cancel) await page.locator(`[data-asset-id="${id}"]`).getByRole('checkbox').uncheck();
    await expect(page.locator('.selected-media')).toHaveCount(keepOne ? 1 : 0);
    await expect(page.locator('.media-selection-card')).toHaveCount(4);
    await expect(page.locator('.publication-material-actions').getByRole('button', { name: '创建发布稿' })).toBeDisabled();
    await page.getByRole('button', { name: '保存新版本' }).click();
    await expect(page.getByText(/当前版本 2 ·/)).toBeVisible();
    const expected = keepOne ? ['a3'] : [];
    expect(f.writes.find(write => write.path.endsWith('/versions'))!.body.asset_ids).toEqual(expected);
    expect(f.rewrite.versions[1].asset_ids).toEqual(expected);
    expect(f.original.asset_ids).toEqual(['a2', 'a1', 'a3']);
    await page.reload();
    await page.getByRole('tab', { name: /发布素材/ }).click();
    await expect(page.locator('.selected-media')).toHaveCount(expected.length);
    for (const id of cancel) await expect(page.locator(`[data-asset-id="${id}"]`).getByRole('checkbox')).not.toBeChecked();
    await page.getByRole('button', { name: '确认此版本' }).click();
    await expect(page.locator('.publication-material-actions').getByRole('button', { name: '创建发布稿' })).toBeEnabled();
    await page.locator('.publication-material-actions').getByRole('button', { name: '创建发布稿' }).click();
    await page.getByRole('dialog').getByRole('button', { name: '创建发布稿', exact: true }).click();
    await expect.poll(() => f.getPublication()?.asset_ids).toEqual(expected);
    expect(f.writes.find(write => write.path === '/publications')!.body.asset_ids).toEqual(expected);
    await expect(page.locator('.ant-drawer .publication-media > .ant-card')).toHaveCount(expected.length);
    expect(errors).toEqual([]);
  });
}

test('Edge 窄窗口瀑布流保留原比例、右上拖拽和右下圆形勾选且无序号', async ({ page }) => {
  await page.setViewportSize({ width: 825, height: 751 });
  await fixture(page);
  await order(page, ['a2', 'a1', 'a3', 'a4']);
  const card = page.locator('[data-asset-id="a1"]');
  const cardBox = (await card.boundingBox())!;
  const checkboxBox = (await card.locator('.ant-checkbox').boundingBox())!;
  expect(cardBox.x + cardBox.width - checkboxBox.x - checkboxBox.width).toBeLessThan(18);
  const handle = card.getByRole('button', { name: '拖拽素材 1', exact: true });
  const handleBox = (await handle.boundingBox())!;
  expect(handleBox.y - cardBox.y).toBeLessThan(14);
  expect(cardBox.x + cardBox.width - handleBox.x - handleBox.width).toBeLessThan(14);
  await expect(handle).toBeEnabled();
  for (const id of ['a1', 'a2', 'a3']) {
    const image = page.locator(`[data-asset-id="${id}"] img`);
    await image.scrollIntoViewIfNeeded();
    await expect.poll(() => image.evaluate(node => (node as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
    const sizes = await image.evaluate(node => {
      const image = node as HTMLImageElement, box = image.getBoundingClientRect();
      return { width: box.width, height: box.height, ratio: image.naturalWidth / image.naturalHeight };
    });
    expect(Math.abs(sizes.height - sizes.width / sizes.ratio)).toBeLessThanOrEqual(1);
  }
  const cardStyle = (node: Element) => ({ border: getComputedStyle(node).borderColor, background: getComputedStyle(node).backgroundColor });
  expect(await card.evaluate(cardStyle)).toEqual(await page.locator('[data-asset-id="a4"]').evaluate(cardStyle));
  await expect(card).toHaveAttribute('draggable', 'true');
  await card.getByRole('checkbox').uncheck();
  await expect(handle).toBeDisabled();
  await expect(card).toHaveAttribute('draggable', 'false');
  await order(page, ['a2', 'a1', 'a3', 'a4']);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(825);
  mkdirSync(proof, { recursive: true });
  await page.locator('.publication-media-grid').screenshot({ path: path.join(proof, 'edge-fixture-default-selection.png') });
});

test('拖拽不能改变勾选状态，键盘排序不干扰复选框操作', async ({ page }) => {
  await fixture(page);
  const first = page.locator('[data-asset-id="a2"]');
  await first.dragTo(page.locator('[data-asset-id="a4"]'));
  await order(page, ['a2', 'a1', 'a3', 'a4']);
  await expect(page.locator('[data-asset-id="a4"]').getByRole('checkbox')).not.toBeChecked();
  await first.getByRole('button', { name: '拖拽素材 2', exact: true }).focus();
  await page.keyboard.press('Alt+ArrowRight');
  await order(page, ['a1', 'a2', 'a3', 'a4']);
  await first.getByRole('checkbox').focus();
  await page.keyboard.press('Space');
  await expect(first.getByRole('checkbox')).not.toBeChecked();
  await order(page, ['a1', 'a2', 'a3', 'a4']);
  await expect(first).toHaveAttribute('draggable', 'false');
});
