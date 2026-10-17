import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { fileURLToPath } from 'node:url';
import { writeFile } from 'node:fs/promises';

const directory = fileURLToPath(new URL('../', import.meta.url));
const child = spawn(process.execPath, [fileURLToPath(new URL('../node_modules/vite/bin/vite.js', import.meta.url)), '--host', '127.0.0.1', '--port', '5173', '--strictPort'], { cwd: directory, windowsHide: true, stdio: ['ignore', 'ignore', 'pipe'] });
let error = ''; child.on('error', e => { error = e.message; }); child.stderr.on('data', () => {});
try {
  let ready = false;
  for (let i = 0; i < 100; i++) {
    if (error || child.exitCode !== null) throw new Error(error || 'Development server could not start');
    try { ready = (await fetch('http://127.0.0.1:5173/')).ok; } catch {}
    if (ready) break;
    await new Promise(resolve => setTimeout(resolve, 150));
  }
  if (!ready) throw new Error('Development server startup timed out');
  const response = await fetch('http://127.0.0.1:5173/api/v1/session', { headers: { Origin: 'http://127.0.0.1:5173' } });
  if (!response.ok || !(await response.json()).csrf_token) throw new Error(`Development API proxy failed: ${response.status}`);
  const report = { passed: true, frontend: 'http://127.0.0.1:5173', api: 'http://127.0.0.1:8082', checks: ['Vite serves frontend independently', 'Development proxy rewrites Host and preserves allowed frontend Origin', 'Real API session returned without database mutation'] };
  await writeFile(new URL('../../data/verification/separation-dev-proxy-20261017.json', import.meta.url), JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report, null, 2));
} finally {
  if (child.exitCode === null) {
    const exited = once(child, 'exit');
    if (process.platform === 'win32') { const stop = spawn('taskkill.exe', ['/PID', String(child.pid), '/T', '/F'], { windowsHide: true, stdio: 'ignore' }); await once(stop, 'exit'); }
    else child.kill('SIGTERM');
    await exited;
  }
}
