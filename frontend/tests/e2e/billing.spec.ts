import { memberAuthResponse } from './member-auth';
import { expect, test, type Page } from '@playwright/test';
import path from 'node:path';

function fixtureData(empty = false) {
  const when = new Date(); when.setHours(10, 0, 0, 0);
  const totals = { requests: 3, reported: 2, missing: 1, failed: 0, pending: 0, unknown: 0, input_tokens: 9800, input_reported: 2, output_tokens: 2200, output_reported: 2, total_tokens: 12000, cached_tokens: 1000, cached_reported: 1, reasoning_tokens: 600, reasoning_reported: 1, estimated_cny: .032, priced: 1, images: 0, video_seconds: 5 };
  const zero = Object.fromEntries(Object.keys(totals).map(key => [key, 0]));
  const record = { id: 'usage-text', created_at: when.toISOString(), capability: 'text', operation: 'rewrite', provider: 'https://provider.example', model: 'qwen-plus', credential_id: 'a1b2c3d4', job_id: 'job-1', status: 'success', input_tokens: 8000, output_tokens: 2000, total_tokens: 10000, cached_tokens: 1000, reasoning_tokens: 600, estimated_cny: .032, price: { provider: 'https://provider.example', model: 'qwen-plus', input_per_million: 2, output_per_million: 8 } };
  return {
    summary: empty ? zero : totals,
    timeline: empty ? [] : [{ key: `${when.getFullYear()}-${String(when.getMonth() + 1).padStart(2, '0')}-${String(when.getDate()).padStart(2, '0')}`, ...totals }],
    models: empty ? [] : [{ key: 'qwen-plus', ...totals, total_tokens: 10000, requests: 1, reported: 1, missing: 0 }, { key: 'qwen-vl', ...totals, total_tokens: 2000, requests: 1, reported: 1, missing: 0 }, { key: 'wan3.0-video', ...zero, requests: 1, missing: 1 }],
    capabilities: empty ? [] : [{ key: 'text', ...totals }],
    items: empty ? [] : [record, { ...record, id: 'usage-vision', model: 'qwen-vl', operation: 'image_analyze', capability: 'vision', input_tokens: 1800, output_tokens: 200, total_tokens: 2000, estimated_cny: null, price: undefined }, { ...record, id: 'usage-video', capability: 'video', model: 'wan3.0-video', operation: 'video_rewrite', input_tokens: null, output_tokens: null, total_tokens: null, cached_tokens: null, reasoning_tokens: null, estimated_cny: null, video_seconds: 5, price: undefined }],
    total: empty ? 0 : 3, page: 1, page_size: 20, started_at: when.toISOString(), prices: [record.price], hourly: false,
    model_options: [{ model: 'qwen-plus', provider: 'https://provider.example' }, { model: 'qwen-vl', provider: 'https://provider.example' }],
  };
}

async function fixture(page: Page, empty = false, error = false) {
  const data = fixtureData(empty); const queries: URLSearchParams[] = []; const writes: unknown[] = [];
  let fail = error;
  await page.addInitScript(() => {
    localStorage.setItem('workbench.xiaohongshu.page', 'billing');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true');
  });
  await page.route('**/api/v1/**', async route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const req = route.request(); const url = new URL(req.url()); const endpoint = url.pathname.replace('/api/v1', '');
    const json = (body: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
    if (endpoint === '/workspaces') return json({ items: [{ id: 'xiaohongshu', platform: 'xiaohongshu' }], total: 1 });
    if (endpoint === '/session') return json({ csrf_token: 'fixture-token' });
    if (endpoint === '/billing/prices') {
      expect(req.headers()['x-csrf-token']).toBe('fixture-token');
      const body = req.postDataJSON(); writes.push(body); data.prices = body.items; return json(body);
    }
    if (endpoint === '/billing/export') return route.fulfill({ contentType: 'text/csv; charset=utf-8', headers: { 'Content-Disposition': 'attachment; filename="token-usage.csv"' }, body: '\ufeff时间,输入Token,输出Token\nfixture,8000,2000\n' });
    if (endpoint === '/billing') {
      queries.push(url.searchParams);
      if (fail) return json({ error: '读取用量记录失败' }, 500);
      return json(data);
    }
    return json({ items: [], total: 0 });
  });
  await page.goto('/');
  return { data, queries, writes, recover: () => { fail = false; } };
}

test('计费入口位于设置上方，图表与调用明细展示真实用量口径', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 980 }); await fixture(page);
  const billing = page.getByRole('menuitem', { name: '计费面板', exact: true });
  const settings = page.getByRole('menuitem', { name: '设置', exact: true });
  await expect(billing).toBeVisible(); await expect(settings).toBeVisible();
  expect((await billing.boundingBox())!.y).toBeLessThan((await settings.boundingBox())!.y);
  await expect(page.locator('.billing-metric-primary strong')).toHaveText('12,000');
  await expect(page.locator('.billing-metric').nth(1)).toContainText('9,800');
  await expect(page.locator('.billing-metric').nth(2)).toContainText('2,200');
  await expect(page.locator('.billing-metric').nth(3)).toContainText('¥0.032');
  await expect(page.getByRole('img', { name: /Token 用量趋势图/ })).toBeVisible();
  const bar = page.locator('.billing-trend g[role="button"]').last(); await bar.focus();
  await expect(page.locator('.billing-chart-detail')).toContainText('9,800');
  await page.getByRole('radio', { name: '功能', exact: true }).locator('..').click();
  await expect(page.locator('.billing-distribution-list')).toContainText('文案与提示词');
  const videoRow = page.locator('tr[data-row-key="usage-video"]');
  await expect(videoRow).toContainText('—'); await videoRow.locator('.ant-table-row-expand-icon').click();
  await expect(page.getByText('未返回 Token，按媒体计量', { exact: true })).toBeVisible();
  await expect(page.locator('.ant-table-expanded-row').getByText('5 秒', { exact: true })).toBeVisible();
  await expect(page.locator('.billing-footnote')).toContainText('此前用量不补算');
  await page.getByRole('radio', { name: '模型', exact: true }).locator('..').click();
  await page.locator('.main-content').evaluate(el => { el.scrollTop = 0; });
  await page.screenshot({ path: path.resolve('../data/verification/billing-20261018-desktop.png'), fullPage: false });
});

test('用户端支持筛选与导出，单价由管理端维护', async ({ page }) => {
  const { queries, writes } = await fixture(page);
  await expect(page.locator('.billing-metric-primary strong')).toHaveText('12,000');
  await page.getByRole('radio', { name: '近 30 天', exact: true }).locator('..').click();
  await expect.poll(() => queries.at(-1)?.get('from')).not.toBeNull();
  await page.getByRole('combobox', { name: '筛选模型', exact: true }).click(); await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content').filter({ hasText: /^qwen-vl$/ }).click();
  await expect.poll(() => queries.at(-1)?.get('model')).toBe('qwen-vl');
  await page.getByRole('combobox', { name: '筛选调用类型' }).click(); await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content').filter({ hasText: /^图片与视频分析$/ }).click();
  await expect.poll(() => queries.at(-1)?.get('capability')).toBe('vision');
  await expect(page.getByRole('button', { name: '单价设置', exact: true })).toHaveCount(0);
  expect(writes).toEqual([]);
  const pending = page.waitForEvent('download'); await page.getByRole('button', { name: '导出明细', exact: true }).click();
  expect((await pending).suggestedFilename()).toBe('token-usage.csv');
});

test('空状态不伪造费用，窄屏保持可用且页面不横向溢出', async ({ page }) => {
  await page.setViewportSize({ width: 644, height: 751 }); await fixture(page, true);
  await expect(page.getByText('暂无调用记录，下一次创作后将在这里显示')).toBeVisible();
  await expect(page.locator('.billing-metric-primary strong')).toHaveText('0');
  await expect(page.locator('.billing-metric').nth(3)).toContainText('未估算');
  await expect(page.getByRole('button', { name: '导出明细', exact: true })).toBeDisabled();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.reload(); await expect(page.getByRole('heading', { name: '计费面板', exact: true })).toBeVisible();
  await page.screenshot({ path: path.resolve('../data/verification/billing-20261018-mobile.png'), fullPage: false });
});

test('用量接口失败时允许重试，不展示假成功或假零值', async ({ page }) => {
  const { recover } = await fixture(page, false, true);
  await expect(page.getByText('读取用量记录失败', { exact: true })).toBeVisible();
  await expect(page.locator('.billing-metric')).toHaveCount(0);
  recover();
  await page.getByRole('button', { name: /重试/ }).click();
  await expect(page.locator('.billing-metric-primary strong')).toHaveText('12,000');
});
