import { memberAuthResponse } from './member-auth';
import { expect, test, type Locator, type Page } from '@playwright/test';
import { readFile } from 'node:fs/promises';

test.setTimeout(60_000);
async function inputMenu(page: Page, index: number, action: string) {
  await page.locator('.image-input-grid .ant-card').nth(index).getByRole('button', { name: /菜单$/ }).click();
  await page.locator('.reference-slot-menu').getByRole('menuitem', { name: action, exact: true }).click();
}

const featureKeys = ['subject', 'action', 'scene', 'composition', 'palette', 'lighting', 'texture', 'text'];

// The image workflow moved from a per-feature source matrix to proposal-based
// schemes: analysing the inputs yields three suggested directions, each fixing
// exactly one source per dimension; the user picks one and may add free-form
// requirements. Feature controls remain the persisted per-dimension contract.
async function fixture(page: Page, options: { legacy?: boolean; legacyV2?: boolean; missing?: boolean; empty?: boolean; failCompose?: boolean; unknown?: boolean; imageSize?: [number, number]; actualPictureCase?: boolean } = {}) {
  const calls: { path: string; body: any }[] = []; let failSave = false; let saveFailureWait: Promise<void> | undefined; let frozen: any;
  const note = { id: 'note-v2', platform: 'xiaohongshu', title: '来源笔记', body: '完整原始正文', author: { name: '作者' }, asset_ids: ['main', 'aux'], topics: [], tags: [] };
  const version = { id: 'version-v2', number: 1, title: '图片仿写示例', body: '文案保持独立', script: '', topics: [], asset_ids: ['main', 'aux'], confirmed: true, source_snapshot: note };
  const rewrite: any = { id: 'rewrite-v2', note_id: note.id, title: version.title, versions: [version], confirmed_version_id: version.id, options: { include_body: true }, image_rewrites: [] };
  const assets: any[] = ['main', 'aux', 'unused'].map((id, position) => ({ id, owner_id: note.id, kind: 'image', mime: 'image/png', status: 'ready', position, width: 960, height: 1280 }));
  assets.push({ ...assets[0], id: 'uploaded', owner_id: rewrite.id, position: 3 });
  let jobs: any[] = [];
  function newUnit(id = 'unit-v2') { return { id, workflow_version: 2, edit_intent: { mode: 'flexible' }, version_id: version.id, primary_asset_id: 'main', references: [], source_asset_ids: ['main'], feature_controls: Object.fromEntries(featureKeys.map(key => [key, { mode: 'image', asset_id: 'main' }])), feature_sources: {}, proposals: [], proposals_input_digest: '', selected_proposal_id: '', extra_requirements: '', output: { ratio: '3:4', clarity: 'standard', count: 1 }, text_mode: 'pending', image_text: '', prompt: '', prompt_input_digest: '', prompt_rules_version: 0, analyses: [], batches: [], results: [], revision: 1, status: 'draft', style: { ratio: '3:4' }, created_at: '2026-10-11T00:00:00Z' }; }
  if (!options.empty) rewrite.image_rewrites.push(options.legacy || options.legacyV2 ? { ...newUnit(), workflow_version: options.legacy ? undefined : 2, edit_intent: { mode: options.legacyV2 ? 'preserve_content' : 'feature_mix', style: '旧画法', palette: '旧配色' }, feature_controls: undefined, feature_sources: { subject: 'main', composition: 'main' }, references: [{ asset_id: 'aux' }], prompt: '旧方案实际提示词', results: [{ asset_id: 'legacy-output', prompt: '旧生成依据', source_asset_ids: ['main'], revision: 1 }] } : newUnit());
  const unit = () => rewrite.image_rewrites.at(-1);
  // Real intrinsic dimensions catch image-component defaults overriding preview heights.
  const imageSize = options.imageSize || [960, 1280];
  const imageData = await page.evaluate(([width, height]) => {
    const canvas = document.createElement('canvas'); canvas.width = width; canvas.height = height;
    const context = canvas.getContext('2d')!; context.fillStyle = '#b32119'; context.fillRect(0, 0, width, height);
    context.strokeStyle = '#fff'; context.lineWidth = 12; context.strokeRect(12, 12, width - 24, height - 24);
    return canvas.toDataURL('image/png').split(',')[1];
  }, imageSize);
  const png = Buffer.from(imageData, 'base64');
  const actualPictures = options.actualPictureCase ? {
    // Local acceptance uses the user's existing pictures. Clean checkouts use
    // the self-contained placeholder; assertions concern contracts, not art.
    main: await readFile('../data/assets/64bf8dc524934e286fbbfce7ae48a8e2.jpg').catch(error => { if (error.code === 'ENOENT') return png; throw error; }),
    aux: await readFile('../data/assets/a20c8cc02068ec7ecdab3c6d458cd6f5.webp').catch(error => { if (error.code === 'ENOENT') return png; throw error; }),
  } : undefined;
  const basis = () => JSON.stringify([unit().primary_asset_id, unit().references.map((r: any) => r.asset_id || `official:${r.library_id}`)]);
  const semantics = () => JSON.stringify([basis(), unit().selected_proposal_id, unit().extra_requirements, unit().output.ratio]);
  // Three suggested directions: keep everything from the primary picture, let
  // the auxiliary pictures lead subject and action while the primary only
  // supplies the scene, or delegate every dimension to the model.
  function buildProposals(u: any) {
    const primary = u.primary_asset_id; const first = u.references[0]?.asset_id || primary; const second = u.references[1]?.asset_id || first;
    const from = (id: string) => Object.fromEntries(featureKeys.map(key => [key, { mode: 'image', asset_id: id }]));
    return [
      { id: 'proposal-primary', title: '整体沿用主图', summary: '每个维度均取自主图', feature_controls: from(primary) },
      { id: 'proposal-aux', title: '辅助图主导主体与动作', summary: '主体与动作来自辅助图，场景取自主图，其余交给模型', feature_controls: { ...from(primary), subject: { mode: 'image', asset_id: first }, action: { mode: 'image', asset_id: second }, composition: { mode: 'auto' }, palette: { mode: 'auto' }, lighting: { mode: 'auto' }, texture: { mode: 'auto' }, text: { mode: 'auto' } } },
      { id: 'proposal-auto', title: '全部交给模型', summary: '所有维度由模型设计', feature_controls: Object.fromEntries(featureKeys.map(key => [key, { mode: 'auto' }])) },
    ];
  }
  const selectedControls = (u: any) => u.proposals?.find((p: any) => p.id === u.selected_proposal_id)?.feature_controls || u.feature_controls;
  const effectiveSources = (controls: any) => Object.fromEntries(Object.entries(controls).filter(([, c]: any) => c.mode === 'image' && c.asset_id).map(([key, c]: any) => [key, c.asset_id]));
  const sizes = [{ ratio: '3:4', clarity: 'standard', width: 960, height: 1280 }, { ratio: '3:4', clarity: 'high', width: 1536, height: 2048 }, { ratio: '1:1', clarity: 'standard', width: 1024, height: 1024 }, { ratio: '1:1', clarity: 'high', width: 2048, height: 2048 }, { ratio: '9:16', clarity: 'standard', width: 720, height: 1280 }, { ratio: '9:16', clarity: 'high', width: 1152, height: 2048 }];
  function job(kind: string) { const id = `${kind}-${calls.length}`; jobs = [{ id, kind, entity_id: rewrite.id, scene_id: unit().id, state: 'succeeded' }]; return { job_id: id }; }
  await page.route('**/api/v1/**', async route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const req = route.request(); const url = new URL(req.url()); const path = url.pathname.replace('/api/v1', ''); const method = req.method();
    const json = (data: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) });
    if (method !== 'GET') { expect(req.headers()['x-csrf-token']).toBe('test-csrf'); calls.push({ path, body: req.postDataJSON() }); }
    if (path === '/session') return json({ csrf_token: 'test-csrf' });
    if (path === '/workspaces') return json({ items: [{ platform: 'xiaohongshu' }], total: 1 });
    if (path === '/jobs') return json({ items: jobs, total: jobs.length });
    if (path === '/collections') return json({ items: [], total: 0 });
    if (path === '/rewrites') return json({ items: [rewrite], total: 1 });
    if (path === `/rewrites/${rewrite.id}`) return json(frozen || { rewrite, note, assets, videos: [] });
    if (path === '/image-capabilities') return json({ image_configured: true, vision_configured: !options.missing, text_configured: true, supported: true, max_count: 4, prompt_rules_version: 6, sizes });
    if (path === '/style-library') return json({ items: [{ id: 'official-card', title: '官方知识卡片', category: 'education', tags: ['简洁'], credit: '测试授权图片' }], categories: [{ id: 'education', name: '教育知识' }], total: 1 });
    if (path.endsWith('/file')) {
      const picture = path.split('/').at(-2);
      if (actualPictures && picture === 'main') return route.fulfill({ contentType: 'image/jpeg', body: actualPictures.main });
      if (actualPictures && picture === 'aux') return route.fulfill({ contentType: 'image/webp', body: actualPictures.aux });
      return route.fulfill({ contentType: 'image/png', body: png });
    }
    if (path === '/media-library') return json({ items: unit()?.results.map((r: any) => ({ id: r.asset_id, rewrite_id: rewrite.id, origin: 'image_rewrite', project_title: rewrite.title, asset: { ...assets[0], id: r.asset_id }, revision: r.revision })) || [], total: unit()?.results.length || 0 });
    if (path.endsWith('/images') && method === 'POST') { const b = req.postDataJSON(); expect(b.workflow_version).toBe(2); expect(b.edit_intent.mode).toBe('flexible'); rewrite.image_rewrites.push({ ...newUnit(`unit-${calls.length}`), primary_asset_id: b.primary_asset_id, source_asset_ids: [b.primary_asset_id], feature_controls: Object.fromEntries(featureKeys.map(key => [key, { mode: 'image', asset_id: b.primary_asset_id }])), references: b.references || [] }); return json(unit(), 201); }
    if (/\/images\/[^/]+$/.test(path) && method === 'PATCH') {
      if (failSave) { failSave = false; await saveFailureWait; saveFailureWait = undefined; return json({ error: '模拟草稿保存失败' }, 500); }
      const b = req.postDataJSON(); expect(b.revision).toBe(unit().revision); expect(b.analyses).toBeUndefined();
      const oldBasis = basis(); const oldSemantics = semantics();
      Object.assign(unit(), b); unit().references = (b.references || []).map((r: any) => r.asset_id ? r : { ...r, asset_id: `official-snapshot-${r.library_id}` });
      if (oldBasis !== basis()) unit().proposals_input_digest = '';
      if (oldSemantics !== semantics()) unit().prompt_input_digest = '';
      unit().revision++; return json(unit());
    }
    if (path.endsWith('/analyze')) {
      const b = req.postDataJSON(); expect(b.revision).toBe(unit().revision); expect(b.suggest_proposals).toBe(true);
      unit().analyses = [unit().primary_asset_id, ...unit().references.map((r: any) => r.asset_id)].map((asset_id: string) => ({ asset_id, width: 960, height: 1280, recognized_text: asset_id === unit().primary_asset_id ? 'AI生成 WORKBUDD>' : '辅助图文字', features: { subject: asset_id === unit().primary_asset_id ? '合拢的书本' : '蓝发猫耳少女穿着女仆装', action: asset_id === unit().primary_asset_id ? '静置，无互动' : '右手挥手致意，左手张开', scene: asset_id === unit().primary_asset_id ? '纯色背景，无其他环境元素' : '开阔背景', composition: '居中构图', palette: asset_id === unit().primary_asset_id ? '红白色' : '蓝白色', lighting: '平光', texture: '扁平插画', text: '分栏版式' } }));
      if (options.actualPictureCase) {
        unit().analyses.find((a: any) => a.asset_id === 'main').features = { subject: '一位女性、拟人化肝脏角色和时钟', action: '女性卧床安睡，肝脏角色举手打鼾', scene: '四角叶片与果实藤蔓边框', composition: '人物居左下，文字分三块布局，留白充足', palette: '暖米色、绿色与橙色', lighting: '均匀平光', texture: '纸张水彩质感', text: '主标题居顶，关键词突出' };
        const auxiliary = unit().analyses.find((a: any) => a.asset_id === 'aux');
        if (auxiliary) auxiliary.features = { subject: '猫耳少女身着女仆装，头戴蕾丝发饰，身后有鲸鱼尾鳍', action: '右手挥手致意，左手张开', scene: '纯背景', composition: '正面全身构图', palette: '深蓝与白，浅蓝与金色强调', lighting: '均匀柔光', texture: '数字插画，线条和色块清晰', text: '无可见文字' };
      }
      unit().image_text = 'AI生成 WORKBUDD>'; unit().text_mode = 'keep';
      unit().proposals = buildProposals(unit()); unit().proposals_input_digest = 'analyzed-input';
      unit().prompt_input_digest = ''; unit().revision++; return json(job('image_analyze'), 202);
    }
    if (path.endsWith('/compose-prompt')) {
      expect(req.postDataJSON().revision).toBe(unit().revision);
      if (options.failCompose) { jobs = [{ id: 'compose-failed', kind: 'image_compose', entity_id: rewrite.id, scene_id: unit().id, state: 'failed', error: '编排失败' }]; unit().error = '编排失败'; return json({ job_id: 'compose-failed' }, 202); }
      const controls = selectedControls(unit());
      const descriptions = Object.entries(controls).map(([key, c]: any) => `${key}：${c.mode === 'custom' ? c.value : c.mode === 'auto' ? '由模型设计' : unit().analyses.find((a: any) => a.asset_id === c.asset_id)?.features[key] || '待分析'}`);
      const extra = (unit().extra_requirements || '').trim();
      unit().prompt = `${descriptions.join('。')}。${extra ? `${extra}。` : ''}仅使用选中来源，不添加水印。`;
      unit().prompt_input_digest = 'confirmed-input'; unit().prompt_rules_version = 6; unit().revision++; return json(job('image_compose'), 202);
    }
    if (path.endsWith('/generate')) {
      const b = req.postDataJSON(); expect(b.revision).toBe(unit().revision); expect(b.prompt).toBe(unit().prompt); expect(b.request_id).toMatch(/^[\w-]+$/);
      const controls = selectedControls(unit()); const sources = effectiveSources(controls);
      const batch: any = { id: `batch-${calls.length}`, request_id: b.request_id, revision: unit().revision, edit_intent: structuredClone(unit().edit_intent), feature_controls: structuredClone(controls), feature_sources: { ...sources }, input_asset_ids: [unit().primary_asset_id, ...unit().references.map((r: any) => r.asset_id)].filter(id => Object.values(sources).includes(id)), prompt: unit().prompt, output: { ...unit().output }, state: options.unknown ? 'submission_unknown' : 'complete', items: Array.from({ length: unit().output.count }, (_, index) => ({ index, state: options.unknown ? index === 0 ? 'submission_unknown' : 'waiting' : 'succeeded' })) };
      unit().batches.push(batch);
      if (options.unknown) { jobs = [{ id: 'unknown-job', kind: 'image_rewrite', entity_id: rewrite.id, scene_id: unit().id, state: 'submission_unknown' }]; unit().error = '提交结果不明，已暂停'; return json({ job_id: 'unknown-job' }, 202); }
      const size = sizes.find(s => s.ratio === unit().output.ratio && s.clarity === unit().output.clarity)!;
      unit().results.push(...batch.items.map((_: any, i: number) => ({ asset_id: `output-${calls.length}-${i}`, batch_id: batch.id, primary_asset_id: unit().primary_asset_id, revision: unit().revision, prompt: batch.prompt, edit_intent: structuredClone(batch.edit_intent), feature_controls: structuredClone(batch.feature_controls), feature_sources: { ...batch.feature_sources }, prompt_rules_version: 6, source_asset_ids: batch.input_asset_ids, model: 'mock-image-model', width: size.width, height: size.height, created_at: '2026-10-12T00:01:00Z' })));
      return json(job('image_rewrite'), 202);
    }
    if (path.endsWith('/apply')) { const b = req.postDataJSON(); const source = rewrite.versions.find((v: any) => v.id === b.version_id); const next = { ...source, id: 'version-applied', number: 2, confirmed: false, parent_version_id: source.id, asset_ids: source.asset_ids.map((id: string) => id === unit().primary_asset_id ? b.asset_id : id) }; rewrite.versions.push(next); unit().applied_version_id = next.id; return json(next, 201); }
    return json({ items: [], total: 0 });
  });
  await page.goto('/'); await page.getByRole('menuitem', { name: /仿写库/ }).click(); await page.getByRole('tab', { name: '图片仿写', exact: true }).click();
  return { calls, unit, rewrite, failNextSave: (wait?: Promise<void>) => { failSave = true; saveFailureWait = wait; }, freeze: () => { frozen = structuredClone({ rewrite, note, assets, videos: [] }); }, unfreeze: () => { frozen = undefined; } };
}
// Analysing the inputs yields three suggested directions; the caller picks one
// before composing is allowed.
async function analyze(page: Page, direction: 'primary' | 'aux' | 'auto' = 'primary') {
  await page.getByRole('button', { name: '分析图片', exact: true }).click();
  await expect(page.getByRole('radio')).toHaveCount(3);
  await page.getByRole('radio').nth(direction === 'aux' ? 1 : direction === 'auto' ? 2 : 0).check();
}
async function compose(page: Page, direction: 'primary' | 'aux' | 'auto' = 'primary') {
  await analyze(page, direction);
  await page.getByRole('button', { name: '生成提示词', exact: true }).click();
  await expect(page.getByRole('textbox', { name: '生成提示词', exact: true })).toHaveValue(/仅使用选中来源/);
}

async function selectAuxiliary(page: Page) {
  await page.getByRole('button', { name: /^选择辅助参考图 / }).first().click();
  const dialog = page.getByRole('dialog', { name: '选择辅助参考图' });
  await dialog.getByRole('checkbox', { name: '选择图片 2' }).check();
  await dialog.getByRole('button', { name: '确认选择' }).click();
}

test('图片仿写提示词审阅不显示思维链或注释', async ({ page }) => {
  await fixture(page);
  await page.getByRole('button', { name: '2. 审阅提示词', exact: true }).click();
  await expect(page.locator('.image-prompt-review > .ant-typography')).toHaveCount(0);
  await expect(page.locator('.image-requirement-summary, .image-intent-review')).toHaveCount(0);
  await expect(page.getByRole('textbox', { name: '生成提示词', exact: true })).toBeVisible();
});

test('角色清晰，删除文字组件且旧OCR不自动进入新提示词，编排后才允许生图', async ({ page }) => {
  const f = await fixture(page); await expect(page.getByText('被仿写图', { exact: true })).toBeVisible(); await expect(page.getByText('辅助参考图 1', { exact: true })).toBeVisible();
  await expect(page.getByRole('textbox', { name: '生成提示词', exact: true })).toHaveCount(0); expect(f.calls).toHaveLength(0);
  await analyze(page); await expect(page.getByRole('button', { name: '生成提示词', exact: true })).toBeEnabled();
  await expect(page.getByRole('textbox', { name: '确认图中文字' })).toHaveCount(0);
  await page.getByRole('button', { name: '生成提示词', exact: true }).click(); await expect(page.getByRole('textbox', { name: '生成提示词', exact: true })).not.toHaveValue(/WORKBUDD|AI生成/);
  expect(f.calls.some(c => c.path.endsWith('/generate'))).toBe(false); await page.getByRole('button', { name: '确认并生成 1 张' }).click();
  await expect(page.getByRole('img', { name: '图片仿写结果', exact: true })).toBeVisible(); await page.getByRole('button', { name: '本次生成依据' }).click();
  await expect(page.getByRole('img', { name: '本次被仿写图' })).toBeVisible(); expect(f.calls.filter(c => c.path.endsWith('/generate'))).toHaveLength(1);
});

test('辅助图可预览筛选并取消选择，建议方向为每个维度指定唯一来源', async ({ page }) => {
  const f = await fixture(page); await page.getByRole('button', { name: /^选择辅助参考图 / }).first().click();
  const dialog = page.getByRole('dialog', { name: '选择辅助参考图' }); await dialog.getByRole('checkbox', { name: '选择图片 2' }).check(); await dialog.getByRole('button', { name: '取消', exact: true }).click(); expect(f.unit().references).toHaveLength(0);
  await page.getByRole('button', { name: /^选择辅助参考图 / }).first().click(); await dialog.getByRole('checkbox', { name: '选择图片 2' }).check(); await dialog.getByRole('button', { name: '确认选择' }).click();
  await analyze(page, 'aux');
  await expect.poll(() => f.unit().selected_proposal_id).toBe('proposal-aux');
  const controls = f.unit().proposals[1].feature_controls;
  expect(Object.keys(controls).sort()).toEqual([...featureKeys].sort());
  // 每个维度要么指向唯一一张图（image 模式必带 asset_id），要么整体交给模型（auto 模式不带来源）
  for (const [key, control] of Object.entries(controls) as [string, any][]) {
    if (control.mode === 'image') { expect(Object.keys(control).sort(), key).toEqual(['asset_id', 'mode']); expect(['main', 'aux'], key).toContain(control.asset_id); }
    else expect(Object.keys(control).sort(), key).toEqual(['mode']);
  }
  await expect(page.getByRole('textbox', { name: /自定义要求/ })).toHaveCount(0);
  expect(f.calls.filter(c => c.path.endsWith('/analyze'))).toHaveLength(1);
  expect(f.calls.filter(c => c.path.endsWith('/generate'))).toHaveLength(0);
});

test('异质辅助图可作为主体，主图仅提供所选背景，历史依据不随当前选择改变', async ({ page }) => {
  const f = await fixture(page); await selectAuxiliary(page); await analyze(page, 'aux');
  await page.getByRole('button', { name: '生成提示词', exact: true }).click();
  const prompt = page.getByRole('textbox', { name: '生成提示词', exact: true });
  await expect(prompt).toHaveValue(/蓝发猫耳少女穿着女仆装/); await expect(prompt).toHaveValue(/纯色背景/);
  await expect(prompt).not.toHaveValue(/合拢的书本|WORKBUDD|AI生成/);
  await expect(page.locator('.image-requirement-summary, .image-intent-review')).toHaveCount(0);
  await expect(page.getByRole('textbox', { name: '生成提示词', exact: true })).toBeVisible();
  expect(f.calls.filter(c => c.path.endsWith('/generate'))).toHaveLength(0);
  for (const call of f.calls.filter(c => /\/images\/[^/]+$/.test(c.path))) {
    expect(call.body.text_mode).toBeUndefined(); expect(call.body.image_text).toBeUndefined();
  }
  await page.getByRole('button', { name: '确认并生成 1 张' }).click();
  await expect(page.getByRole('img', { name: '图片仿写结果', exact: true })).toBeVisible();
  expect(f.unit().results[0].source_asset_ids).toEqual(['main', 'aux']);
  await page.getByRole('button', { name: '本次生成依据' }).click();
  await expect(page.locator('.image-result-feature-controls')).toContainText('主体与对象：图片 2');
  await page.getByRole('button', { name: '1. 选图与方案' }).click(); await page.getByRole('radio').first().check();
  await expect.poll(() => f.unit().selected_proposal_id).toBe('proposal-primary');
  await page.getByRole('button', { name: '2. 审阅提示词' }).click();
  await expect(page.getByText('提示词待更新', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: '确认并生成 1 张' })).toBeDisabled();
  await page.getByRole('button', { name: '3. 审阅结果' }).click();
  await expect(page.getByText('当前修改尚未生成图片，历史结果使用当时的生成依据。')).toBeVisible();
  await page.getByRole('button', { name: '历史结果（1）' }).click();
  await page.getByRole('button', { name: '本次生成依据' }).click();
  await expect(page.locator('.image-result-feature-controls')).toContainText('主体与对象：图片 2');
  expect(f.calls.filter(c => c.path.endsWith('/generate'))).toHaveLength(1);
});

test('旧编排规则的提示词仅可查看，重新编排后才可再次生成', async ({ page }) => {
  const f = await fixture(page); await compose(page);
  f.unit().prompt_rules_version = 0;
  await expect(page.getByText('旧提示词需按当前特征重新编排')).toBeVisible({ timeout: 10_000 });
  await expect(page.getByRole('button', { name: '确认并生成 1 张' })).toBeDisabled();
  await page.getByRole('button', { name: '重新编排提示词' }).click();
  await expect(page.getByRole('button', { name: '确认并生成 1 张' })).toBeEnabled();
  expect(f.calls.filter(c => c.path.endsWith('/generate'))).toHaveLength(0);
});

test('自存提示词，修改数量清晰度不再编排，更换方向必须更新', async ({ page }) => {
  const f = await fixture(page); await compose(page); await page.getByRole('textbox', { name: '生成提示词', exact: true }).fill('用户确认的新提示词，不添加额外文字');
  await page.getByRole('combobox', { name: '图片清晰度' }).click(); await page.getByRole('option', { name: '高清', exact: true }).click();
  await page.getByRole('combobox', { name: '生成数量' }).click(); await page.getByRole('option', { name: '3 张', exact: true }).click(); await expect(page.getByText('1536 × 2048', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: '确认并生成 3 张' }).click(); await expect(page.getByRole('img', { name: '图片仿写结果', exact: true })).toHaveCount(3);
  expect(f.calls.filter(c => c.path.endsWith('/compose-prompt'))).toHaveLength(1); expect(f.calls.find(c => c.path.endsWith('/generate'))?.body.prompt).toBe('用户确认的新提示词，不添加额外文字');
  await page.getByRole('button', { name: '1. 选图与方案' }).click(); await page.getByRole('radio').nth(2).check();
  await page.getByRole('button', { name: '2. 审阅提示词' }).click(); await expect(page.getByRole('button', { name: '确认并生成 3 张' })).toBeDisabled();
});

test('保存失败保留编辑并阻止生成，重试可恢复', async ({ page }) => {
  const f = await fixture(page); await compose(page);
  let release!: () => void; f.failNextSave(new Promise<void>(resolve => { release = resolve; }));
  const saves = f.calls.filter(c => c.path.endsWith('/images/unit-v2')).length + 1;
  const saveStarted = page.waitForRequest(request => request.method() === 'PATCH' && new URL(request.url()).pathname.endsWith('/images/unit-v2'));
  await page.getByRole('textbox', { name: '生成提示词', exact: true }).fill('保存失败仍保留这段文字');
  await saveStarted; await expect.poll(() => f.calls.filter(c => c.path.endsWith('/images/unit-v2')).length).toBe(saves);
  try { await page.getByRole('button', { name: '确认并生成 1 张' }).click(); } finally { release(); }
  await expect(page.getByText(/^草稿保存失败：/)).toBeVisible(); expect(f.calls.some(c => c.path.endsWith('/generate'))).toBe(false);
  expect(f.calls.filter(c => c.path.endsWith('/images/unit-v2'))).toHaveLength(saves);
  await expect(page.getByRole('textbox', { name: '生成提示词', exact: true })).toHaveValue('保存失败仍保留这段文字'); await page.getByRole('button', { name: '重试保存' }).click();
  await expect(page.getByText(/^草稿保存失败：/)).toHaveCount(0); await page.getByRole('button', { name: '确认并生成 1 张' }).click(); await expect(page.getByRole('img', { name: '图片仿写结果', exact: true })).toBeVisible();
});

test('采用只替换主图，文案未保存编辑与辅助素材保留', async ({ page }) => {
  const f = await fixture(page); await page.getByRole('tab', { name: '文案编辑', exact: true }).click(); await page.getByRole('textbox', { name: '仿写正文' }).fill('尚未保存的独立文案');
  await page.getByRole('tab', { name: '图片仿写', exact: true }).click(); await compose(page); await page.getByRole('button', { name: '确认并生成 1 张' }).click(); await expect(page.getByRole('button', { name: '采用此图', exact: true })).toBeEnabled(); await page.getByRole('button', { name: '采用此图', exact: true }).click();
  await page.getByRole('tab', { name: '文案编辑', exact: true }).click(); await expect(page.getByRole('textbox', { name: '仿写正文' })).toHaveValue('尚未保存的独立文案');
  expect(f.rewrite.versions[1].asset_ids).toContain('aux'); expect(f.rewrite.versions[1].asset_ids).not.toContain('main'); expect(f.rewrite.versions[0].body).toBe('文案保持独立');
});

test('官方图片只作为辅助图，图库没有自动付费行为', async ({ page }) => {
  const f = await fixture(page); await page.getByRole('button', { name: /^选择辅助参考图 / }).first().click(); const dialog = page.getByRole('dialog', { name: '选择辅助参考图' });
  await dialog.getByText('官方图库', { exact: true }).click(); await dialog.getByRole('button', { name: '预览官方知识卡片' }).click(); await expect(page.getByRole('img', { name: '官方知识卡片大图' })).toBeVisible(); await page.getByRole('button', { name: '关闭预览' }).click();
  await dialog.getByRole('checkbox', { name: '选择官方知识卡片' }).check(); await dialog.getByRole('button', { name: '确认选择' }).click(); await expect.poll(() => f.unit().references[0]?.library_id).toBe('official-card');
  expect(f.calls.filter(c => /\/(analyze|compose-prompt|generate)$/.test(c.path))).toHaveLength(0);
});

test('旧方案只读，不自动迁移或生图', async ({ page }) => {
  const f = await fixture(page, { legacy: true }); await expect(page.getByRole('button', { name: '以此创建新版方案' })).toBeVisible(); await expect(page.getByRole('button', { name: /确认并生成/ })).toHaveCount(0); expect(f.calls).toHaveLength(0);
  await page.getByRole('button', { name: '原方案提示词' }).click(); await expect(page.getByText('旧方案实际提示词', { exact: true })).toBeVisible();
});

test('旧轮询不覆盖已保存编辑，新建方案前等待草稿保存', async ({ page }) => {
  const f = await fixture(page); await compose(page); f.freeze();
  await page.getByRole('textbox', { name: '生成提示词', exact: true }).fill('最终已审阅提示词');
  await expect.poll(() => f.unit().prompt).toBe('最终已审阅提示词');
  await page.waitForTimeout(2200);
  await expect(page.getByRole('textbox', { name: '生成提示词', exact: true })).toHaveValue('最终已审阅提示词');
  await page.getByRole('textbox', { name: '生成提示词', exact: true }).fill('切换前应保存的提示词');
  await page.getByRole('button', { name: /新方案/, exact: false }).click();
  await expect(page.getByRole('dialog', { name: '选择被仿写图' })).toBeVisible();
  expect(f.unit().prompt).toBe('切换前应保存的提示词');
  expect(f.calls.filter(c => c.path.endsWith('/generate'))).toHaveLength(0);
});

test('配置缺失和编排失败均不产生图片请求', async ({ page }) => {
  let f = await fixture(page, { missing: true }); await expect(page.getByRole('button', { name: '图片分析待配置' })).toBeDisabled(); expect(f.calls).toHaveLength(0);
  await page.unroute('**/api/v1/**'); f = await fixture(page, { failCompose: true }); await analyze(page); await page.getByRole('button', { name: '生成提示词', exact: true }).click();
  await expect(page.getByText('编排失败', { exact: true })).toBeVisible(); expect(f.calls.some(c => c.path.endsWith('/generate'))).toBe(false);
});

test('提交结果不明暂停剩余图片，禁止直接重投', async ({ page }) => {
  const f = await fixture(page, { unknown: true }); await compose(page); await page.getByRole('button', { name: '确认并生成 1 张' }).click(); await expect(page.getByText('提交结果不明，已暂停')).toBeVisible();
  await expect(page.getByRole('button', { name: '返回修改提示词' })).toBeDisabled(); await page.getByRole('button', { name: '重试当前图片' }).click(); await expect(page.getByRole('dialog')).toContainText('已核实供应商没有生成结果'); await page.getByRole('button', { name: '返回核实' }).click();
  expect(f.calls.filter(c => c.path.endsWith('/generate'))).toHaveLength(1); expect(f.calls.some(c => c.path.endsWith('/resume'))).toBe(false);
});

for (const width of [1536, 825, 597]) test(`Edge ${width}px 建议方向自由选择且附加要求进入提示词，无预设特例`, async ({ page }) => {
  await page.setViewportSize({ width, height: 900 }); const f = await fixture(page, { actualPictureCase: true });
  await selectAuxiliary(page); await page.getByRole('button', { name: '分析图片', exact: true }).click();
  await expect(page.getByRole('radio')).toHaveCount(3);
  for (const option of await page.getByRole('radio').all()) await expect(option).toBeEnabled();
  await expect(page.getByRole('button', { name: '保留主图内容', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /使用数字插画|使用蓝白/ })).toHaveCount(0);
  await page.getByRole('radio').nth(1).check();
  await page.getByRole('textbox', { name: '附加要求', exact: true }).fill('赭石与墨绿，背景低饱和；木刻版画，粗线条和纸张颗粒');
  await page.locator('.image-proposals').scrollIntoViewIfNeeded();
  await page.screenshot({ path: `../data/verification/image-flexible-controls-edge-${width}.png` });
  await page.getByRole('button', { name: '生成提示词', exact: true }).click();
  const prompt = page.getByRole('textbox', { name: '生成提示词', exact: true });
  await expect(prompt).toHaveValue(/猫耳少女身着女仆装/); await expect(prompt).toHaveValue(/右手挥手致意，左手张开/);
  await expect(prompt).toHaveValue(/四角叶片与果实藤蔓边框/);
  await expect(prompt).toHaveValue(/赭石与墨绿/); await expect(prompt).toHaveValue(/木刻版画/);
  await expect(prompt).not.toHaveValue(/一位女性、拟人化肝脏角色和时钟|蓝白|暖米色|WORKBUDD|AI生成/);
  await expect(page.locator('.image-requirement-summary, .image-intent-review')).toHaveCount(0);
  await expect(page.getByRole('textbox', { name: '生成提示词', exact: true })).toBeVisible();
  expect(f.calls.filter(c => c.path.endsWith('/generate'))).toHaveLength(0);
});

test('修改附加要求使提示词过期，历史保持原快照', async ({ page }) => {
  const f = await fixture(page); await analyze(page);
  await page.getByRole('textbox', { name: '附加要求', exact: true }).fill('银灰和紫色');
  await page.getByRole('button', { name: '生成提示词', exact: true }).click(); await expect(page.getByRole('textbox', { name: '生成提示词', exact: true })).toHaveValue(/银灰和紫色/);
  await page.getByRole('button', { name: '确认并生成 1 张' }).click(); await expect(page.getByRole('img', { name: '图片仿写结果', exact: true })).toBeVisible();
  const snapshot = structuredClone(f.unit().results[0]);
  await page.getByRole('button', { name: '1. 选图与方案' }).click(); await page.getByRole('textbox', { name: '附加要求', exact: true }).fill('黑白单色');
  await page.getByRole('button', { name: '2. 审阅提示词' }).click(); await expect(page.getByRole('button', { name: '确认并生成 1 张' })).toBeDisabled();
  await expect.poll(() => f.unit().extra_requirements).toBe('黑白单色');
  expect(f.unit().results[0]).toEqual(snapshot); expect(f.calls.filter(c => c.path.endsWith('/generate'))).toHaveLength(1);
});

test('全部交给模型时主图仅作为采用位置，不进入模型请求', async ({ page }) => {
  const f = await fixture(page); await analyze(page, 'auto');
  await page.getByRole('button', { name: '生成提示词', exact: true }).click(); await expect(page.getByRole('textbox', { name: '生成提示词', exact: true })).toHaveValue(/由模型设计/);
  expect(f.calls.filter(c => c.path.endsWith('/analyze'))).toHaveLength(1);
  await page.getByRole('button', { name: '确认并生成 1 张' }).click(); await expect(page.getByRole('img', { name: '图片仿写结果', exact: true })).toBeVisible();
  expect(f.unit().batches[0].input_asset_ids).toEqual([]); expect(f.unit().primary_asset_id).toBe('main');
});

test('附加要求可选且支持非人物场景，长度限制1000字', async ({ page }) => {
  const f = await fixture(page); await analyze(page);
  const extra = page.getByRole('textbox', { name: '附加要求', exact: true });
  await expect(extra).toHaveAttribute('maxlength', '1000');
  await expect(page.getByRole('button', { name: '生成提示词', exact: true })).toBeEnabled();
  await extra.fill('一只陶瓷茶杯静置在石制桌面上，俯视构图，写实摄影，不添加文字');
  await page.getByRole('button', { name: '生成提示词', exact: true }).click(); await expect(page.getByRole('textbox', { name: '生成提示词', exact: true })).toHaveValue(/陶瓷茶杯/);
  expect(f.calls.filter(c => c.path.endsWith('/analyze'))).toHaveLength(1); expect(f.calls.filter(c => c.path.endsWith('/generate'))).toHaveLength(0);
});

test('未选择建议方向时禁止编排，选择后即可生成提示词', async ({ page }) => {
  const f = await fixture(page);
  await expect(page.getByRole('button', { name: '生成提示词', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: '分析图片', exact: true }).click();
  await expect(page.getByRole('radio')).toHaveCount(3);
  await expect(page.getByRole('button', { name: '生成提示词', exact: true })).toBeDisabled();
  await page.getByRole('radio').first().check();
  await expect(page.getByRole('button', { name: '生成提示词', exact: true })).toBeEnabled();
  expect(f.calls.filter(c => c.path.endsWith('/generate'))).toHaveLength(0);
});

test('借用辅助图的主体和动作时，实际输入只含所选维度对应的图片', async ({ page }) => {
  const f = await fixture(page); await selectAuxiliary(page); await analyze(page, 'aux');
  await page.getByRole('button', { name: '生成提示词', exact: true }).click(); await expect(page.getByRole('textbox', { name: '生成提示词', exact: true })).toHaveValue(/右手挥手/);
  await page.getByRole('button', { name: '确认并生成 1 张' }).click(); await expect(page.getByRole('img', { name: '图片仿写结果', exact: true })).toBeVisible();
  expect(f.unit().batches[0].input_asset_ids).toEqual(['main', 'aux']);
  expect(f.unit().batches[0].feature_sources).toEqual({ subject: 'aux', action: 'aux', scene: 'main' });
});

test('移除辅助图后需重新分析，不自动沿用旧方向', async ({ page }) => {
  const f = await fixture(page); await selectAuxiliary(page); await analyze(page, 'aux');
  await expect(page.getByRole('button', { name: '生成提示词', exact: true })).toBeEnabled();
  await inputMenu(page, 1, '移除');
  await expect(page.getByText('图片已更换，请重新点击“分析图片”更新三个方案。')).toBeVisible();
  await expect(page.getByRole('button', { name: '生成提示词', exact: true })).toBeDisabled();
  await expect.poll(() => f.unit().selected_proposal_id).toBe('');
  await page.getByRole('button', { name: '分析图片', exact: true }).click();
  await expect(page.getByRole('radio')).toHaveCount(3);
  await page.getByRole('radio').first().check();
  await expect(page.getByRole('button', { name: '生成提示词', exact: true })).toBeEnabled();
  expect(f.calls.filter(c => c.path.endsWith('/generate'))).toHaveLength(0);
});

test('更换主图后需重新分析，附加要求保留', async ({ page }) => {
  const f = await fixture(page); await analyze(page);
  await page.getByRole('textbox', { name: '附加要求', exact: true }).fill('黑金');
  await inputMenu(page, 0, '更换'); const dialog = page.getByRole('dialog', { name: '选择被仿写图' });
  await dialog.getByRole('checkbox', { name: '选择图片 3' }).check(); await dialog.getByRole('button', { name: '确认选择' }).click();
  await expect.poll(() => f.unit().primary_asset_id).toBe('unused'); await expect.poll(() => f.unit().selected_proposal_id).toBe('');
  await expect(page.getByRole('textbox', { name: '附加要求', exact: true })).toHaveValue('黑金');
  await expect(page.getByRole('button', { name: '生成提示词', exact: true })).toBeDisabled();
});

test('上一版保留模式只读，显式复制才创建默认可编辑方案，历史原样保留', async ({ page }) => {
  const f = await fixture(page, { legacyV2: true }); const before = structuredClone(f.unit());
  await expect(page.locator('.image-feature-matrix')).toHaveCount(0); expect(f.calls).toHaveLength(0);
  await page.getByRole('button', { name: '以此创建新版方案' }).click();
  await expect(page.getByRole('button', { name: '分析图片', exact: true })).toBeEnabled();
  await expect(page.getByRole('textbox', { name: '附加要求', exact: true })).toBeVisible();
  expect(f.rewrite.image_rewrites[0]).toEqual(before); expect(f.unit().references).toEqual([{ asset_id: 'aux' }]);
  expect(f.calls).toHaveLength(1); expect(f.calls[0].path).toMatch(/\/images$/); expect(f.calls[0].body.edit_intent.mode).toBe('flexible');
});

test('两张辅助图可独立贡献，移除第一张后重新分析再选方向', async ({ page }) => {
  const f = await fixture(page); await selectAuxiliary(page);
  await page.getByRole('button', { name: /^选择辅助参考图 / }).first().click(); const dialog = page.getByRole('dialog', { name: '选择辅助参考图' });
  await dialog.getByRole('checkbox', { name: '选择图片 3' }).check(); await dialog.getByRole('button', { name: '确认选择' }).click();
  await analyze(page, 'aux');
  await page.getByRole('button', { name: '生成提示词', exact: true }).click(); await expect(page.getByRole('textbox', { name: '生成提示词', exact: true })).toHaveValue(/仅使用选中来源/);
  await page.getByRole('button', { name: '确认并生成 1 张' }).click(); await expect(page.getByRole('img', { name: '图片仿写结果', exact: true })).toBeVisible();
  expect(f.unit().batches[0].feature_sources).toEqual({ subject: 'aux', action: 'unused', scene: 'main' });
  expect(f.unit().batches[0].input_asset_ids).toEqual(['main', 'aux', 'unused']);
  await page.getByRole('button', { name: '1. 选图与方案' }).click();
  await inputMenu(page, 1, '移除');
  await expect(page.getByRole('button', { name: '生成提示词', exact: true })).toBeDisabled();
  await expect.poll(() => f.unit().selected_proposal_id).toBe('');
  await page.getByRole('button', { name: '分析图片', exact: true }).click(); await expect(page.getByRole('radio')).toHaveCount(3);
  await page.getByRole('radio').first().check();
  await expect(page.getByRole('button', { name: '生成提示词', exact: true })).toBeEnabled();
  expect(f.calls.filter(c => c.path.endsWith('/generate'))).toHaveLength(1);
});

for (const width of [1536, 825, 597]) test(`Edge ${width}px 紧凑布局与底部操作栏`, async ({ page }) => {
  await page.setViewportSize({ width, height: 900 }); const errors: string[] = []; page.on('pageerror', e => errors.push(e.message)); const f = await fixture(page); await analyze(page);
  await expect(page.getByRole('img', { name: '被仿写图', exact: true })).toBeVisible(); await expect(page.getByRole('textbox', { name: '图片自定义风格' })).toHaveCount(0); await expect(page.getByRole('button', { name: '保存图片方案' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: '分析图片', exact: true })).toBeEnabled(); await page.locator('.image-workflow-actions').scrollIntoViewIfNeeded(); const box = await page.locator('.image-workflow-actions').boundingBox(); expect(box!.x).toBeGreaterThan(0); expect(box!.y + box!.height).toBeLessThanOrEqual(901);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true); expect(errors).toEqual([]); expect(f.calls.some(c => c.path.endsWith('/generate'))).toBe(false);
  await page.screenshot({ path: `../data/verification/image-flexible-edge-${width}.png` });
});

async function expectContainedImage(preview: Locator, size: [number, number], fit = 'contain') {
  const image = preview.locator('img').first();
  await image.evaluate(async img => { await (img as HTMLImageElement).decode(); });
  const bounds = await image.evaluate(img => {
    const root = img.parentElement!;
    const container = root.parentElement!;
    const box = (node: Element) => {
      const r = node.getBoundingClientRect();
      return { left: r.left, top: r.top, right: r.right, bottom: r.bottom, width: r.width, height: r.height };
    };
    return { image: box(img), root: box(root), container: box(container), fit: getComputedStyle(img).objectFit,
      natural: [(img as HTMLImageElement).naturalWidth, (img as HTMLImageElement).naturalHeight] };
  });
  expect(bounds.natural).toEqual(size); expect(bounds.fit).toBe(fit);
  for (const [inner, outer] of [[bounds.image, bounds.root], [bounds.root, bounds.container]]) {
    expect(inner.width).toBeGreaterThan(0); expect(inner.height).toBeGreaterThan(0);
    expect(inner.left).toBeGreaterThanOrEqual(outer.left - 1); expect(inner.top).toBeGreaterThanOrEqual(outer.top - 1);
    expect(inner.right).toBeLessThanOrEqual(outer.right + 1); expect(inner.bottom).toBeLessThanOrEqual(outer.bottom + 1);
  }
}

const previewShapes: { label: string; size: [number, number] }[] = [
  { label: '方图', size: [2048, 2048] }, { label: '竖图', size: [1152, 2048] }, { label: '横图', size: [2048, 1152] }, { label: '长图', size: [360, 3600] },
];
for (const width of [1536, 825, 597]) for (const shape of previewShapes) test(`Edge ${width}px ${shape.label}铺满素材卡片且可查看完整原图`, async ({ page }) => {
  await page.setViewportSize({ width, height: 900 }); const f = await fixture(page, { imageSize: shape.size });
  const preview = page.locator('.image-input-preview').first(); await expectContainedImage(preview, shape.size, 'cover');
  const card = page.locator('.image-input-grid .ant-card').first();
  const imageBox = (await preview.locator('img').boundingBox())!;
  const cardBox = (await card.boundingBox())!;
  const proposalsBox = (await page.locator('.image-proposals').boundingBox())!;
  expect(imageBox.y + imageBox.height).toBeLessThanOrEqual(cardBox.y + cardBox.height + 1);
  expect(proposalsBox.y - cardBox.y - cardBox.height).toBeGreaterThanOrEqual(28);
  expect(imageBox.width / cardBox.width).toBeGreaterThan(.95); expect(imageBox.height / cardBox.height).toBeGreaterThan(.75);
  for (const control of await card.getByRole('button', { name: /菜单$/ }).all()) {
    const controlBox = (await control.boundingBox())!;
    expect(controlBox.x + controlBox.width).toBeLessThanOrEqual(cardBox.x + cardBox.width - 10);
    expect(controlBox.y).toBeGreaterThanOrEqual(cardBox.y + 10);
    await control.click(); await expect(page.locator('.reference-slot-menu').getByRole('menuitem', { name: '更换', exact: true })).toBeVisible();
    await page.keyboard.press('Escape');
  }
  await preview.locator('img').first().click();
  const zoom = page.getByRole('dialog', { name: '被仿写图', exact: true }); await expect(zoom).toBeVisible();
  await expect(zoom.getByRole('img', { name: '被仿写图', exact: true })).toBeVisible();
  await zoom.locator('.ant-image-preview-close').click(); await expect(zoom).toHaveCount(0);
  await expectContainedImage(preview, shape.size, 'cover');
  expect(f.calls).toHaveLength(0); expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
});

test('Edge 大图辅助预览及历史结果缩略图保持边界，不触发模型', async ({ page }) => {
  const size: [number, number] = [1152, 2048]; let f = await fixture(page, { imageSize: size });
  await page.getByRole('button', { name: /^选择辅助参考图 / }).first().click();
  const picker = page.getByRole('dialog', { name: '选择辅助参考图' });
  await picker.getByRole('checkbox', { name: '选择图片 2' }).check(); await picker.getByRole('button', { name: '确认选择' }).click();
  await expect(page.locator('.image-input-preview')).toHaveCount(2);
  for (const width of [1536, 825, 597]) {
    await page.setViewportSize({ width, height: 900 });
    for (const preview of await page.locator('.image-input-preview').all()) await expectContainedImage(preview, size, 'cover');
  }
  expect(f.calls.filter(c => /\/(analyze|compose-prompt|generate)$/.test(c.path))).toHaveLength(0);
  await page.unroute('**/api/v1/**'); f = await fixture(page, { legacy: true, imageSize: size });
  await page.getByRole('button', { name: '本次生成依据' }).click();
  for (const width of [1536, 825, 597]) {
    await page.setViewportSize({ width, height: 900 });
    await expectContainedImage(page.locator('.image-result-preview'), size);
    await expectContainedImage(page.locator('.image-result-inputs > div').first(), size);
    const root = page.locator('.image-result-inputs .image-bounded-root').first();
    const box = (await root.boundingBox())!; expect(box.width).toBeLessThanOrEqual(83); expect(box.height).toBeLessThanOrEqual(91);
  }
  expect(f.calls).toHaveLength(0);
});
