import http from 'node:http';
import https from 'node:https';
import { createReadStream } from 'node:fs';
import { realpath, stat } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { parseArgs } from 'node:util';

const mime = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8', '.css': 'text/css; charset=utf-8', '.json': 'application/json', '.svg': 'image/svg+xml', '.png': 'image/png', '.jpg': 'image/jpeg', '.jpeg': 'image/jpeg', '.webp': 'image/webp', '.ico': 'image/x-icon', '.woff2': 'font/woff2', '.txt': 'text/plain; charset=utf-8' };
const hopHeaders = new Set(['connection', 'keep-alive', 'proxy-authenticate', 'proxy-authorization', 'te', 'trailer', 'transfer-encoding', 'upgrade']);
const forwardHeaders = headers => Object.fromEntries(Object.entries(headers).filter(([key]) => !hopHeaders.has(key.toLowerCase())));
const inside = (root, candidate) => { const rel = path.relative(root, candidate); return rel !== '..' && !rel.startsWith(`..${path.sep}`) && !path.isAbsolute(rel); };

export async function createFrontendServer({ distDir = fileURLToPath(new URL('./dist/', import.meta.url)), apiTarget = 'http://127.0.0.1:8082' } = {}) {
  const root = await realpath(distDir);
  await stat(path.join(root, 'index.html'));
  const target = new URL(apiTarget);
  if (!['http:', 'https:'].includes(target.protocol) || target.username || target.password || target.pathname !== '/' || target.search || target.hash) throw new Error('API target must be an http(s) origin');
  const fail = (res, code, error) => { if (!res.headersSent) { res.writeHead(code, { 'Content-Type': 'application/json; charset=utf-8' }); res.end(JSON.stringify({ error })); } else res.destroy(); };
  const server = http.createServer(async (req, res) => {
    res.setHeader('X-Content-Type-Options', 'nosniff'); res.setHeader('Referrer-Policy', 'no-referrer'); res.setHeader('X-Frame-Options', 'DENY'); res.setHeader('Cache-Control', 'no-store');
    const port = req.socket.localPort;
    if (![ `127.0.0.1:${port}`, `localhost:${port}` ].includes(req.headers.host)) { fail(res, 403, '仅允许本机工作台访问'); return; }
    let pathname;
    try { pathname = decodeURIComponent(new URL(req.url, 'http://127.0.0.1').pathname); } catch { fail(res, 400, '请求路径无效'); return; }
    if (pathname.startsWith('/api/')) {
      const transport = target.protocol === 'https:' ? https : http;
      const upstream = transport.request(target, { path: req.url, method: req.method, headers: { ...forwardHeaders(req.headers), host: target.host } }, reply => {
        res.writeHead(reply.statusCode || 502, forwardHeaders(reply.headers));
        reply.on('error', () => res.destroy()); reply.pipe(res);
      });
      upstream.on('error', () => fail(res, 502, '无法连接工作台服务，请检查后端是否已启动'));
      req.on('aborted', () => upstream.destroy()); res.on('close', () => { if (!res.writableFinished) upstream.destroy(); });
      req.pipe(upstream); return;
    }
    if (!['GET', 'HEAD'].includes(req.method)) { res.setHeader('Allow', 'GET, HEAD'); fail(res, 405, '方法不支持'); return; }
    if (pathname === '/healthz') { res.setHeader('Content-Type', 'application/json'); res.end(req.method === 'HEAD' ? '' : JSON.stringify({ service: 'frontend', status: 'ok' })); return; }
    try {
      let file = path.resolve(root, `.${pathname}`);
      if (pathname.includes('\\') || pathname.includes('\0') || !inside(root, file)) { fail(res, 403, '路径不允许'); return; }
      let info = await stat(file).catch(() => undefined);
      if (!info?.isFile()) {
        if (pathname !== '/' && (path.extname(pathname) || !req.headers.accept?.includes('text/html'))) { fail(res, 404, '文件不存在'); return; }
        file = path.join(root, 'index.html'); info = await stat(file);
      }
      file = await realpath(file);
      if (!inside(root, file)) { fail(res, 403, '路径不允许'); return; }
      res.writeHead(200, { 'Content-Type': mime[path.extname(file).toLowerCase()] || 'application/octet-stream', 'Content-Length': info.size });
      if (req.method === 'HEAD') { res.end(); return; }
      createReadStream(file).on('error', () => res.destroy()).pipe(res);
    } catch { fail(res, 404, '文件不存在'); }
  });
  server.headersTimeout = 15_000;
  server.requestTimeout = 0;
  return server;
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  try {
    const { values } = parseArgs({ options: { host: { type: 'string', default: '127.0.0.1' }, port: { type: 'string', default: '8080' }, 'api-target': { type: 'string', default: process.env.API_PROXY_TARGET || 'http://127.0.0.1:8082' } } });
    const port = Number(values.port);
    if (values.host !== '127.0.0.1' || !Number.isInteger(port) || port < 1 || port > 65535) throw new Error('Use a 127.0.0.1 host and a valid port');
    const server = await createFrontendServer({ apiTarget: values['api-target'] });
    server.on('error', error => { console.error(error.message); process.exitCode = 1; });
    server.listen(port, values.host, () => console.log(`Frontend: http://${values.host}:${port}`));
    for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => server.close(() => process.exit(0)));
  } catch (error) { console.error(error.message); process.exitCode = 1; }
}
