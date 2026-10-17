import { expect, test } from '@playwright/test';

for (const initialState of ['draft', 'queued', 'running']) {
  test(`视频步骤始终可查看，任务状态 ${initialState} 控制编辑`, async ({ page }) => {
    const note = { id: 'source', platform: 'xiaohongshu', title: '步骤切换', body: '原始正文', asset_ids: [], topics: [], media_status: 'succeeded' };
    const version = { id: 'version', number: 1, title: note.title, body: note.body, topics: [], asset_ids: [], script: '' };
    const rewrite = { id: 'rewrite', note_id: note.id, title: note.title, versions: [version], options: {} };
    let jobState = initialState;
    const project = { id: 'video', rewrite_id: rewrite.id, version_id: version.id, workflow: 'video_rewrite_v2', revision: 1, created_at: '2026-10-17T00:00:00Z',
      video_rewrite: { primary_asset_id: '', reference_video_id: '', reference_image_id: '', purpose_sources: {}, requirements: '保留的要求', prompt: initialState === 'draft' ? '' : '已生成的提示词',
        output: { duration: 10, resolution: '720p', ratio: '9:16', generate_audio: true }, script: [], script_input_digest: '', analyses: [], submissions: [], results: [] } };
    const writes: string[] = []; const errors: string[] = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.route('**/api/v1/**', route => {
      const request = route.request(); const path = new URL(request.url()).pathname.replace('/api/v1', '');
      const json = (value: unknown) => route.fulfill({ contentType: 'application/json', body: JSON.stringify(value) });
      if (path === '/session') return json({ csrf_token: 'step-fixture' });
      if (path === '/workspaces') return json({ items: [{ platform: 'xiaohongshu' }], total: 1 });
      if (path === '/rewrites') return json({ items: [rewrite], total: 1 });
      if (path === '/rewrites/rewrite') return json({ rewrite, note, assets: [], videos: [project] });
      if (path === '/rewrites/rewrite/videos/initialize') return json([project]);
      if (path === '/jobs') return json({ items: jobState === 'draft' ? [] : [{ id: 'job', entity_id: project.id, kind: 'video_rewrite', state: jobState, created_at: '2026-10-17T00:00:00Z' }], total: jobState === 'draft' ? 0 : 1 });
      if (path === '/video-capabilities') return json({ text_configured: true, vision_configured: true, video_configured: true, supported: true, rules_version: 1 });
      if (path === '/video-projects/video') {
        if (request.method() === 'PATCH') { writes.push(path); project.video_rewrite = { ...project.video_rewrite, ...request.postDataJSON().video_rewrite }; project.revision++; }
        return json(project);
      }
      return json({ items: [], total: 0 });
    });
    await page.goto('/'); await page.getByRole('menuitem', { name: '仿写库', exact: true }).click();
    await page.getByRole('tab', { name: '视频仿写', exact: true }).click();
    const steps = page.locator('.video-rewrite-steps');
    await expect(steps.getByRole('button')).toHaveCount(3);
    for (const button of await steps.getByRole('button').all()) await expect(button).toBeEnabled();
    await steps.getByRole('button', { name: '2. 审阅提示词', exact: true }).click();
    const prompt = page.getByRole('textbox', { name: '最终提交提示词', exact: true });
    await expect(prompt).toHaveValue(project.video_rewrite.prompt);
    await expect(prompt).toBeEnabled();
    await expect(prompt).toHaveJSProperty('readOnly', initialState !== 'draft');
    await steps.getByRole('button', { name: '3. 审阅结果', exact: true }).click();
    await expect(steps.getByRole('button', { name: '3. 审阅结果', exact: true })).toHaveAttribute('aria-current', 'step');
    if (initialState === 'draft') await expect(page.getByText('暂无生成结果，可返回审阅提示词', { exact: true })).toBeVisible();
    else await expect(page.getByText('任务处理中，关闭页面后可继续查询', { exact: true })).toBeVisible();
    await page.getByRole('button', { name: '返回修改提示词', exact: true }).click();
    await expect(prompt).toBeVisible();
    await page.getByRole('button', { name: '返回修改要求', exact: true }).click();
    const requirements = page.getByRole('textbox', { name: '视频补充要求', exact: true });
    await expect(requirements).toHaveValue('保留的要求');
    await expect(requirements).toHaveJSProperty('readOnly', initialState !== 'draft');
    const menus = page.locator('.reference-slot-menu-button');
    await expect(menus).toHaveCount(3);
    for (const menu of await menus.all()) {
      if (initialState === 'draft') await expect(menu).toBeEnabled();
      else await expect(menu).toBeDisabled();
    }
    expect(writes).toEqual([]);
    if (initialState !== 'draft') {
      await requirements.focus(); await requirements.press('Control+A'); await requirements.press('x');
      await expect(requirements).toHaveValue('保留的要求');
      expect(writes).toEqual([]);
      jobState = 'succeeded';
      await expect(requirements).toBeEditable();
      for (const menu of await menus.all()) await expect(menu).toBeEnabled();
    }
    await requirements.fill('完成后可编辑的要求');
    await expect.poll(() => project.video_rewrite.requirements).toBe('完成后可编辑的要求');
    expect(writes).toHaveLength(1);
    expect(errors).toEqual([]);
  });
}
