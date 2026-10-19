import { chromium, expect } from '@playwright/test';
import { mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

const baseURL = process.env.XHS_SCREENSHOT_URL || 'http://127.0.0.1:8080';
const output = fileURLToPath(new URL('../../docs/images/', import.meta.url));
const today = new Date(); today.setHours(10, 0, 0, 0);
const when = today.toISOString();
const member = { id: 'demo-creator', username: 'demo-creator', display_name: '演示创作者', role: 'member', enabled: true, must_change_password: false, created_at: when, daily_request_limit: 100, max_concurrent: 2 };
const admin = { ...member, id: 'demo-admin', username: 'demo-admin', display_name: '演示管理员', role: 'admin', daily_request_limit: 0 };
const topics = ['周末出走', '生活记录', '山野漫步'];
const body = '把手机调成静音，给自己留一个没有安排的上午。\n\n沿着湖边走一走，听风穿过树梢，看阳光慢慢铺满山坡。不必走很远，也能遇见生活里被忽略的细节。\n\n带一瓶水、一双舒服的鞋，再带一点好奇心。这个周末，试着把时间还给自己。';
const notes = [
  { id: 'demo-note-1', title: '把周末交给山野', body, author: { name: '山野来信' }, topics, likes: 128, collects: 86, comments: 12, type: 'image', asset_ids: ['forest', 'forest-detail'] },
  { id: 'demo-note-2', title: '在城市里，找到一处慢下来', body: '转过街角，发现一家安静的小店。\n\n一杯咖啡、一本读到一半的书，让忙碌的日子多一点留白。', author: { name: '街角日记' }, topics: ['城市漫游', '日常灵感'], likes: 96, collects: 42, comments: 8, type: 'image', asset_ids: ['city', 'city-detail'] },
  { id: 'demo-note-3', title: '记录一个有风的下午', body: '傍晚的光线是温柔的滤镜。\n\n收集一些普通却值得记住的片刻，让每一天都有自己的颜色。', author: { name: '生活切片' }, topics: ['生活切片', '慢生活'], likes: 203, collects: 71, comments: 16, type: 'video', asset_ids: ['demo-video', 'afternoon'] },
].map((note, index) => ({ ...note, platform: 'xiaohongshu', note_id: note.id, url: `https://www.xiaohongshu.com/explore/${note.id}`, cover_asset_id: index === 2 ? 'afternoon' : note.asset_ids[0], status: 'ready', metadata_status: 'succeeded', media_status: 'succeeded', extraction_status: 'succeeded', published_at: when, collected_at: when, tags: [] }));
const assets = notes.flatMap(note => note.asset_ids.map((id, position) => ({ id, owner_id: note.id, kind: id === 'demo-video' ? 'video' : 'image', mime: id === 'demo-video' ? 'video/mp4' : 'image/svg+xml', position, width: 600, height: 800, duration: id === 'demo-video' ? 8 : undefined, status: 'ready', extraction: { text: '', status: 'succeeded', segments: [] } })));
const version = { id: 'demo-version-2', number: 2, title: notes[0].title, body, topics, asset_ids: ['forest', 'forest-detail'], script: '', created_at: when };
const rewrites = notes.map((note, index) => ({ id: `demo-rewrite-${index + 1}`, note_id: note.id, title: note.title, versions: [{ ...version, id: `demo-version-${index}-1`, number: 1, title: note.title, body: note.body }, { ...version, id: `demo-version-${index}-2`, title: note.title, body: note.body }], confirmed_version_id: `demo-version-${index}-2`, options: { audience: '热爱生活的你', tone: '自然分享', length: '精简（约 200 字）', instructions: '' }, created_at: when, updated_at: when }));
const dateKey = date => `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
const records = [2000, 3200, 1600, 4000, 2800, 3600, 4800].flatMap((total, index) => {
  const date = new Date(today); date.setDate(date.getDate() - 6 + index);
  return [0, 1].map(part => ({ id: `demo-usage-${index}-${part}`, created_at: date.toISOString(), user_id: member.id, username: member.username, capability: part ? 'vision' : 'text', operation: part ? 'image_analyze' : 'rewrite', provider: 'https://provider.example/v1', model: part ? 'qwen-vl-plus' : 'qwen-plus', credential_id: 'demo-credential', status: 'success', input_tokens: total * .4, output_tokens: total * .1, total_tokens: total / 2, cached_tokens: 0, reasoning_tokens: 0, estimated_cny: total * .0000016 }));
});
function totals(rows) {
  return { requests: rows.length, reported: rows.length, missing: 0, failed: 0, pending: 0, unknown: 0, input_tokens: rows.reduce((sum, row) => sum + row.input_tokens, 0), output_tokens: rows.reduce((sum, row) => sum + row.output_tokens, 0), total_tokens: rows.reduce((sum, row) => sum + row.total_tokens, 0), cached_tokens: 0, reasoning_tokens: 0, input_reported: rows.length, output_reported: rows.length, cached_reported: 0, reasoning_reported: 0, estimated_cny: rows.reduce((sum, row) => sum + row.estimated_cny, 0), priced: rows.length, images: 0, video_seconds: 0 };
}
const summary = totals(records);
const daily = totals(records.slice(-2));
const billing = { summary, timeline: Array.from({ length: 7 }, (_, index) => { const rows = records.slice(index * 2, index * 2 + 2); return { key: dateKey(new Date(rows[0].created_at)), ...totals(rows) }; }), models: ['qwen-plus', 'qwen-vl-plus'].map(key => ({ key, ...totals(records.filter(row => row.model === key)) })), capabilities: ['text', 'vision'].map(key => ({ key, ...totals(records.filter(row => row.capability === key)) })), items: [...records].reverse(), total: records.length, page: 1, page_size: 20, started_at: records[0].created_at, hourly: false, model_options: [{ model: 'qwen-plus', provider: records[0].provider }, { model: 'qwen-vl-plus', provider: records[0].provider }], prices: [] };

function illustration(id) {
  const city = id.startsWith('city'), afternoon = id === 'afternoon';
  const background = city ? '#E9DFD0' : afternoon ? '#EBDBCF' : '#D9E1D8';
  const heading = city ? '街角的慢时光' : afternoon ? '有风的下午' : '把周末交给山野';
  const scene = city
    ? '<path d="M0 350L140 265 140 800H0Z" fill="#B48B72"/><path d="M340 310L600 390V800H340Z" fill="#C9BCA4"/><rect x="175" y="320" width="250" height="480" rx="115" fill="#65756C"/><rect x="195" y="340" width="210" height="460" rx="97" fill="#D7CBB4"/><path d="M300 350V800M195 510H405" stroke="#65756C" stroke-width="12"/><path d="M70 800Q230 580 380 800" fill="#9D7D62"/><path d="M490 800V490M480 590Q390 490 440 440Q520 480 490 610M495 660Q580 530 565 480Q500 510 495 660" fill="#76866B" stroke="#76866B" stroke-width="9"/>'
    : afternoon
      ? '<circle cx="410" cy="340" r="116" fill="#D8A467"/><path d="M0 480Q160 335 340 480T600 450V800H0Z" fill="#B3B39B"/><path d="M0 600Q200 440 410 590T600 580V800H0Z" fill="#7A8D7B"/><path d="M85 800Q155 595 235 800" fill="#DAD3BC"/><path d="M425 800V420M425 515Q330 440 360 390Q440 420 425 550M425 580Q530 460 510 400Q430 440 425 600" stroke="#526D5C" stroke-width="10" fill="#526D5C"/>'
      : '<circle cx="430" cy="315" r="92" fill="#E9C58B"/><path d="M0 590L170 340 340 570 470 400 600 550V800H0Z" fill="#9DADA0"/><path d="M0 680L210 460 410 670 600 500V800H0Z" fill="#637D70"/><path d="M170 800Q350 630 470 800" fill="#C2D2CC"/><path d="M55 600L92 440 130 600M480 680L530 470 580 680" fill="#36574A"/>';
  return `<svg xmlns="http://www.w3.org/2000/svg" width="600" height="800" viewBox="0 0 600 800"><rect width="600" height="800" fill="${background}"/><text x="46" y="75" font-family="Georgia,serif" font-size="15" letter-spacing="4" fill="#635F51">LITTLE MOMENTS / 0${city ? 2 : afternoon ? 3 : 1}</text><text x="46" y="149" font-family="Microsoft YaHei,sans-serif" font-size="37" fill="#34493E">${heading}</text><text x="48" y="191" font-family="Microsoft YaHei,sans-serif" font-size="16" fill="#656C5D">留一点时间，给喜欢的生活。</text>${scene}</svg>`;
}

const browser = await chromium.launch({ channel: process.env.XHS_BROWSER_CHANNEL || 'msedge', headless: true });
const errors = [], mutations = [];
try {
  await mkdir(output, { recursive: true });
  for (const screen of ['collections', 'rewrites', 'billing', 'admin']) {
    const context = await browser.newContext({ viewport: { width: 1280, height: screen === 'rewrites' ? 1100 : 1000 }, deviceScaleFactor: 1, timezoneId: 'Asia/Shanghai', locale: 'zh-CN', reducedMotion: 'reduce', colorScheme: 'light', serviceWorkers: 'block' });
    await context.route('**/*', route => {
      const request = route.request();
      if (['fetch', 'xhr'].includes(request.resourceType())) return route.fulfill({ status: 403, json: { error: 'Only mocked screenshot APIs are allowed' } });
      if (new URL(request.url()).origin !== new URL(baseURL).origin) return route.abort('blockedbyclient');
      return route.continue();
    });
    const page = await context.newPage(); page.on('pageerror', error => errors.push(error.message));
    await page.addInitScript(screen => { localStorage.setItem('workbench.xiaohongshu.page', screen); localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'false'); }, screen);
    await page.route('**/api/v1/**', route => {
      const request = route.request(), path = new URL(request.url()).pathname.replace('/api/v1', '');
      const json = value => route.fulfill({ json: value });
      if (request.method() !== 'GET') { mutations.push(path); return route.fulfill({ status: 403, json: { error: 'README screenshots are read-only' } }); }
      if (path === '/auth/me' || path === '/admin/auth/me') return json({ user: screen === 'admin' ? admin : member, csrf_token: 'screenshot-demo' });
      if (path === '/session') return json({ csrf_token: 'screenshot-demo' });
      if (path === '/workspaces') return json({ items: [{ platform: 'xiaohongshu' }], total: 1 });
      if (path === '/collections') return json({ items: notes, total: notes.length });
      if (/^\/collections\/[^/]+$/.test(path)) { const note = notes.find(note => note.id === path.split('/')[2]); return json({ note, assets: assets.filter(asset => asset.owner_id === note?.id) }); }
      if (/^\/assets\/[^/]+\/file$/.test(path)) return route.fulfill({ contentType: 'image/svg+xml', body: illustration(path.split('/')[2]) });
      if (path === '/rewrites') return json({ items: rewrites, total: rewrites.length });
      if (/^\/rewrites\/[^/]+$/.test(path)) { const rewrite = rewrites.find(rewrite => rewrite.id === path.split('/')[2]); return json({ rewrite, note: notes.find(note => note.id === rewrite?.note_id), assets: assets.filter(asset => asset.owner_id === rewrite?.note_id), videos: [] }); }
      if (path === '/billing' || path === '/admin/billing') return json(billing);
      if (path === '/admin/overview') return json({ users: 2, enabled_users: 2, requests: daily.requests, tokens: daily.total_tokens, estimated_cny: daily.estimated_cny, pending_jobs: 0, failed_jobs: 0, unreported: 0, per_user: [{ user_id: member.id, username: member.username, display_name: member.display_name, requests: daily.requests, tokens: daily.total_tokens, estimated_cny: daily.estimated_cny, daily_requests: daily.requests, daily_request_limit: 100 }] });
      return json({ items: [], total: 0 });
    });
    await page.goto(`${baseURL}/${screen === 'admin' ? 'admin' : ''}`);
    if (screen === 'collections') {
      await expect(page.locator('.collection-note-card')).toHaveCount(3);
      await page.locator('.collection-library-panel').scrollIntoViewIfNeeded();
      await expect(page.locator('.collection-note-card video')).toHaveCount(1);
      await page.waitForFunction(() => [...document.querySelectorAll('.collection-card-image')].length === 2 && [...document.querySelectorAll('.collection-card-image')].every(image => image.complete && image.naturalWidth > 0));
      await page.locator('.collection-note-card video').evaluate(async video => { const image = new Image(); image.src = video.poster; await image.decode(); });
    } else if (screen === 'rewrites') {
      await expect(page.getByRole('textbox', { name: '仿写标题', exact: true })).toHaveValue(notes[0].title);
      await expect(page.getByRole('region', { name: 'AI 文案对话框', exact: true })).toBeVisible();
      await page.getByRole('textbox', { name: '补充要求', exact: true }).fill('保留自然、轻松的语气，补充适合周末出行的小建议。');
      await page.locator('body').click({ position: { x: 10, y: 10 } });
    } else if (screen === 'billing') await expect(page.locator('.billing-metric-primary strong')).toHaveText('22,000');
    else await expect(page.getByRole('heading', { name: '管理总览', exact: true })).toBeVisible();
    await page.evaluate(() => document.fonts.ready);
    await page.mouse.move(0, 0);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(1280);
    await page.screenshot({ path: `${output}/${screen === 'rewrites' ? 'rewrite-editor' : screen}.png`, animations: 'disabled', caret: 'hide' });
    await context.close();
  }
  expect(errors).toEqual([]); expect(mutations).toEqual([]);
  console.log('Captured 4 README screenshots with demo data; no API writes.');
} finally { await browser.close(); }
