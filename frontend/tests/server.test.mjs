import { test } from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { mkdtemp, mkdir, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { createFrontendServer } from '../server.mjs';

async function listen(server) { await new Promise(resolve => server.listen(0, '127.0.0.1', resolve)); return `http://127.0.0.1:${server.address().port}`; }
async function close(server) { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }

test('frontend serves its build independently, proxies APIs and preserves upload/download semantics', async t => {
  const directory = await mkdtemp(path.join(tmpdir(), 'xhs-frontend-'));
  const dist = path.join(directory, 'dist'); await mkdir(dist);
  await writeFile(path.join(dist, 'index.html'), '<html>frontend fixture</html>');
  await writeFile(path.join(dist, 'app.js'), 'console.log("frontend")');
  await writeFile(path.join(directory, 'secret.txt'), 'must not be served');
  const received = [];
  const api = http.createServer(async (req, res) => {
    const chunks = []; for await (const chunk of req) chunks.push(chunk);
    received.push({ method: req.method, url: req.url, headers: req.headers, body: Buffer.concat(chunks) });
    if (req.url.startsWith('/api/v1/assets/')) { res.writeHead(206, { 'Content-Type': 'video/mp4', 'Content-Range': 'bytes 2-4/6', 'Content-Disposition': 'attachment; filename="video.mp4"' }); res.end('234'); return; }
    res.writeHead(req.method === 'POST' ? 201 : 200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ ok: true }));
  });
  const target = await listen(api), frontend = await createFrontendServer({ distDir: dist, apiTarget: target }), base = await listen(frontend);
  t.after(async () => {
    await close(frontend); await close(api);
    const resolved = path.resolve(directory);
    if (!resolved.startsWith(path.join(path.resolve(tmpdir()), 'xhs-frontend-'))) throw new Error('Cleanup outside temporary test directory');
    await rm(resolved, { recursive: true, force: true });
  });
  assert.match(await (await fetch(base)).text(), /frontend fixture/);
  assert.match((await fetch(base + '/app.js')).headers.get('content-type'), /javascript/);
  assert.equal((await fetch(base + '/app.js', { method: 'HEAD' })).headers.get('content-length'), '23');
  assert.equal((await fetch(base + '/route', { headers: { Accept: 'text/html' } })).status, 200);
  assert.equal((await fetch(base + '/missing.js', { headers: { Accept: 'text/html' } })).status, 404);
  assert.equal((await fetch(base + '/%2e%2e%5csecret.txt')).status, 403);
  assert.equal((await fetch(base + '/.env')).status, 404);
  assert.equal((await fetch(base + '/healthz')).status, 200);
  const body = Buffer.alloc(128 * 1024, 42);
  assert.equal((await fetch(base + '/api/v1/assets?platform=xiaohongshu', { method: 'POST', body, headers: { 'Content-Type': 'multipart/form-data; boundary=test', 'X-CSRF-Token': 'fixture-token', Origin: base } })).status, 201);
  assert.deepEqual(received[0].body, body); assert.equal(received[0].headers['x-csrf-token'], 'fixture-token'); assert.equal(received[0].headers.origin, base); assert.equal(received[0].headers.host, new URL(target).host);
  const download = await fetch(base + '/api/v1/assets/video/file', { headers: { Range: 'bytes=2-4' } });
  assert.equal(download.status, 206); assert.equal(download.headers.get('content-range'), 'bytes 2-4/6'); assert.match(download.headers.get('content-disposition'), /video.mp4/); assert.equal(await download.text(), '234'); assert.equal(received[1].headers.range, 'bytes=2-4');
  assert.equal((await fetch(base + '/api/not-found')).headers.get('content-type'), 'application/json');
  await close(api);
  const unavailable = await fetch(base + '/api/v1/session'); assert.equal(unavailable.status, 502); assert.match((await unavailable.json()).error, /后端/);
});
