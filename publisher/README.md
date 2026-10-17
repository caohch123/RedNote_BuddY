# 小红书扫码发布适配器

此组件复用固定的 Spider_XHS 5.0.0 本地源码，提供扫码登录、账号验证、
图文及视频发布。它是非官方 Creator API 适配器，接口及签名可能随平台改变。
来源与文件校验见 `NOTICE.md`、`upstream.lock.json`；上游缺失许可证的情况
已如实记录，未伪造许可证。

安装 Python 3.12+、Node 22+，安装 `requirements.txt` 到本目录 `.venv`，
在 `vendor/Spider_XHS` 执行 `npm ci --no-audit --no-fund`。
Go 启动 `.venv/Scripts/python.exe -u bridge.py`，Unix 使用 `.venv/bin/python`。
组件不监听端口，不读取 `.env` 或现存 Cookie 文件。
原分发包的 `curl_cffi==0.15.0` 不支持源码默认的 Chrome 150 TLS 档位；
适配器显式选该固定版本支持的 Chrome 146。真实服务是否接受此档位须由
当前扫码联调验证，不能以纯导入/模拟测试判断登录成功。

私有 stdin/stdout 每行一份 JSON：

```json
{"id":1,"method":"health","params":{}}
{"id":1,"result":{"available":true,"protocol":1}}
```

接口：

| method | params | result |
| --- | --- | --- |
| health | {} | available, protocol；仅本地依赖检测 |
| login_start | {} | login_id, status, qr_image, expires_at, message |
| login_status / login_poll | login_id | 上述字段，成功后 account |
| account_restore | session | status, account, message |
| account_export | {} | session, account；session 含凭据，仅供后端加密保存 |
| account_status | {} | status, account, message |
| logout / account_clear | {} | {}，使旧登录线程失效并清除内存凭据 |
| publish | session, publication, data_root, expected_account_id | note_id, url, published_at, account, visibility |

QR 状态为 `starting / waiting_scan / waiting_confirm / logged_in / expired /
error`。登录初始化与二维码查询在后台执行，前端查询不会等待完整初始化。
用户必须本人扫码并在 App 确认。成功还需真实 Creator 用户信息接口返回
非空用户 ID。账号状态为 `logged_out / logged_in / expired`。

`data_root` 由后端传入当前绝对数据目录，支持启动脚本 `-DataDir`；
`expected_account_id` 必须与恢复后核实的真实用户 ID 一致，之后才允许
上传和提交。这些字段只走内部 stdio，前端不提供目录或会话。
发布稿格式包含 `id,title,body,topics,media_type,visibility,assets`；素材为
`id,kind,path,sha256`，必须在工作台 `data` 目录且摘要一致。图文素材数组
保留用户确认的顺序；视频必须一份 `kind=video` MP4 和一份 `kind=cover`
图片，使用这份指定封面，SRT 不作为上传素材。

本工具首版支持范围为：标题 1–20 个 Unicode 字符，正文含 Creator 话题
标记不超过 1000 字符，最多 10 个话题，每个话题最多 30 字符；图文 1–18 张 JPEG/PNG，每张
20 MiB；视频 MP4、1 GiB、15 分钟。这是工具采用的保守限制，并非保证
平台当前所有账号的配额。话题必须查询到名称精确匹配项；找不到则明确
报错。`public` 映射 `privacy_info.type=0`，`private` 映射 `1`；该语义可在
[ReaJason/xhs 原作者源码](https://github.com/ReaJason/xhs/blob/master/xhs/core.py)
的 `create_note(..., is_private=False)` 与 `int(is_private)` 对照。

上传、话题查询和签名完成后，最终创建笔记端点**仅提交一次**，不调用
上游含重试的 `post_note`。网络断开、HTTP 408/5xx、非 JSON 响应、成功但无笔记 ID 归为
`submission_unknown=true`，必须人工核实后再处理；不得自动重复发布。
上传前失败归为未提交，不产生伪造成功。成功只认可真实 `success=true`
及 24 位笔记 ID。账号 Cookie 与 host scope、DS 签名程序通过 session
私有管道传递，DPAPI 持久化归 Go 后端负责。原始异常、日志、headers 与
Cookie 不进入 API 错误；Node 签名敏感载荷通过 stdin，不写临时 JSON 文件。

媒体 PUT 使用独立的上传超时：连接等待 15 秒，传输等待按文件大小以
256 KiB/s 的预算加 30 秒计算，限制在 60–900 秒。账号检查、许可查询和
最终创建笔记仍使用原有短超时，上传不会自动重试。该适配位于 `bridge.py`，
未改动固定校验的第三方源码。

```powershell
.venv/Scripts/python.exe -m unittest discover -s tests -v
```

测试覆盖二维码取消/过期、会话投影、素材摘要与路径、准确话题、指定
视频封面、可见范围、一次提交及结果不明。真实扫码和真实发布需分别验证；
测试不会向小红书创建笔记。
