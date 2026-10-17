# 前端

React + TypeScript + Vite + Ant Design，通过 HTTP API 连接后端。

```powershell
npm ci
npm run dev         # 开发：127.0.0.1:5173，代理到 API 8082
npm run build       # 生成 dist，不启动后端
npm start           # 生产：127.0.0.1:8080，代理到 API 8082
npm test
npm run test:server
npm run test:e2e
```

生产服务使用 Node 内置模块；已有 `dist` 时可直接 `node server.mjs`，不依赖开发包。复制 `.env.example` 为 `.env.local` 可配置开发 API 目标，或设置完整的 `VITE_API_BASE_URL`。前端不保存服务端密钥。

生产代理用 `API_PROXY_TARGET` 环境变量，或 `node server.mjs --api-target http://127.0.0.1:8082`。端口可由 `--port` 指定；默认只监听本机。

[架构与部署](../docs/frontend-backend.md) · [后端 API](../docs/api.md)
