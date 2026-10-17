# 项目长期记忆（xhs-workbench）

## 平台定位
- 项目已收敛为**仅小红书**：后端 `validWorkspacePlatform` 只认 xiaohongshu，`workspace_registry` 只管理小红书；抖音适配代码 2026-10-16 移入 `.backup-douyin/20261016/`，抖音工作区数据当天全部删除（见 2026-10-16 日志）。

## 抖音工作区删除（2026-10-16）
- 删除范围：DB 22 条记录（notes 3/rewrites 1/videos 1/publications 1/assets 6/jobs 9 + douyin_accounts）、6 个媒体文件（~195 MiB）、36 项残留（空 douyin/ 目录、data/qa 32 个证据、verification 2 张图、stop-douyin.txt）。
- 删除逻辑复刻自 `internal/app/deletions.go` 的 `plan()`（血缘 lineage + 级联 + 保留规则），脚本一次性跑完：重算计划→断言预期集合→DB 备份→媒体归档（Temp + SHA-256）→事务删除→删文件→清理残留→验证。
- 恢复点：`data/backups/before-douyin-workspace-delete-20261016-233547.db`、`%TEMP%\xhs-douyin-workspace-archive-20261016-233547\`、`bin/xhs-workbench-20.40-backup.exe`（退役前唯一旧二进制）。

## 关键运维教训
- **改 Go 源码后必须重建 bin/xhs-workbench.exe 并重启**，否则跑旧逻辑（本项目反复踩坑；2026-10-16 发现 exe 比源码旧 26 小时）。构建：`CGO_ENABLED=0 GOARCH=amd64 go build -o bin/xhs-workbench.exe ./cmd/workbench`（go1.26.2 在 D:\go\bin，主机 windows/386 可交叉编译 amd64）。
- 工作区删除若走 `DELETE /workspaces/:id`，后端会**无条件删 `xhs_accounts` 并清 settings.Cookie**——对非小红书平台是隐患；数据级删除应只删对应平台账号桶（douyin→douyin_accounts）。
- 数据库是通用 `records` 表（bucket+id+JSON），删数据前用 sqlite backup API 做单文件快照（含 WAL 内容），比单纯复制 .db 可靠。
- `data/.deletions/` 是应用删除暂存区，启动时 `recoverDeletionCleanup` 会扫描：无 manifest 的非空目录会直接返回错误**阻断服务启动**，不要往里放自定义文件。

## 环境
- 本机代理 127.0.0.1:58180：curl 必须 `--noproxy '*'`，否则 502 误判服务未运行。
- Bash 工具 PATH 偶发损坏，命令前加 `export PATH="/usr/bin:/bin:/c/Windows/System32:$PATH"`。
