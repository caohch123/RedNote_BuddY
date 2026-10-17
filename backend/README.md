# 后端 API

Go + Gin + SQLite，负责内容、素材、生成任务和发布。前端构建及 Node 前端服务不是 Go 构建依赖。

在本目录运行：

```powershell
$env:CGO_ENABLED='0'
$env:GOARCH='amd64'
go run ./cmd/workbench --root .. --addr 127.0.0.1:8082
go test -count=1 ./...
go build -o ../bin/xhs-workbench.exe ./cmd/workbench
```

`--root` 指向含 `data`、`engine` 和 `publisher` 的项目根目录。生产启动脚本始终传入绝对 root 和数据目录，避免因启动位置变化创建另一份数据库。

[API 文档](../docs/api.md) · [架构与部署](../docs/frontend-backend.md) · [运行依赖](../docs/local-dependencies.md)
