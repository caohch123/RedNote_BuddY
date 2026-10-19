import { expect, test, type Page } from '@playwright/test';
import { mkdirSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { memberAuthResponse } from './member-auth';

const proof = path.resolve('../data/verification');
const longBody = '清晨沿着山间的小路出发，光线落在树叶和湖面上。把沿途的颜色、声音与心情记录下来，留给下一次旅程。';
const title = (index: number) => index === 0 ? '山间日记：把生活过成喜欢的模样' : index === 1 ? '沿途风景｜一段有声音的视频' : `收藏灵感 ${index + 1}${index % 3 === 0 ? '：在忙碌中找到自己的生活节奏与远方' : ''}`;
type Call = { path: string; method: string; body: any };

async function fixtures(page: Page, video?: Buffer) {
  const notes = Array.from({ length: 24 }, (_, index) => ({
    id: `note-${index}`, note_id: `source-${index}`, platform: 'xiaohongshu', url: `https://www.xiaohongshu.com/explore/source-${index}`,
    title: title(index), body: Array.from({ length: index === 0 ? 16 : 2 }, () => longBody).join('\n\n'),
    author: { id: 'author', name: '山野观察员' }, type: index === 1 ? 'video' : 'image',
    likes: index === 0 ? 1205 : index * 7, collects: index * 3, comments: index, shares: null,
    status: index === 5 ? 'collecting' : 'ready', metadata_status: index === 5 ? 'running' : 'succeeded',
    media_status: 'succeeded', extraction_status: 'succeeded', topics: ['生活灵感', '旅行记录'], tags: [index % 2 ? '旅行' : '日常'],
    collected_at: '2026-10-18T02:30:00Z', published_at: '2026-10-17T02:30:00Z',
    cover_asset_id: index === 2 ? '' : `cover-${index}`, asset_ids: index === 0 ? ['cover-0', 'second-0', 'third-0'] : index === 1 ? ['video-1', 'poster-1'] : [`cover-${index}`],
  }));
  const calls: Call[] = [];
  const asset = (id: string, position = 0) => ({ id, owner_id: id === 'video-1' ? 'note-1' : 'note-0',
    kind: id.startsWith('video') ? 'video' : 'image', mime: id.startsWith('video') ? 'video/mp4' : 'image/svg+xml', status: 'ready', position,
    width: 600, height: 800, duration: id.startsWith('video') ? 2 : 0, size: 1024, created_at: '2026-10-18T02:30:00Z',
    extraction: { text: '已识别的文字', segments: [], status: 'succeeded', edited: false },
  });
  await page.route('**/api/v1/**', async route => {
    const auth = memberAuthResponse(route); if (auth) return auth;
    const request = route.request(); const url = new URL(request.url()); const pathname = url.pathname.replace('/api/v1', '');
    const method = request.method(); const body = request.postData() && request.headers()['content-type']?.includes('json') ? request.postDataJSON() : undefined;
    calls.push({ path: pathname, method, body });
    const json = (value: unknown, status = 200) => route.fulfill({ status, json: value });
    if (pathname === '/session') return json({ csrf_token: 'feed-fixture' });
    if (pathname === '/workspaces') return json({ items: [{ id: 'xiaohongshu', platform: 'xiaohongshu' }], total: 1 });
    if (pathname === '/collections' && method === 'DELETE' || pathname.startsWith('/collections/') && method === 'DELETE') {
      const ids = pathname === '/collections' ? body.ids : [pathname.split('/')[2]];
      for (let i = notes.length - 1; i >= 0; i--) if (ids.includes(notes[i].id)) notes.splice(i, 1);
      return json({ deleted: { notes: ids.length }, files_deleted: 0 });
    }
    if (pathname === '/collections') {
      const query = url.searchParams.get('q') || ''; const status = url.searchParams.get('status') || ''; const tag = url.searchParams.get('tag') || '';
      const filtered = notes.filter(note => (!query || note.title.includes(query)) && (!status || note.status === status) && (!tag || note.tags.includes(tag)));
      const start = (Number(url.searchParams.get('page') || '1') - 1) * Number(url.searchParams.get('page_size') || '20');
      return json({ items: filtered.slice(start, start + Number(url.searchParams.get('page_size') || '20')), total: filtered.length });
    }
    if (/^\/collections\/[^/]+$/.test(pathname)) {
      const note = notes.find(note => note.id === pathname.split('/')[2]);
      return json({ note, assets: note?.asset_ids.map(asset) || [] });
    }
    if (/^\/assets\/[^/]+\/file$/.test(pathname)) {
      const id = pathname.split('/')[2];
      if (id === 'cover-3') return route.fulfill({ status: 404, body: 'missing image' });
      if (id.startsWith('video')) return route.fulfill({ contentType: 'video/mp4', body: video || readFileSync(new URL('./fixtures/collection-preview.mp4', import.meta.url)) });
      const index = Number(id.replace(/\D/g, '')) || 0;
      const width = index % 3 === 1 ? 900 : 600; const height = index % 3 === 1 ? 560 : index % 3 === 2 ? 600 : 1000;
      const color = ['#c4dbcd', '#d5c4a7', '#bdcadf', '#e1b6ae'][index % 4];
      return route.fulfill({ contentType: 'image/svg+xml', body: `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}"><rect width="100%" height="100%" fill="${color}"/><circle cx="${width * .72}" cy="${height * .28}" r="${width * .2}" fill="#ffffff" opacity=".6"/><path d="M0 ${height} L${width * .4} ${height * .35} L${width} ${height}" fill="#486954" opacity=".5"/><text x="40" y="80" fill="#264336" font-size="32">Collected moment ${id}</text></svg>` });
    }
    if (pathname === '/rewrites' && method === 'POST') return json({ id: `rewrite-${body.note_id}`, note_id: body.note_id, title: '新仿写', versions: [], options: {} });
    if (pathname === '/rewrites') return json({ items: [], total: 0 });
    if (pathname.startsWith('/rewrites/')) return json({ rewrite: { id: pathname.split('/')[2], title: '新仿写', versions: [], options: {} }, note: notes[0], assets: [], videos: [] });
    return json({ items: [], total: 0 });
  });
  return { calls, notes };
}

const cards = (page: Page) => page.locator('.collection-note-card');
async function open(page: Page) { await page.goto('/'); await expect(cards(page)).toHaveCount(20); }
async function openDetail(page: Page, index = 0) {
  await page.getByRole('button', { name: `更多操作：${title(index)}`, exact: true }).click();
  await page.getByRole('menuitem', { name: '查看详情', exact: false }).click();
  await expect(page.locator('.collection-detail')).toBeVisible();
}
async function noOverflow(page: Page) {
  const width = page.viewportSize()!.width;
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
  expect(await page.locator('.main-content').evaluate(node => node.scrollWidth - node.clientWidth)).toBeLessThanOrEqual(1);
  for (const box of await cards(page).evaluateAll(elements => elements.map(element => {
    const b = element.getBoundingClientRect(); return { left: b.left, right: b.right };
  }))) { expect(box.left).toBeGreaterThanOrEqual(0); expect(box.right).toBeLessThanOrEqual(width + 1); }
}
async function confirmDelete(page: Page) {
  await page.getByRole('dialog').getByRole('button', { name: '继续删除' }).click();
  await page.getByRole('checkbox', { name: '我已了解删除范围，确认永久删除' }).check();
  await page.getByRole('dialog').getByRole('button', { name: '确认删除' }).click();
}

test('默认瀑布流为真实内容手机预览，缺失封面可读且卡片互不重叠', async ({ page }) => {
  const errors: string[] = []; page.on('pageerror', error => errors.push(error.message));
  await fixtures(page); await open(page); await expect(page.locator('.collection-feed')).toBeVisible();
  await expect(page.locator('.collection-table-panel .ant-table')).toHaveCount(0);
  await expect(page.locator('.collection-note-card .collection-device')).toHaveCount(20);
  const firstPhone = page.locator('.collection-note-card[data-note-id="note-0"] .collection-device');
  await expect(firstPhone).toContainText('山野观察员');
  await expect(firstPhone.locator('.collection-device-body')).toContainText(longBody);
  await expect(firstPhone).toContainText('#生活灵感');
  await expect(firstPhone).toContainText('1,205');
  await expect(firstPhone.getByRole('button', { name: /关注|点赞|收藏|评论/ })).toHaveCount(0);
  const shell = await firstPhone.evaluate(node => {
    const style = getComputedStyle(node); return { border: parseFloat(style.borderTopWidth), radius: parseFloat(style.borderTopLeftRadius) };
  });
  expect(shell.border).toBeGreaterThanOrEqual(3); expect(shell.radius).toBeGreaterThanOrEqual(20);
  const boxes = await cards(page).evaluateAll(elements => elements.map(element => { const b = element.getBoundingClientRect(); return { x: b.x, y: b.y, right: b.right, bottom: b.bottom }; }));
  expect(new Set(boxes.map(box => Math.round(box.x))).size).toBeGreaterThan(1);
  for (let i = 0; i < boxes.length; i++) for (let j = i + 1; j < boxes.length; j++) {
    const a = boxes[i], b = boxes[j]; expect(a.right <= b.x + 1 || b.right <= a.x + 1 || a.bottom <= b.y + 1 || b.bottom <= a.y + 1).toBe(true);
  }
  await expect(page.locator('.collection-note-card[data-note-id="note-2"]')).toContainText(title(2));
  await expect(page.locator('.collection-note-card[data-note-id="note-5"]').getByRole('button', { name: '加入仿写库' })).toBeDisabled();
  await noOverflow(page); mkdirSync(proof, { recursive: true }); await page.screenshot({ path: path.join(proof, 'collection-feed-desktop.png') });
  expect(errors).toEqual([]);
});

test('瀑布流在平板和手机保持留白，不产生页面横向溢出', async ({ page }) => {
  await fixtures(page); await open(page);
  for (const width of [644, 360]) {
    await page.setViewportSize({ width, height: 900 }); await noOverflow(page);
    await page.locator('.collection-feed').scrollIntoViewIfNeeded();
    await page.screenshot({ path: path.join(proof, `collection-feed-${width}.png`) });
  }
});

test('卡片封面与标题原地阅读，箭头、圆点和滑动切换真实图片', async ({ page }) => {
  const fixture = await fixtures(page); await open(page); const originalURL = page.url();
  const card = page.locator('.collection-note-card[data-note-id="note-0"]');
  const visual = card.locator('.collection-device-visual');
  const image = visual.locator('img');
  await expect(image).toHaveAttribute('src', /\/assets\/cover-0\/file/);
  await image.click(); await card.locator('h3').click();
  await expect(page.getByRole('dialog')).toHaveCount(0); expect(page.url()).toBe(originalURL);
  await card.locator('.collection-device-scroll').evaluate(node => { node.scrollTop = 0; });
  await card.getByRole('button', { name: '下一张素材', exact: true }).click();
  await expect(image).toHaveAttribute('src', /\/assets\/second-0\/file/); await expect(card.getByLabel('素材页码')).toHaveText('2 / 3');
  await card.getByRole('button', { name: '查看素材 3', exact: true }).click();
  await expect(image).toHaveAttribute('src', /\/assets\/third-0\/file/); await expect(card.getByLabel('素材页码')).toHaveText('3 / 3');
  await card.getByRole('button', { name: '上一张素材', exact: true }).click();
  await expect(image).toHaveAttribute('src', /\/assets\/second-0\/file/);
  await visual.evaluate(node => {
    const start = new Touch({ identifier: 1, target: node, clientX: 220, clientY: 80 });
    node.dispatchEvent(new TouchEvent('touchstart', { touches: [start], changedTouches: [start], bubbles: true }));
    const end = new Touch({ identifier: 1, target: node, clientX: 80, clientY: 80 });
    node.dispatchEvent(new TouchEvent('touchend', { touches: [], changedTouches: [end], bubbles: true }));
  });
  await expect(image).toHaveAttribute('src', /\/assets\/third-0\/file/);
  await expect(page.getByRole('dialog')).toHaveCount(0); expect(page.url()).toBe(originalURL);
  expect(fixture.calls.filter(call => call.method !== 'GET')).toHaveLength(0);
});

test('未进入可视区的手机卡片不拉取详情与媒体，滚动到该卡片后按需加载', async ({ page }) => {
  const fixture = await fixtures(page); await open(page);
  const farCard = page.locator('.collection-note-card[data-note-id="note-19"]');
  const detailReads = () => fixture.calls.filter(call => call.path === '/collections/note-19' && call.method === 'GET');
  const mediaReads = () => fixture.calls.filter(call => call.path === '/assets/cover-19/file');
  expect(detailReads()).toHaveLength(0); expect(mediaReads()).toHaveLength(0);
  await farCard.scrollIntoViewIfNeeded();
  await expect.poll(() => detailReads().length).toBeGreaterThan(0);
  await expect.poll(() => mediaReads().length).toBeGreaterThan(0);
  expect(fixture.calls.filter(call => call.method !== 'GET')).toHaveLength(0);
  await expect(page.getByRole('dialog')).toHaveCount(0);
});

test('手机卡片视频原地播放，翻页后暂停离开的真实视频', async ({ page }) => {
  const fixture = await fixtures(page); await open(page); const originalURL = page.url();
  const card = page.locator('.collection-note-card[data-note-id="note-1"]');
  await card.scrollIntoViewIfNeeded();
  const video = card.locator('video'); await expect(video).toBeVisible();
  await video.evaluate((node: HTMLVideoElement) => { node.muted = true; node.loop = true; });
  expect(fixture.calls.filter(call => call.path === '/assets/video-1/file')).toHaveLength(0);
  await card.getByRole('button', { name: '播放笔记视频 1', exact: true }).click();
  await expect.poll(() => video.evaluate((node: HTMLVideoElement) => node.paused)).toBe(false);
  await video.evaluate(node => { (window as any).__inlineVideo = node; });
  await card.getByRole('button', { name: '下一张素材', exact: true }).click();
  await expect(card.getByLabel('素材页码')).toHaveText('2 / 2');
  await expect(card.locator('.collection-device-visual img')).toHaveAttribute('src', /\/assets\/poster-1\/file/);
  await expect.poll(() => page.evaluate(() => ((window as any).__inlineVideo as HTMLVideoElement).paused)).toBe(true);
  await expect(page.getByRole('dialog')).toHaveCount(0); expect(page.url()).toBe(originalURL);
});

test('原地阅读时轮询更新正文和数据，保留正在查看的图片页码', async ({ page }) => {
  const fixture = await fixtures(page); await open(page);
  const card = page.locator('.collection-note-card[data-note-id="note-0"]');
  await card.getByRole('button', { name: '下一张素材', exact: true }).click();
  await expect(card.getByLabel('素材页码')).toHaveText('2 / 3');
  const updatedBody = '原地阅读的新正文与已采集内容保持同步，不需要打开其他页面。';
  fixture.notes[0].body = updatedBody; fixture.notes[0].likes = 7654;
  await expect(card.locator('.collection-note-body')).toHaveText(updatedBody);
  await expect(card).toContainText('7,654'); await expect(card.getByLabel('素材页码')).toHaveText('2 / 3');
  await expect(card.locator('.collection-device-visual img')).toHaveAttribute('src', /\/assets\/second-0\/file/);
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(fixture.calls.filter(call => call.method !== 'GET')).toHaveLength(0);
});

test('卡片选择不会误开详情，切换表格与瀑布流后保持选择', async ({ page }) => {
  await fixtures(page); await open(page);
  await page.getByRole('checkbox', { name: `选择笔记：${title(0)}`, exact: true }).check();
  await expect(page.getByRole('dialog')).toHaveCount(0); await expect(page.getByText('已选择 1 篇')).toBeVisible();
  await page.getByRole('button', { name: '表格视图', exact: true }).click();
  await expect(page.locator('tr.ant-table-row').first().getByRole('checkbox')).toBeChecked();
  await page.getByRole('button', { name: '瀑布流视图', exact: true }).click();
  await expect(page.getByRole('checkbox', { name: `选择笔记：${title(0)}`, exact: true })).toBeChecked();
  await page.getByRole('button', { name: '取消选择', exact: true }).click();
  await expect(page.getByRole('checkbox', { name: `选择笔记：${title(0)}`, exact: true })).not.toBeChecked();
});

test('批量加入使用选中记录 ID，采集中记录阻止批量加入', async ({ page }) => {
  const fixture = await fixtures(page); await open(page);
  await page.getByRole('checkbox', { name: `选择笔记：${title(5)}`, exact: true }).check();
  await expect(page.getByRole('button', { name: '批量加入仿写库', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: '取消选择', exact: true }).click();
  for (const index of [0, 1]) await page.getByRole('checkbox', { name: `选择笔记：${title(index)}`, exact: true }).check();
  await page.getByRole('button', { name: '批量加入仿写库', exact: true }).click();
  await expect.poll(() => fixture.calls.filter(call => call.path === '/rewrites' && call.method === 'POST').map(call => call.body.note_id)).toEqual(['note-0', 'note-1']);
});

test('选择本页仅选当前页，批量删除经过两次确认并刷新结果', async ({ page }) => {
  const fixture = await fixtures(page); await open(page);
  await page.getByRole('checkbox', { name: '选择本页', exact: true }).check();
  await expect(page.getByText('已选择 20 篇')).toBeVisible();
  await page.getByRole('button', { name: '批量删除', exact: false }).click();
  expect(fixture.calls.filter(call => call.method === 'DELETE')).toHaveLength(0);
  await confirmDelete(page);
  await expect.poll(() => fixture.calls.find(call => call.method === 'DELETE')?.body.ids.length).toBe(20);
  await expect(cards(page)).toHaveCount(4); expect(fixture.notes).toHaveLength(4);
});

test('分页和搜索使用服务端条件，搜索后回到第一页并支持空结果', async ({ page }) => {
  const fixture = await fixtures(page); await open(page);
  await page.locator('.collection-feed-pagination .ant-pagination-item-2').click();
  await expect(cards(page)).toHaveCount(4); await expect(page.locator('.collection-note-card[data-note-id="note-20"]')).toBeVisible();
  const search = page.getByPlaceholder('搜索标题、正文或博主');
  await search.fill('山间日记'); await search.press('Enter'); await expect(cards(page)).toHaveCount(1);
  await expect(page.locator('.collection-note-card[data-note-id="note-0"]')).toBeVisible();
  await search.fill('完全不存在的笔记'); await search.press('Enter'); await expect(cards(page)).toHaveCount(0);
  expect(fixture.calls.filter(call => call.method !== 'GET')).toHaveLength(0);
});

test('详情为居中单手机，媒体与正文在壳内滚动，互动保持只读', async ({ page }) => {
  const fixture = await fixtures(page); await open(page);
  await openDetail(page);
  const detail = page.locator('.collection-detail');
  await expect(detail.locator('.collection-device')).toHaveCount(1);
  await expect(detail.locator('.collection-reading-pane')).toHaveCount(0);
  const phone = detail.locator('.collection-device');
  const reader = detail.locator('.collection-reader');
  await expect.poll(() => reader.evaluate(node => {
    const phoneBox = node.querySelector('.collection-device')!.getBoundingClientRect(); const readerBox = node.getBoundingClientRect();
    return Math.abs(phoneBox.x + phoneBox.width / 2 - readerBox.x - readerBox.width / 2);
  })).toBeLessThanOrEqual(2);
  await expect(detail.getByLabel('素材页码')).toContainText('1 / 3');
  await detail.getByRole('button', { name: '下一张素材', exact: true }).click(); await expect(detail.getByLabel('素材页码')).toContainText('2 / 3');
  await detail.getByRole('button', { name: '上一张素材', exact: true }).click(); await expect(detail.getByLabel('素材页码')).toContainText('1 / 3');
  await expect(phone).toContainText('1,205');
  await expect(detail.getByRole('button', { name: /关注|点赞|收藏|评论/ })).toHaveCount(0);
  await expect(detail).toContainText(longBody);
  const scroll = phone.locator('.collection-device-scroll');
  await expect.poll(() => scroll.evaluate(node => node.scrollHeight - node.clientHeight)).toBeGreaterThan(100);
  const outsideScroll = await page.locator('.collection-detail-drawer .ant-drawer-body').evaluate(node => node.scrollTop);
  await scroll.evaluate(node => { node.scrollTop = node.scrollHeight; });
  await expect.poll(() => scroll.evaluate(node => node.scrollTop)).toBeGreaterThan(100);
  expect(await page.locator('.collection-detail-drawer .ant-drawer-body').evaluate(node => node.scrollTop)).toBe(outsideScroll);
  await page.setViewportSize({ width: 360, height: 900 });
  expect(await detail.evaluate(node => node.scrollWidth)).toBeLessThanOrEqual(360);
  const mobilePhone = (await phone.boundingBox())!;
  expect(mobilePhone.x).toBeGreaterThanOrEqual(0); expect(mobilePhone.x + mobilePhone.width).toBeLessThanOrEqual(360);
  await page.screenshot({ path: path.join(proof, 'collection-feed-detail-mobile.png') });
  expect(fixture.calls.filter(call => call.method !== 'GET')).toHaveLength(0);
});

test('采集内容变化后，卡片和打开的手机预览随轮询同步真实正文与数据', async ({ page }) => {
  const fixture = await fixtures(page); await open(page);
  const firstCard = page.locator('.collection-note-card[data-note-id="note-0"]');
  await expect(firstCard.locator('.collection-device-body')).toContainText(longBody);
  await openDetail(page);
  const detailPhone = page.locator('.collection-detail .collection-device');
  await expect(detailPhone.locator('.collection-note-body')).toContainText(longBody);
  const readsBefore = fixture.calls.filter(call => call.path === '/collections/note-0' && call.method === 'GET').length;
  const updatedBody = '这是采集结果更新后的完整正文，手机预览应当与当前保存的数据保持一致。';
  fixture.notes[0].body = updatedBody; fixture.notes[0].likes = 4321;
  await expect(detailPhone.locator('.collection-note-body')).toHaveText(updatedBody);
  expect(fixture.calls.filter(call => call.path === '/collections/note-0' && call.method === 'GET').length).toBeGreaterThan(readsBefore);
  await expect(detailPhone).toContainText('4,321');
  await page.getByRole('dialog').getByRole('button', { name: /关闭|Close/ }).click();
  await expect(firstCard.locator('.collection-note-body')).toHaveText(updatedBody);
  await expect(firstCard).toContainText('4,321');
  expect(fixture.calls.filter(call => call.method !== 'GET')).toHaveLength(0);
});

test('菜单和表格显式查看详情，删除取消后保留记录', async ({ page }) => {
  const fixture = await fixtures(page); await open(page);
  await page.getByRole('button', { name: `更多操作：${title(0)}`, exact: true }).click();
  await page.getByRole('menuitem', { name: '查看详情', exact: false }).click();
  await expect(page.locator('.collection-detail')).toBeVisible();
  await page.getByRole('dialog').getByRole('button', { name: /关闭|Close/, exact: false }).click();
  await expect(page.getByRole('checkbox', { name: `选择笔记：${title(0)}`, exact: true })).not.toBeChecked();
  await page.getByRole('button', { name: `更多操作：${title(0)}`, exact: true }).click();
  await page.getByRole('menuitem', { name: '删除采集记录', exact: false }).click();
  await page.getByRole('dialog').getByRole('button', { name: /取\s*消/ }).click();
  expect(fixture.calls.filter(call => call.method === 'DELETE')).toHaveLength(0); expect(fixture.notes).toHaveLength(24);
  await page.getByRole('button', { name: '表格视图', exact: true }).click();
  await page.locator('tr.ant-table-row').first().getByRole('button', { name: '查看详情', exact: false }).click();
  await expect(page.locator('.collection-detail')).toBeVisible();
});

test('视频详情不会自动播放，关闭后暂停并释放播放状态', async ({ page }) => {
  const videoFile = readFileSync(new URL('./fixtures/collection-preview.mp4', import.meta.url));
  const fixture = await fixtures(page, videoFile); await open(page);
  await openDetail(page, 1);
  const video = page.locator('.collection-detail .collection-detail-media video'); await expect(video).toBeVisible();
  await video.evaluate((node: HTMLVideoElement) => { node.loop = true; node.muted = true; });
  expect(fixture.calls.filter(call => call.path === '/assets/video-1/file')).toHaveLength(0);
  await page.locator('.collection-detail').getByRole('button', { name: '播放笔记视频 1', exact: true }).click();
  await expect.poll(() => video.evaluate((node: HTMLVideoElement) => node.paused)).toBe(false);
  await video.evaluate((node: HTMLVideoElement) => { (window as any).__closedVideo = node; });
  await page.getByRole('dialog').getByRole('button', { name: /关闭|Close/ }).click();
  await expect.poll(() => page.evaluate(() => ((window as any).__closedVideo as HTMLVideoElement).paused)).toBe(true);
});
