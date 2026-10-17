import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: { port: 5173, strictPort: true, proxy: { '/api': 'http://127.0.0.1:8080' },
    watch: { ignored: ['**/test-results*/**', '**/qa/**', '**/playwright-report/**'] } },
  build: { chunkSizeWarningLimit: 1100 },
  test: { include: ['src/**/*.test.ts'], environment: 'node' },
});
