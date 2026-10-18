import { memberAuthResponse } from './member-auth';
import { expect, test } from '@playwright/test';

for (const workflow of ['image', 'video'] as const) for (const width of [1536, 644, 360]) {
  test(`${workflow} 素材卡片在 ${width}px 通过菜单选择、移除和上传`, async ({ page }) => {
    const note = { id: 'note', platform: 'xiaohongshu', title: '素材菜单测试', body: '完整正文', asset_ids: ['main', 'aux'], topics: [], author: {}, media_status: 'succeeded' };
    const version = { id: 'version', number: 1, title: note.title, body: note.body, script: '', topics: [], asset_ids: note.asset_ids };
    const image = { id: 'main', owner_id: note.id, name: '主图.png', kind: 'image', mime: 'image/png', position: 0, width: 320, height: 240, status: 'ready' };
    const auxiliary = { ...image, id: 'aux', name: '辅助图片.png', position: 1 };
    const assets = workflow === 'image' ? [image, auxiliary] : [{ ...image, id: 'video-main', name: '主视频.mp4', kind: 'video', mime: 'video/mp4', duration: 4 }, { ...auxiliary, id: 'video-aux', name: '辅助视频.mp4', kind: 'video', mime: 'video/mp4', duration: 3 }, image];
    const unit: any = { id: 'scheme', workflow_version: 2, edit_intent: { mode: 'flexible' }, version_id: version.id, primary_asset_id: image.id, references: [], source_asset_ids: [image.id], feature_controls: {}, proposals: [], analyses: [], batches: [], results: [], prompt: '', revision: 1, status: 'draft', output: { ratio: '3:4', clarity: 'standard', count: 1 } };
    const project: any = { id: 'video-project', rewrite_id: 'rewrite', version_id: version.id, workflow: 'video_rewrite_v2', revision: 1, created_at: '2026-10-17T00:00:00Z', video_rewrite: { primary_asset_id: 'video-main', reference_video_id: '', reference_image_id: '', purpose_sources: {}, requirements: '', prompt: '', output: { duration: 10, resolution: '720p', ratio: '9:16', generate_audio: true }, analyses: [], submissions: [], results: [], script: [] } };
    const rewrite = { id: 'rewrite', note_id: note.id, title: note.title, versions: [version], image_rewrites: workflow === 'image' ? [unit] : [], options: {} };
    let uploads = 0;
    await page.route('**/api/v1/**', route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
      const request = route.request(); const path = new URL(request.url()).pathname.replace('/api/v1', '');
      const json = (value: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) });
      if (path === '/workspaces') return json({ items: [{ platform: 'xiaohongshu' }], total: 1 });
      if (path === '/session') return json({ csrf_token: 'reference-menu' });
      if (path === '/rewrites') return json({ items: [rewrite], total: 1 });
      if (path === '/rewrites/rewrite') return json({ rewrite, note, assets, videos: workflow === 'video' ? [project] : [] });
      if (path === '/rewrites/rewrite/videos/initialize') return json([project]);
      if (path === '/rewrites/rewrite/images/initialize') return json([]);
      if (path === '/video-capabilities') return json({ text_configured: true, vision_configured: true, video_configured: true, supported: true, rules_version: 1 });
      if (path === '/image-capabilities') return json({ sizes: [], vision_configured: true, text_configured: true, image_configured: true, prompt_rules_version: 6 });
      if (path === '/assets' && request.method() === 'POST') {
        expect(request.headers()['content-type']).toContain('multipart/form-data'); uploads++;
        const uploaded = { ...assets[1], id: 'uploaded', owner_id: rewrite.id, position: 4 }; assets.push(uploaded); return json(uploaded, 201);
      }
      if (path === '/rewrites/rewrite/images/scheme') {
        if (request.method() === 'PATCH') { Object.assign(unit, request.postDataJSON()); unit.revision++; }
        return json(unit);
      }
      if (path === '/video-projects/video-project') {
        if (request.method() === 'PATCH') { Object.assign(project.video_rewrite, request.postDataJSON().video_rewrite); project.revision++; }
        return json(project);
      }
      if (path.startsWith('/assets/')) return route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="320" height="240"/>' });
      return json({ items: [], total: 0 });
    });
    await page.setViewportSize({ width, height: 751 }); await page.goto('/');
    await page.getByRole('menuitem', { name: '仿写库', exact: true }).click();
    await page.getByRole('tab', { name: workflow === 'image' ? '图片仿写' : '视频仿写', exact: true }).click();
    const title = workflow === 'image' ? '辅助参考图 1' : '参考视频';
    const card = page.locator(`[data-reference-title="${title}"]`);
    await expect(page.locator('.reference-slot-card')).toHaveCount(3);
    await expect(card).toHaveCSS('border-radius', '24px');
    await expect(card.getByRole('button', { name: `${title}菜单`, exact: true })).toBeVisible();
    const box = (await card.boundingBox())!; expect(box.height).toBeGreaterThanOrEqual(176);
    const grid = page.locator(workflow === 'image' ? '.image-input-grid' : '.video-material-grid');
    const spacing = await grid.evaluate(node => ({ gap: parseFloat(getComputedStyle(node).columnGap), top: parseFloat(getComputedStyle(node).paddingTop), bottom: parseFloat(getComputedStyle(node).paddingBottom) }));
    expect(spacing.gap).toBeGreaterThanOrEqual(18); expect(spacing.top).toBeGreaterThanOrEqual(10); expect(spacing.bottom).toBeGreaterThanOrEqual(18);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
    await card.getByRole('button', { name: `${title}菜单`, exact: true }).click();
    await page.locator('.reference-slot-menu').getByRole('menuitem', { name: '选择', exact: true }).click();
    const picker = page.getByRole('dialog', { name: workflow === 'image' ? '选择辅助参考图' : '参考视频', exact: true });
    await picker.getByRole('checkbox', { name: workflow === 'image' ? /辅助图片/ : /辅助视频/ }).check();
    await picker.getByRole('button', { name: '确认选择', exact: true }).click();
    const selected = () => workflow === 'image' ? unit.references[0]?.asset_id : project.video_rewrite.reference_video_id;
    await expect.poll(selected).toBe(workflow === 'image' ? 'aux' : 'video-aux');
    await expect(card).toHaveClass(/has-media/);
    const media = card.locator(workflow === 'image' ? 'img' : 'video').first();
    await expect(media).toHaveCSS('object-fit', 'cover');
    const previewBox = (await media.boundingBox())!;
    expect(previewBox.width / box.width).toBeGreaterThan(.95); expect(previewBox.height / box.height).toBeGreaterThan(.75);
    await card.getByRole('button', { name: `${title}菜单`, exact: true }).click();
    await page.locator('.reference-slot-menu').getByRole('menuitem', { name: '移除', exact: true }).click();
    await expect.poll(selected).toBe(workflow === 'image' ? undefined : '');
    await card.getByRole('button', { name: `${title}菜单`, exact: true }).click();
    const chooser = page.waitForEvent('filechooser');
    await page.locator('.reference-slot-menu').getByRole('menuitem', { name: workflow === 'image' ? '上传图片' : '上传视频', exact: true }).click();
    await (await chooser).setFiles({ name: workflow === 'image' ? 'reference.png' : 'reference.mp4', mimeType: workflow === 'image' ? 'image/png' : 'video/mp4', buffer: Buffer.from('fixture-upload') });
    await expect.poll(selected).toBe('uploaded');
    expect(uploads).toBe(1);
    await expect(card).toHaveClass(/has-media/);
    await grid.screenshot({ path: `../data/verification/reference-slot-spacious-${workflow}-${width}.png` });
    if (width === 1536) await page.locator('.rewrite-main').screenshot({ path: `../data/verification/rewrite-menu-centered-${workflow}-20261017.png` });
  });
}
