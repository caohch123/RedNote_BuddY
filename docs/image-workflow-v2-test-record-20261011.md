# 图片仿写 V2 测试与交付记录

日期：2026-10-11（Asia/Shanghai）。方案与接口见 [图片仿写 V2](image-workflow-v2.md)。本记录与同日较早的旧工作流修复记录分开保存。

后续用户提供了大图溢出截图，并授权准备单图真实示例。预览尺寸约束与空辅助图提示词保存的修复、23 项 Edge 回归和真实单图准备记录见 [后续测试记录](image-example-test-record-20261011.md)。本文件保留先前测试范围与时间点。

## 后端与供应商模拟

- `go test ./... -count=1`：Go 全包通过，包含原小红书／抖音、采集、发布、数据隔离及删除测试。
- `TestImageV2RolesCacheCompositionAndSerialBatch`：按内容复用分析缓存；主图完整 OCR；语言模型不接收未选特征或笔记正文；输出数量／清晰度变化保留提示词；三张串行生成，第二张明确失败后暂停；保留第一张；重复请求 ID 返回原批次；主动跳过失败后继续第三张。断言实际 prompt、图片顺序、尺寸、`n:1`、`prompt_extend:false` 与确认一致。
- `TestImageV2UnselectedReferenceAndStaleInputs`：没有选中特征的辅助图不进入请求；特征变更后旧提示词被拒；跨工作区图片及同一平台其他项目的私人图片不可用；文件内容变化被哈希校验拒绝。
- `TestImageV2UnknownNeverResubmitsAndReceiptRestores`：结果不明不自动执行剩余图片；禁止后台通用重试；有收据时模拟进程中断恢复下载，供应商付费 POST 始终只有一次。
- `TestImageV2ConfirmedUnknownEndsWithoutLeavingLock`：本人核实无结果后跳过／结束可解除锁定；启动重放已跳过任务不把批次再次锁住。
- `TestImageV2DeletionProtectsBatchAndResultInputs`：仅由历史结果／批次引用的素材仍不能单独删除。
- 原有图片供应商、图文模型、素材库与旧数据测试继续通过。未新增依赖或修改供应商类型。
- 补充模型 `finish_reason:length` 用例，拒绝把截断的 OCR 输出当作全文；`npm ls --depth=0` 依赖检查无缺失。

## 前端与 Edge

- `npm run test`：3 个文件、10 项单元测试通过，包含取消主图选择后采用结果不重新勾选该素材。
- `npm run build`：TypeScript 与 Vite 生产构建通过。
- Microsoft Edge 模拟验收共 **35 项通过**：
  - `image-rewrite.spec.ts`：13 项，覆盖角色、单一特征来源、OCR 修改、显式提示词编排、手改提示词、自动保存失败阻止提交、旧轮询保护、切换前保存、缺配置、编排失败、结果不明、旧方案只读、采用保留文案／辅助素材，以及 1536／825／597px 布局。
  - `media-sort.spec.ts`：7 项，包含两个工作区的拖拽、取消勾选、全部取消、保存刷新及发布快照顺序。
  - `project-media-library.spec.ts`：1 项，项目与工作区查询、预览、旧导航兼容。
  - `rewrite-input.spec.ts`：3 项，完整来源输入、独立口播、切换与保存保留编辑。
  - `workspace-deletion.spec.ts`：11 项，顶部布局、弹层隔离、采集／仿写／发布稿／工作区删除、素材与任务确认。

命令：

```powershell
cd web
npx playwright test tests/e2e/image-rewrite.spec.ts tests/e2e/project-media-library.spec.ts tests/e2e/media-sort.spec.ts tests/e2e/rewrite-input.spec.ts tests/e2e/workspace-deletion.spec.ts --reporter=line --output=test-results-image-workflow-v2
```

过程中的问题均已定位并复核：图标按钮与文字的可访问名称不一致，已为关键按钮补明确名称；Ant Radio 使用可见标签完成真实点击；保存错误同时出现在通知和提示栏，测试改为断言对应提示栏。旧视频用例仍断言“先保存文案再打开视频”并在视频页寻找文案保存按钮，已更新为既有模块独立规则；未因此更改产品视频行为。

最终补充单图分析入口后，重跑图片测试发现任务完成通知可能覆盖用户刚切回的阶段。已移除生成完成后的重复跳转，提示词编排完成也只在用户尚未切换阶段时导航；13 项图片流程随后重跑全部通过。最终图片测试输出目录为 `web/test-results-image-workflow-v2-final`。

Edge 截图：`data/verification/image-workflow-v2-edge-1536.png`、`image-workflow-v2-edge-825.png`、`image-workflow-v2-edge-597.png`。模拟图片为测试图片，不能作为生成质量验收。

2026-10-12 证据保留说明：以上三个通用文件名曾被新版模拟回归覆盖，原轮次截图没有独立备份，不能继续作为 2026-10-11 的原始视觉证据。本文文字测试结果仍为当时记录；下方按日期保存的实际页面证据未受影响。新版截图已改用 `image-flexible-edge-*` 独立文件名。

## 本机部署与实际页面

- 更新前分别检查两个工作区无排队、运行中或结果不明任务；保留旧后端可执行文件备份。
- 更新主进程到新版本，采集进程保持原 PID 41100。首次复制文件遇到 Windows 短暂文件锁，确认旧进程已经退出后重试成功；无数据改写或队列重投。
- 启动脚本使用 `-SkipEngine -NoBrowser`；主服务及采集服务健康检查通过。
- `node web/scripts/verify-image-workflow.mjs`（在 `web` 目录执行时为 `node scripts/verify-image-workflow.mjs`）使用已安装 Microsoft Edge，对真实 `127.0.0.1:8080` 只读核验。
- 六组尺寸能力、最大输入 3 张／输出 4 张、旧方案提示词保持原样、历史结果提示词与存储一致、旧结果不可直接套用新方案、已有图片可读取，均通过。
- 实际页面 1536、825、597px 无整页水平溢出；页面异常 0，API 写请求 0。没有为只读验收创建或修改用户方案。

证据：`data/verification/edge-image-workflow-v2-20261011/live-checks.json` 及该目录的三张实际页面截图。脚本在浏览器层拦截所有 API 写请求，防止只读验收误触发生成。

## 尚需本人验收

本次没有调用真实付费图片模型，没有填充官方图库，也没有正式发布作品。软件模拟流程通过不代表模型已经获得账号权限，或实际图片符合审美／文字准确性要求。本人选定主图、辅助图、保留特征和最终提示词后，可先确认生成 1 张，核对实际画面及文字再采用；不自动追加重画。
