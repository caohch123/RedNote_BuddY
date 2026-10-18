import type { Route } from '@playwright/test';

export function memberAuthResponse(route: Route) {
  if (new URL(route.request().url()).pathname !== '/api/v1/auth/me') return undefined;
  return route.fulfill({ json: { user: {
    id: 'member-fixture', username: 'fixture-member', display_name: '测试创作者', role: 'member', enabled: true,
    must_change_password: false, created_at: '2026-10-18T00:00:00Z', daily_request_limit: 100, max_concurrent: 2,
  } } });
}
