import { memberAuthResponse } from './member-auth';
import { expect, test } from '@playwright/test';

test('发布素材汇总当前文案与图片视频，确认版本后可准备视频发布', async ({ page }) => {
  const note = { id: 'source', platform: 'xiaohongshu', title: '发布汇总', body: '保存的正文', asset_ids: ['image'], topics: ['分享'], author: {} };
  const original = { id: 'v1', number: 1, title: note.title, body: note.body, script: '', topics: note.topics, asset_ids: ['image'], confirmed: true };
  const rewrite = { id: 'rewrite', title: note.title, note_id: note.id, versions: [original], confirmed_version_id: original.id, options: {} };
  const image = { id: 'image', owner_id: note.id, kind: 'image', mime: 'image/png', status: 'ready', position: 0, width: 320, height: 240 };
  const video = { ...image, id: 'video', owner_id: 'video-project', kind: 'video', mime: 'video/mp4', position: 1, duration: 4 };
  const cover = { ...image, id: 'cover', owner_id: 'publication', kind: 'cover', position: 1 };
  const account = { id: 'owner', nickname: '本人测试账号' };
  let publication: any; let createBody: any; let publishCalls = 0;
  await page.route('**/api/v1/**', route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const request = route.request(); const path = new URL(request.url()).pathname.replace('/api/v1', '');
    const json = (value: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) });
    if (path === '/session') return json({ csrf_token: 'publish-materials' });
    if (path === '/workspaces') return json({ items: [{ platform: 'xiaohongshu' }], total: 1 });
    if (path === '/rewrites') return json({ items: [rewrite], total: 1 });
    if (path === '/rewrites/rewrite') return json({ rewrite, note, assets: [image], videos: [] });
    if (path === '/rewrites/rewrite/versions') {
      const next = { ...request.postDataJSON(), id: 'v2', number: 2, confirmed: false }; rewrite.versions.push(next); return json(next, 201);
    }
    if (path === '/rewrites/rewrite/confirm') { rewrite.confirmed_version_id = request.postDataJSON().version_id; rewrite.versions.at(-1)!.confirmed = true; return json(rewrite); }
    if (path === '/media-library') return json({ items: [{ id: video.id, asset: video, rewrite_id: rewrite.id, origin: 'video_rewrite', project_title: rewrite.title }], total: 1 });
    if (path === '/assets') return json({ items: [image, video, cover], total: 3 });
    if (path.startsWith('/assets/')) return route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="320" height="240"/>' });
    if (path === '/xhs/account') return json({ status: 'logged_in', dependency_ready: true, account });
    if (path === '/publications' && request.method() === 'POST') {
      createBody = request.postDataJSON(); const version = rewrite.versions.find(item => item.id === createBody.version_id)!;
      publication = { ...version, ...createBody, id: 'publication', status: 'ready' }; return json(publication, 201);
    }
    if (path === '/publications') return json({ items: publication ? [publication] : [], total: publication ? 1 : 0 });
    if (path === '/publications/publication') return json(publication);
    if (path === '/publications/publication/publish-preview') return json({ ...publication, publication_id: publication.id, media_type: 'video', assets: [video, cover], account, visibility: 'public', confirmation_token: 'video-confirm', expires_at: new Date(Date.now() + 60000).toISOString() });
    if (path === '/publications/publication/publish') { publishCalls++; return json({ job_id: 'publish' }, 202); }
    return json({ items: [], total: 0 });
  });
  await page.goto('/'); await page.getByRole('menuitem', { name: '仿写库', exact: true }).click();
  await expect(page.locator('.editor-footer').getByRole('button')).toHaveCount(2);
  await expect(page.getByRole('button', { name: '创建发布稿', exact: true })).toHaveCount(0);
  await page.getByRole('textbox', { name: '仿写正文', exact: true }).fill('本次准备发布的正文');
  await page.getByRole('tab', { name: /发布素材/ }).click();
  await expect(page.locator('.publication-copy-preview')).toContainText('本次准备发布的正文');
  await expect(page.locator('.publication-copy-preview')).toContainText('#分享');
  await expect(page.locator('.media-selection-card')).toHaveCount(2);
  await expect(page.locator('[data-asset-id="video"] video')).toHaveAttribute('controls', '');
  await page.locator('[data-asset-id="image"]').getByRole('checkbox').uncheck();
  await page.locator('[data-asset-id="video"]').getByRole('checkbox').check();
  const create = page.locator('.publication-material-actions').getByRole('button', { name: '创建发布稿', exact: true });
  await expect(create).toBeDisabled();
  await page.getByRole('button', { name: '保存新版本', exact: true }).click();
  await page.getByRole('button', { name: '确认此版本', exact: true }).click();
  await expect(create).toBeEnabled(); await create.click();
  await page.getByRole('dialog', { name: '创建小红书发布稿', exact: true }).getByRole('button', { name: '创建发布稿', exact: true }).click();
  await expect.poll(() => createBody?.asset_ids).toEqual(['video']);
  expect(publication.body).toBe('本次准备发布的正文');
  const detail = page.getByRole('dialog', { name: '发布稿详情', exact: true });
  await detail.getByRole('button', { name: '发布到小红书', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: '确认发布到小红书', exact: true });
  await expect(confirmation.locator('video')).toHaveAttribute('src', /video/);
  await expect(confirmation.getByRole('img', { name: '待发布素材 2', exact: true })).toHaveAttribute('src', /cover/);
  await confirmation.getByRole('checkbox').check();
  await expect(confirmation.getByRole('button', { name: '确认发布到小红书', exact: true })).toBeEnabled();
  expect(publishCalls).toBe(0);
});
