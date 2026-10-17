import { chromium } from 'playwright';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

// Default is read-only. --prepare explicitly analyses the selected primary and
// composes a draft prompt; image generation and adoption are always blocked.
const prepare = process.argv.includes('--prepare');
const promptFileIndex = process.argv.indexOf('--prompt-file');
const promptFile = promptFileIndex >= 0 ? process.argv[promptFileIndex + 1] : undefined;
assert(!promptFile || prepare, '--prompt-file requires --prepare');
const base = process.env.XHS_LIVE_URL || 'http://127.0.0.1:8080';
const output = path.resolve(process.env.VERIFICATION_DIR || '../data/verification/edge-image-example-20261011');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ channel: 'msedge', headless: true });
try {
  const context = await browser.newContext({ viewport: { width: 1536, height: 1000 } });
  await context.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'rewrites');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true');
  });
  const query = async endpoint => {
    const response = await context.request.get(`${base}/api/v1${endpoint}${endpoint.includes('?') ? '&' : '?'}platform=xiaohongshu`);
    assert(response.ok(), `GET ${endpoint}: ${response.status()}`); return response.json();
  };
  const { items: projects } = await query('/rewrites');
  const project = projects.find(p => (!process.env.REWRITE_ID || p.id === process.env.REWRITE_ID) && p.image_rewrites?.some(u => u.workflow_version === 2));
  assert(project, 'No V2 image scheme found');
  const scheme = process.env.IMAGE_SCHEME_ID ? project.image_rewrites.find(u => u.id === process.env.IMAGE_SCHEME_ID) : project.image_rewrites.filter(u => u.workflow_version === 2).at(-1);
  assert(scheme?.primary_asset_id, 'Missing primary image');
  const schemePath = `/rewrites/${project.id}/images/${scheme.id}`;
  const snapshot = () => query(`/rewrites/${project.id}`).then(d => d.rewrite.image_rewrites.find(u => u.id === scheme.id));
  const page = await context.newPage(); const blocked = [], writes = [], errors = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), endpoint = new URL(request.url()).pathname.replace('/api/v1', '');
    if (['GET', 'HEAD'].includes(request.method())) return route.continue();
    const allowed = prepare && (request.method() === 'PATCH' && endpoint === schemePath || request.method() === 'POST' && [schemePath + '/analyze', schemePath + '/compose-prompt'].includes(endpoint));
    if (!allowed) { blocked.push(endpoint); return route.abort(); }
    writes.push({ endpoint, method: request.method() }); return route.continue();
  });
  await page.goto(base); const workspace = page.locator('[data-workspace="xiaohongshu"]');
  await workspace.getByRole('menuitem', { name: /仿写库/ }).click();
  await workspace.locator('.rewrite-list-item').filter({ hasText: project.title }).first().click();
  await workspace.getByRole('tab', { name: '图片仿写', exact: true }).click();
  const selectedScheme = workspace.getByRole('combobox', { name: '图片方案' });
  await selectedScheme.click();
  await page.getByRole('option', { name: `方案 ${project.image_rewrites.indexOf(scheme) + 1}`, exact: true }).click();
  await workspace.getByRole('button', { name: '1. 选图与特征', exact: true }).click();
  const bounds = [];
  for (const width of [1536, 825, 597, 569]) {
    await page.setViewportSize({ width, height: 751 });
    const image = workspace.getByRole('img', { name: '被仿写图', exact: true });
    await image.scrollIntoViewIfNeeded();
    await image.evaluate(img => img.decode());
    const measurement = await image.evaluate(img => {
      const box = img.getBoundingClientRect(), holder = img.closest('.image-input-preview').getBoundingClientRect();
      return { width: innerWidth, natural: [img.naturalWidth, img.naturalHeight], image: { x: box.x, y: box.y, width: box.width, height: box.height }, holder: { x: holder.x, y: holder.y, width: holder.width, height: holder.height }, fit: getComputedStyle(img).objectFit, horizontalOverflow: document.documentElement.scrollWidth > innerWidth + 1 };
    });
    assert.equal(measurement.fit, 'contain'); assert.equal(measurement.horizontalOverflow, false);
    for (const axis of ['x', 'y']) {
      const size = axis === 'x' ? 'width' : 'height';
      assert(measurement.image[axis] >= measurement.holder[axis] - 1);
      assert(measurement.image[axis] + measurement.image[size] <= measurement.holder[axis] + measurement.holder[size] + 1, `Image overflow ${width}px`);
    }
    const dock = workspace.locator('.workspace-action-dock');
    await dock.waitFor();
    const footer = await dock.evaluate(el => {
      const box = el.getBoundingClientRect(); const content = el.closest('.ant-layout').querySelector('.main-content').getBoundingClientRect();
      return { bottom: box.bottom, top: box.top, contentBottom: content.bottom, viewportBottom: innerHeight };
    });
    assert(Math.abs(footer.bottom - footer.viewportBottom) <= 1, `Footer is not aligned at ${width}px`);
    assert(Math.abs(footer.top - footer.contentBottom) <= 1, `Footer overlays content at ${width}px`);
    assert.equal(await workspace.locator('.image-text-confirm').count(), 0);
    measurement.footer = footer;
    bounds.push(measurement); await page.screenshot({ path: path.join(output, `primary-${width}.png`) });
  }
  console.log('Edge image containment and bottom action dock passed: 1536, 825, 597, 569 px');
  await page.setViewportSize({ width: 1536, height: 1000 });
  let final = await snapshot();
  if (prepare) {
    assert(!final.results?.length && !final.batches?.length, 'Use a fresh scheme to prepare an example');
    assert(!final.references?.length, 'Single-image example must have no auxiliary inputs');
    const before = await query(`/rewrites/${project.id}`);
    async function runTask(label, button, endpoint) {
      console.log(`Starting ${label}`);
      const responsePromise = page.waitForResponse(r => new URL(r.url()).pathname.endsWith(endpoint) && r.request().method() === 'POST');
      await button.click(); const response = await responsePromise;
      assert(response.ok(), `${label}: ${response.status()} ${await response.text()}`);
      const { job_id } = await response.json(); assert(job_id);
      const deadline = Date.now() + 180_000;
      while (Date.now() < deadline) {
        const { items: jobs } = await query('/jobs'); const job = jobs.find(j => j.id === job_id);
        if (job && !['queued', 'running'].includes(job.state)) {
          assert.equal(job.state, 'succeeded', `${label}: ${job.error || job.state}`);
          console.log(`${label} succeeded (${job_id})`); return;
        }
        await new Promise(resolve => setTimeout(resolve, 1000));
      }
      throw new Error(`${label} is still pending; do not resubmit`);
    }
    if (!final.analyses?.some(a => a.asset_id === final.primary_asset_id)) {
      await runTask('primary feature analysis', workspace.getByRole('button', { name: '分析图片', exact: true }), '/analyze');
    } else console.log('Reusing saved primary analysis');
    final = await snapshot(); const analysis = final.analyses.find(a => a.asset_id === final.primary_asset_id); assert(analysis);
    // Keep visual evidence from the single main image. OCR/watermarks are not
    // automatically carried into the current prompt composition flow.
    const chosen = ['subject', 'scene', 'composition', 'palette', 'lighting', 'texture'].filter(key => analysis.features[key] && analysis.features[key] !== '未知');
    const labels = { subject: '主体与动作', scene: '场景与背景', composition: '构图与视角', palette: '色彩', lighting: '光线', texture: '风格与材质', text: '文字排版' };
    assert(chosen.includes('subject') || chosen.includes('composition'), 'No usable main-image characteristics');
    const alreadyPrepared = final.prompt_input_digest && final.prompt_rules_version >= 3 && Object.keys(labels).every(key => (final.feature_sources[key] || '') === (chosen.includes(key) ? final.primary_asset_id : ''));
    if (alreadyPrepared) {
      console.log('Reusing prepared prompt; no repeated model call');
      await workspace.getByRole('button', { name: '2. 审阅提示词', exact: true }).click();
    } else {
      for (const [key, label] of Object.entries(labels)) {
        await workspace.getByRole('combobox', { name: `${label}来源`, exact: true }).click();
        await page.locator(`[id="feature-${scheme.id}-${key}_list_${chosen.includes(key) ? 1 : 0}"]`).click();
      }
      await runTask('prompt composition', workspace.getByRole('button', { name: '生成提示词', exact: true }), '/compose-prompt');
    }
    const prompt = workspace.getByRole('textbox', { name: '生成提示词', exact: true });
    await prompt.waitFor({ timeout: 15_000 });
    if (promptFile) {
      const reviewedPrompt = (await readFile(path.resolve(promptFile), 'utf8')).trim();
      assert(reviewedPrompt && [...reviewedPrompt].length <= 4000, 'Invalid reviewed prompt');
      if (await prompt.inputValue() !== reviewedPrompt) {
        await prompt.fill(reviewedPrompt);
        const deadline = Date.now() + 15_000;
        while ((await snapshot()).prompt !== reviewedPrompt && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 400));
        assert.equal((await snapshot()).prompt, reviewedPrompt, 'Reviewed prompt not saved');
      }
    }
    final = await snapshot(); assert(final.prompt_input_digest && final.prompt);
    assert.equal((final.references || []).length, 0);
    assert(Object.values(final.feature_sources).every(source => !source || source === final.primary_asset_id));
    const after = await query(`/rewrites/${project.id}`);
    assert.deepEqual(after.rewrite.versions, before.rewrite.versions, 'Text or publishing materials changed');
    await page.screenshot({ path: path.join(output, 'review-prompt.png'), fullPage: true });
    await writeFile(path.join(output, 'prepared-example.json'), JSON.stringify({ project_id: project.id, scheme_id: final.id, main_asset_id: final.primary_asset_id, revision: final.revision, feature_sources: final.feature_sources, features: analysis.features, recognized_text: analysis.recognized_text, text_mode: final.text_mode, output: final.output, prompt: final.prompt, prompt_input_digest: final.prompt_input_digest, generated: false }, null, 2));
    console.log('Single-primary prompt prepared; generation and adoption were not called');
  }
  let refreshPreservesPrompt = false;
  if (final.prompt_input_digest && final.prompt) {
    const beforeRefreshWrites = writes.length;
    await page.reload();
    await workspace.locator('.rewrite-list-item').filter({ hasText: project.title }).first().click();
    await workspace.getByRole('tab', { name: '图片仿写', exact: true }).click();
    await selectedScheme.click();
    await page.getByRole('option', { name: `方案 ${project.image_rewrites.indexOf(scheme) + 1}`, exact: true }).click();
    await workspace.getByRole('button', { name: '2. 审阅提示词', exact: true }).click();
    const prompt = workspace.getByRole('textbox', { name: '生成提示词', exact: true });
    await prompt.waitFor(); assert.equal(await prompt.inputValue(), final.prompt);
    assert.equal(await workspace.getByRole('button', { name: `确认并生成 ${final.output.count} 张`, exact: true }).isEnabled(), (final.prompt_rules_version || 0) >= 3, 'Old prompts must be recomposed before generation');
    await page.waitForTimeout(800);
    assert.equal(writes.length, beforeRefreshWrites, 'Refresh caused unexpected automatic writes');
    refreshPreservesPrompt = true;
    await page.screenshot({ path: path.join(output, 'review-prompt-refreshed.png'), fullPage: true });
    console.log('Edge refresh preserves the reviewed prompt and its generation readiness');
  }
  assert.deepEqual(errors, []); assert.deepEqual(blocked, []);
  await writeFile(path.join(output, prepare ? 'checks.json' : 'readonly-checks.json'), JSON.stringify({ browser: 'Microsoft Edge', prepare, bounds, writes, blocked, pageErrors: errors, refreshPreservesPrompt, schemeID: final.id, mainAssetID: final.primary_asset_id, generated: false }, null, 2));
  await context.close();
} finally { await browser.close(); }
