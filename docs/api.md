# 工作台 API

基路径 `/api/v1`，JSON 字段使用 snake_case。后端默认地址为 `http://127.0.0.1:8082/api/v1`；前端 `http://127.0.0.1:8080` 通过代理访问同一 API。

普通成功响应直接返回资源；列表响应为 `{items,total}`；错误响应为 `{error,code}`。耗时操作返回 HTTP 202 和 `{job_id,job_ids?}`，通过 `/jobs` 查询进度。

API 现要求用户或管理员会话。先调用 `GET /session?realm=user`（管理员使用 `realm=admin`），保留 Cookie 与 `{csrf_token}`，再调用对应的 `/auth/login` 或 `/admin/auth/login`。首次登录需要改密，完整双端接口与权限见 [账号和管理端](accounts-and-admin.md)。所有写操作，以及会更新导出状态的 `GET /publications/:id/export`，需要 `X-CSRF-Token`。Host 必须匹配 API 地址；Origin 必须匹配 API 自身或后端明确配置的前端来源，支持 CORS 预检。AI 密钥与采集 Cookie 经设置写入，读取仅返回掩码和配置状态；创作平台登录会话由扫码流程取得并由后端加密保存，账号接口只返回显示资料与状态。

## 工作区范围

资源请求带 `?platform=xiaohongshu`，列表、详情、写操作、素材和后台任务均按平台处理。账号面板只显示小红书。不存在的记录或账号入口返回 404，非法或重复的 `platform` 参数返回 400；未带范围的旧 API 保持兼容。

仿写与视频项目跟随来源笔记的平台。发布稿的来源项目、引用素材及不可变的 `target_platform` 必须属于同一平台，创建时拒绝错配。工作区列表只返回满足这项一致性要求的记录；历史错配稿件保留在数据库但从工作区隐藏，其详情、素材预览、导出和发布均不可访问。

AI 服务配置共用。保存设置前获取最新配置，仅提交脏字段和实际修改的服务项，避免旧缓存覆盖新设置。设置管理采集 Cookie 和采集服务。

## 接口

| 方法与路径 | 请求／返回 |
| --- | --- |
| GET `/workspaces` | 当前有效工作区 `{items:[{id,platform,name}],total}`；初始小红书 |
| POST `/workspaces` | `{platform}`；重新添加已删除的平台为全新空白工作区 |
| DELETE `/workspaces/:id` | `{confirm:true}`；级联清除该平台本地内容及登录会话 |
| GET `/collections` | 查询 `q,status,tag,platform,sort,order,page,page_size`；platform 为 xiaohongshu；sort 支持 collected_at、title、likes、collects、comments |
| POST `/collections` | `{text,platform?}`，最多50条当前平台 URL、分享文字或短链接；作用域内拒绝其他平台链接；返回采集任务、existing_ids 与 rejected_items |
| GET `/collections/:id` | `{note,assets}` |
| PATCH `/collections/:id` | `{tags:[]}` |
| POST `/collections/:id/recollect` | 更新原始采集，保留独立仿写版本 |
| POST `/collections/:id/extract` | `{asset_ids?:[]}`，空数组表示全部可提取素材 |
| GET `/rewrites` | 仿写项目列表，置顶项目优先；各组内按内容更新时间降序 |
| POST `/rewrites` | `{note_id}`，创建原文副本供手动编辑；小红书按原笔记图片顺序为每张已就绪图片创建一个独立图片仿写草稿 |
| GET `/rewrites/:id` | `{rewrite,note,assets,videos}` |
| PATCH `/rewrites/:id` | `{title?,pinned?}`，至少一项；名称去首尾空白、1–100 字。`pinned:true/false` 支持置顶／取消置顶并持久保存，置顶不改变文案、素材或内容更新时间；返回 Rewrite |
| POST `/rewrites/:id/images/initialize` | 小红书：无请求体；为尚无图片方案的项目按原笔记图片顺序初始化草稿，直接返回 `ImageRewrite[]`。已有方案时原样返回，重复请求不重复创建；未就绪、非图片和已移除素材跳过，不创建模型任务 |
| POST `/rewrites/:id/generate` | `{audience,tone,length,instructions,include_body,extraction_asset_ids}` |
| POST `/rewrites/:id/versions` | `{title,body,topics,script,asset_ids,parent_version_id?}`，保存独立版本 |
| POST `/rewrites/:id/confirm` | `{version_id}` |
| GET `/media-library` | 项目素材库使用 `rewrite_id` 查询该项目生成结果；跨工作区或不存在的项目返回 404。省略参数兼容旧工作区索引。`items` 包含 asset、rewrite_id、project_title、version_id、origin、revision、created_at、stale；排除来源下载、配音和参考图，按时间降序去重 |
| DELETE `/rewrites/:id/media-library/:asset_id` | `{confirm:true}`；从当前项目素材库移除生成图片、视频或用户上传素材。返回 `{asset_id,library_deleted_at,history_preserved:true}`，重复删除幂等。仅标记库内删除，保留历史与发布引用；跨工作区、跨项目、来源笔记、官方参考快照返回 404 |
| GET `/style-library` | 产品只读参考图库，返回 `items,total,categories`；不返回磁盘路径，不存在或损坏的参考图不展示 |
| GET `/style-library/:id/file` | 安全预览已上架的 PNG/JPEG；目录外路径不可访问 |
| POST `/rewrites/:id/images` | 小红书：`{version_id,source_asset_ids:[1–2张],style_library_id?,style_reference_id?,style?}`，创建图片方案；两种风格参考字段不能同时使用，官方参考图保存独立项目快照 |
| PATCH `/rewrites/:id/images/:image_id` | `{revision,analyses,style,prompt,brief?,image_text?,rebuild_prompt?}`；画面要求最多 1000 字，图中文字最多 500 字、留空不加文字。要求变化时自动更新未手改的提示词，同时修改要求与提示词须明确 rebuild，否则 409；历史结果保留 |
| POST `/rewrites/:id/images/:image_id/analyze` | V2 `{revision,suggest_proposals:true}`，202，分析全部主图及可选参考图，再生成恰好三个 `proposals`，不默认选中；复用有效分析及建议缓存。未带建议标志保留旧特征分析接口 |
| POST `/rewrites/:id/images/:image_id/generate` | 新界面提交 `{revision,prompt}`，与已保存方案不一致返回 409；202 `image_rewrite`。旧空载荷兼容，仍校验分析与有效提示词。结果保存 prompt、model、revision、ratio、source_asset_ids、style_reference_id，历史记录不补造信息 |
| POST `/rewrites/:id/images/:image_id/apply` | `{asset_id,version_id?}`；当前修订结果更新目标版本素材，保留该版文案，不要求重新仿写文字；派生版本需重新确认，发布前校验标题限制；重复应用幂等 |
| POST `/rewrites/:id/images/:image_id/resolve` | `{confirm_no_result:true}`；本人核实供应商没有结果后，原子解除结果不明暂停 |
| GET `/video-projects` | 仅返回新版视频仿写方案 |
| POST `/video-projects` | `{workflow:"video_rewrite_v2",rewrite_id,version_id?,primary_asset_id?}`；旧工作流或省略workflow返回410 |
| GET / PATCH `/video-projects/:id` | 新版方案查询／自动保存，字段见视频仿写V2；旧项目返回410 |
| POST `/video-projects/:id/compose-script` | 分析所选素材并编排最终提示词，返回202持久化任务；审阅只编辑prompt |
| POST `/video-projects/:id/generate` | 新版 `{revision,request_id,prompt}`，仅用户确认后生成一条视频 |
| POST `/video-projects/:id/resume`、`/apply` | 新版恢复／采用；旧图片配音plan与render路由已删除 |
| GET `/publications` | 发布稿列表 |
| POST `/publications` | `{rewrite_id,version_id,asset_ids?,target_platform?}`，仅接受当前工作区确认版本及素材；来源与目标平台必须一致且目标不可修改；无作用域旧调用省略目标时默认小红书，也不得创建跨平台稿件 |
| GET `/publications/:id` | 不可变发布内容快照 |
| GET `/publications/:id/export` | 流式 ZIP；更新为已导出，保留已发布状态 |
| PATCH `/publications/:id` | `{status:'published',published_url,published_at}`；时间使用 RFC3339 |
| GET `/xhs/account` | 登录状态、依赖状态、账号 ID／昵称／头像；不返回凭据 |
| POST `/xhs/login` | 创建扫码流程，返回 `202 {login_id}` |
| GET `/xhs/login/:id` | 初始化、待扫码、待手机确认、成功、过期／失败；返回真实二维码 PNG 与过期时间 |
| DELETE `/xhs/account` | 清除本机保存的创作平台会话 |
| POST `/publications/:id/publish-preview` | `{visibility:'public'\|'private'}`，返回绑定账号及素材哈希的五分钟确认 token 和实际发布快照 |
| POST `/publications/:id/publish` | `{confirmation_token}`，返回 `202 {job_id}`；token 单次，重复提交或结果不明时拒绝 |
| GET `/jobs`、GET `/jobs/:id` | 进度、阶段、错误和不含密钥的模型／制作记录 |
| DELETE `/jobs?platform=xiaohongshu` | 清空全部任务并中断本地处理；需要 CSRF。返回 `{deleted,ids,interrupted,stopping,remote_unstopped}`；记录持久化清除标记，迟到回调及重启不会复活任务。保留项目内容、生成结果及供应商提交凭据 |
| DELETE `/jobs/completed?platform=xiaohongshu` | 旧客户端兼容入口，只清理已结束的任务；新界面使用 DELETE `/jobs` |
| POST `/jobs/:id/retry` | `{remote_task_id?}`；仅旧视频任务可填远程ID恢复；图片提交结果不明时暂停并拒绝重投 |
| GET `/settings` | 配置地址、模型、工具路径、密钥掩码 |
| PATCH `/settings` | 普通用户仅可更新自己的 `{cookie}`；以下配置字段仅通过管理员 `/admin/models` 更新：前端仅提交脏字段；工具路径空字符串表示自动检测，省略保持原值；Node路径切换遇到正在执行的账号操作返回409；空密钥保留原值，`__CLEAR__` 清除指定密钥或 Cookie；AI 服务共用 |
| POST `/settings/test`（内部接口，用户端禁用） | `{capability:'collector'\|'text'\|'vision'\|'ocr'\|'asr'\|'tts'\|'image'\|'video'}`；返回 `{ok,message}` |
| GET `/health` | 公开最小状态 `{service,status,authentication:true}`，不暴露运行环境详情 |
| GET `/assets` | 可按 `owner_id` 查询；不会返回本地文件路径 |
| POST `/assets` | multipart：`file,owner_id`，仅支持图片、视频和音频 |
| GET `/assets/:id/file` | 本地预览／下载，支持 HTTP Range；`?download=1` 下载附件 |
| PATCH `/assets/:id` | `{position}` |
| PATCH `/assets/:id/text` | `{text,segments:[{text,start,end}]}`；时间单位秒 |

任务状态包括 queued、running、succeeded、failed、waiting_config、submission_unknown。已有远程视频任务 ID 只恢复查询；提交结果不明不会自动重发付费请求。明确失败的远程任务经人工重试后才重新提交。

项目素材库与发布选择分别管理。生成完成、选图上传仅入素材库；`apply` 明确采用后才替换主图对应发布位置。从素材库“加入发布素材”只追加当前发布草稿，随后沿用 `/versions` 保存、`/confirm` 确认再创建发布稿，不会自动生成文案或发布。视频项目生成的配图和成片也可由所属仿写项目显式选择。取消勾选、顺序和未保存文案继续保留。

新上传素材的 `asset.name` 保存原始文件名；选图窗口按名称、已有 OCR／视觉分析及图库标签搜索，不将分辨率当作关键词，也不会为了搜索调用模型。`library_deleted_at` 非空的素材从库索引和新选图候选中排除；既有方案、发布选择和文件预览保持有效。需要物理清理时仍使用受引用检查保护的 `/assets/:id` 删除或删除整个项目。

`POST /settings/test` 的视频检测只调用百炼原生 `/api/v1/tasks?page_no=1&page_size=1`，不创建任务。`ok:true` 表示密钥可查询任务，不代表万相模型权限已开通。视频配置使用原生 `/api/v1` 地址及 `wan3.0-video`／`wan3.0-video-prime`；不使用兼容文本地址或旧供应商密钥。上游原始错误正文不进入响应或日志。

每个仿写版本保留 source_snapshot 和所选 source_extractions；手工编辑通过 parent_version_id 继承正确的来源。重新采集与人工修正提取文字不会改变历史版本的来源快照。

发布 ZIP 包含文案、标题、按顺序命名的本地素材及 manifest.json；视频稿包含用户确认采用或选择的视频素材；已有归档稿继续按其快照导出。纯文案版本可导出文案包。素材缺失时返回具体错误。导出、下载、复制或打开创作平台均不会标记实际发布成功。

扫码发布支持图文和视频稿；视频使用确认稿中的封面，SRT 文件保留在发布包中，不作为图片上传。平台话题按名称匹配；找不到指定话题时明确失败，不用其他相近话题代替。首版工具限制为标题 20 字符、正文与实际话题文本共 1000 字符、图文 1–18 张 JPEG/PNG，每张不超过 20 MiB，视频为单个 MP4、不超过 1 GiB／15 分钟；平台账号能力和实时规则仍可能使请求被拒绝。

发布状态额外包括 publishing、publish_failed、submission_unknown。真正收到作品 ID 才更新 published；任务入队和发布稿状态原子写入。进程在发布时退出、最终请求超时或成功响应缺少作品 ID 时不会自动重复提交。账号会话单独保存，持久化任务不含 Cookie。确认 token 不进入任务内容或公开 API 日志。

数据库迁移位于 `backend/internal/store/migrations/001_initial.sql`，由 Go 程序嵌入并在启动时执行。完整备份必须包含数据目录内的数据库及全部媒体文件。


## 图片仿写 V2

以上图片接口保留旧版兼容分支。新版创建传 `workflow_version:2`，默认 `edit_intent.mode:flexible`。新界面分析提交 `suggest_proposals:true`，返回三个建议及 `proposals_input_digest`；不自动选中。PATCH 传 `revision,selected_proposal_id,extra_requirements?`：选择当前持久化建议，后端绑定对应八维控制；附加要求最多1000字，空字符串或 null 清空，省略则保留。更换图片清除建议，过期选择返回409。八维 `feature_controls` 继续作为内部生成依据及旧客户端兼容入口，`feature_sources` 仅由图片控制派生。

主图为采用位置，实际模型输入只含贡献特征的图片，旧接口允许零图片输入。生成必须携带 `revision,prompt,request_id`。GET `/image-capabilities` 返回规则版本 6；POST `/rewrites/:id/images/:image_id/compose-prompt` 和 `/rewrites/:id/images/:image_id/batches/:batch_id/resume` 沿用持久化任务和回执恢复。建议选择及附加要求会进入提示词依据摘要，并以不可变快照保存在生成批次和结果中。完整请求、幂等与兼容约定见 [V2 接口说明](image-workflow-v2.md)。

## 删除与任务一致性

视频新版工作流、请求字段及短视频／本地文件自动临时上传约定见 [视频仿写V2](video-rewrite-v2.md)。新版工作流统一支持；旧图片配音分支及plan/render接口已移除，新版使用独立 `video_script` 和 `video_rewrite` 持久化任务。

`DELETE /collections/:id`、`/rewrites/:id`、`/video-projects/:id`、`/publications/:id`、`/assets/:id` 需要 `{confirm:true}`。批量采集删除为 `DELETE /collections {ids:[...],confirm:true}`，最多500条，校验或清理失败时整批不提交。前端提供两次确认和范围说明。删除返回 `{deleted:{notes,rewrites,videos,publications,assets,jobs},files_deleted,retained_assets,cleanup_pending?}`。

来源删除沿真实所有权级联；仍被使用的共享素材保留。单独删除被引用素材返回409。排队任务与记录一起取消，运行中或提交结果不明任务返回409。文件先安全暂存再提交数据库，失败回滚；进程重启会处理文件清理日志。路径限定本地媒体目录，拒绝目录联接、符号链接与越界路径。删除工作区后带该平台的资源、账号和新增采集请求均拒绝，直至重新添加。平台上已有作品不会删除。

采集占位记录与collect任务原子写入，失败显示真实阶段而非等待标题；重试同步实体与任务状态。启动时校正孤立等待记录。图片生成提交前持久化submitting，响应结果不明或此阶段崩溃进入submission_unknown，禁止自动重投。旧图片配音、口播编排、镜头与合成任务停用，重试返回410；未知提交保留待核实状态和供应商凭据记录。
