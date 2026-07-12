# 图片与视频渠道对接总结

> 目的：把当前代码里已经接入的图片/视频渠道整理成可对比文档。后续拿到新上游文档时，先按本文的「统一入口与对接检查表」比对入参、出参、任务轮询、鉴权和计费；若形态相同，可直接复用现有 adaptor；若不同，再参考相近渠道实现。

## 统一入口与代码索引

### 图片同步接口

- OpenAI 兼容入口：`/v1/images/generations`、`/v1/images/edits`
- 标准 DTO：`dto.ImageRequest` / `dto.ImageResponse`（`dto/openai_image.go`）
- adaptor 接口：`channel.Adaptor.ConvertImageRequest`、`DoResponse`（`relay/channel/adapter.go`）
- 通用 OpenAI 兼容实现：`relay/channel/openai/adaptor.go`、`relay/channel/openai/relay_image.go`
- 上游返回统一成：
  ```json
  {"created": 1710000000, "data": [{"url": "...", "b64_json": "...", "revised_prompt": "..."}]}
  ```

### 视频/异步任务接口

- OpenAI 风格视频入口：`/v1/videos`、`/v1/videos/{video_id}`、`/v1/videos/{video_id}/remix`
- 内部统一任务 DTO：`relaycommon.TaskSubmitReq`（`relay/common/relay_info.go`）
  ```go
  type TaskSubmitReq struct {
      Prompt string
      Model string
      Mode string
      Image string
      Images []string
      Size string
      Duration int
      Seconds string
      InputReference string
      Metadata map[string]interface{}
  }
  ```
- adaptor 接口：`channel.TaskAdaptor`（`relay/channel/adapter.go`）
- adaptor 选择：`relay.GetTaskAdaptor`（`relay/relay_adaptor.go`）
- 任务提交/查询/转换：`relay/relay_task.go`
- 上游异步任务返回统一存入 `model.Task`；查询时输出 OpenAI Video 对象，优先由各 adaptor 的 `ConvertToOpenAIVideo` 适配。

## 新上游文档对接检查表

1. **接口模式**：同步图片 / 异步图片 / 异步视频 / OpenAI 视频兼容 / Replicate 风格 prediction。
2. **提交入参**：`prompt/model/size/duration/n/images` 是否能直接映射；其它参数放到 `metadata` 或图片请求的 `extra_fields`/额外 JSON 字段。
3. **图片输入**：URL、base64、data URL、multipart 文件；是否支持多图、首尾帧、参考图、视频/音频输入。
4. **鉴权**：Bearer、Token、API key header、AK/SK 签名、Google service account。
5. **提交返回**：task id 字段、错误字段、是否立即返回资源 URL。
6. **查询接口**：method/path/body；状态枚举如何映射到 `submitted/in_progress/success/failure`。
7. **结果提取**：视频 URL、封面 URL、图片 URL/base64、失败原因字段。
8. **计费**：是否按 `n`、秒数、分辨率、音频、prompt 改写、视频输入折扣等调整 `OtherRatio`；涉及动态计费先读 `pkg/billingexpr/expr.md`。
9. **兼容性**：JSON 调用必须使用 `common.Marshal/Unmarshal`；可选标量需要保留显式 0/false 时使用指针。
10. **测试**：至少覆盖请求转换、状态解析、结果 URL 提取、错误响应；后端测试用 `testify/require` 和 `assert`。

## 图片渠道汇总

| 渠道 | 代码 | 支持接口 | 上游请求形态 | 返回适配 | 适合作参考 |
| --- | --- | --- | --- | --- | --- |
| OpenAI / OpenAI-compatible | `relay/channel/openai` | generations、edits、stream | 原样 OpenAI JSON；edits 支持 multipart 重新封装、多 `image[]` | OpenAI image；流式转换 `image_generation.*` SSE | 新上游本身兼容 OpenAI 时直接复用 |
| Ali / DashScope / Bailian | `relay/channel/ali` | generations、edits | `input` + `parameters`；同步模型走 messages 内容，异步模型走 prompt；edits 可 multipart 转 data URL | `aliImageHandler` 统一为 OpenAI image，metadata 保留原始 body | 同时有同步/异步图片模型、参数复杂的上游 |
| Gemini / Imagen | `relay/channel/gemini` | generations（仅 `imagen*`） | `instances`/`parameters`，`size` 映射 aspect ratio，`quality` 映射 `imageSize` | Imagen response 转 OpenAI image；按生成图片数计 usage | Imagen/Google AI Studio 图片生成 |
| Vertex Imagen | `relay/channel/vertex` | generations（Imagen） | 复用 Gemini Imagen 转换，但 URL/鉴权使用 Vertex | 复用 Gemini image handler | Google Cloud Vertex Imagen |
| MiniMax | `relay/channel/minimax` | generations | `/v1/image_generation`；OpenAI 字段转 MiniMax image request | `image_urls`/`image_base64` 转 OpenAI image | 返回 data.image_urls 的图片上游 |
| Jimeng 即梦图片 | `relay/channel/jimeng` | generations | `prompt/model/width/height/sample_strength/return_url/image_urls` | `data.image_urls` 转 OpenAI image | 火山/即梦风格图片生成 |
| VolcEngine / Doubao image | `relay/channel/volcengine` | generations（edits 代码预留） | `/api/v3/images/generations`；豆包图生图也走 generations | 当前主要委托 OpenAI handler | OpenAI 兼容但需小幅 URL/格式转换 |
| Zhipu v4 image | `relay/channel/zhipu_4v` | generations | `/api/paas/v4/images/generations` 或特殊 OpenAI base URL | `image_url` 或下载转 `b64_json` | 返回单图字段而非 OpenAI data 数组 |
| Replicate | `relay/channel/replicate` | generations、edits | predictions 风格；输入组装到 `input`，edits 可上传 multipart 图片并使用 URL | output 数组/字符串转 URL 或 base64 | prediction/任务式但希望用图片同步入口的上游 |
| Baidu v2 | `relay/channel/baidu_v2` | generations、edits | `/v2/images/generations`、`/v2/images/edits` | 复用 OpenAI handler | 百度 v2/OpenAI-like 图片接口 |
| SiliconFlow | `relay/channel/siliconflow` | generations/edits（按模型） | 优先使用 `image_size/batch_size`，兼容 OpenAI `size/n`；支持 image/image2/image3 | 自身/通用响应适配 | 支持多图输入和批量参数的上游 |
| xAI | `relay/channel/xai` | generations、edits | OpenAI-like；模型含 `grok-imagine-image*` | 通用 OpenAI handler | OpenAI 近似图片 API |
| Cloudflare/AWS/其它 | 各自 `ConvertImageRequest` 多为未实现或透传 | 视具体模型 | 多数不是独立图片生成入口 | - | 不作为新增图片上游首选参考 |

### 图片请求字段映射约定

- 标准字段：`prompt/model/n/size/quality/response_format/stream` 尽量保留 OpenAI 语义。
- 非标准字段：
  - 图片请求 `ImageRequest.Extra` 会接收未知 JSON 字段；Ali 使用 `input`、`parameters`。
  - edits 的 `multipart/form-data` 需要特别处理，不要直接丢失文件字段；OpenAI/Ali/Replicate 都有可参考实现。
- 计费：`n` 不在 `ImageRequest.GetTokenCountMeta()` 中重复计费，渠道按实际返回/提交参数添加 `OtherRatio("n")`。

## 视频/任务渠道汇总

| 渠道 | ChannelType / platform | 代码 | 模型 | 提交 URL | 鉴权 | 主要能力 | 状态/结果适配 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| OpenAI/Sora | `OpenAI`/`Sora` | `relay/channel/task/sora` | `sora-2`, `sora-2-pro`, `seedance-gateway` | `/v1/videos`；remix `/v1/videos/{id}/remix` | Bearer | OpenAI 视频兼容、remix、multipart/JSON 原样透传 | 解析 OpenAI video 状态，输出 OpenAI Video shape |
| Ali Wan | `Ali` | `relay/channel/task/ali` | `wan2.5-i2v-preview`, `wan2.2-i2v-*`, `wanx2.1-i2v-*` | `/api/v1/services/aigc/video-generation/video-synthesis` | Bearer + `X-DashScope-Async: enable` | 图生视频、首尾帧、音频 URL、反向词、模板、分辨率/尺寸/时长 | `output.task_status/video_url` 映射任务状态和 URL |
| Doubao/VolcEngine Seedance | `DoubaoVideo`/`VolcEngine` | `relay/channel/task/doubao` | `doubao-seedance-1-*`, `doubao-seedance-2-*` | `/api/v3/contents/generations/tasks` | Bearer | 文生/图生/视频输入（metadata content），Seedance 2 视频输入折扣 | 解析 task id、状态、content 结果 URL |
| Gemini Veo | `Gemini` | `relay/channel/task/gemini` | `veo-3.0-*`, `veo-3.1-*` | `/{version}/models/{model}:predictLongRunning` | `x-goog-api-key` | 文生视频、图片首帧；metadata 支持 `durationSeconds/resolution/aspectRatio` | Google operation 轮询，提取 `generatedVideos[].video.uri` |
| Vertex Veo | `VertexAi` | `relay/channel/task/vertex` | 同 Gemini Veo | Vertex model URL `predictLongRunning` | service account OAuth token | Vertex 版 Veo；区域从模型/API version 推导 | operation response 转任务状态/URL |
| Kling | `Kling` | `relay/channel/task/kling` | `kling-v1`, `kling-v1-6`, `kling-v2-master` | `/v1/videos/image2video` 或 `/v1/videos/text2video` | JWT Bearer（AK/SK） | 文生、图生、尾帧；New API relay 模式可加 `/kling` 前缀 | 上游 task status 转内部状态，URL 转 OpenAI Video |
| Tencent VOD | `TencentVOD` | `relay/channel/task/tencentvod` | `kling-vod-1.6` 至 `kling-vod-3.0-omni` | `CreateAigcVideoTask` / `DescribeTaskDetail` | TC3-HMAC-SHA256（SecretId/SecretKey/SubAppId） | 腾讯云点播可灵文生、图生、参考图、首尾帧 | `AigcVideoTask` 状态和 `Output.FileInfos[].FileUrl` 转 OpenAI Video |
| Jimeng Video | `Jimeng` | `relay/channel/task/jimeng` | `jimeng_vgfm_t2v_l20` | `?Action=CVSync2AsyncSubmitTask&Version=2022-08-31` | 火山签名或 Bearer relay | 文生/图生/首尾帧；multipart `input_reference` 转 base64 | 解析 query 结果 URL/状态 |
| Jimeng Dimensio | `JimengDimensio` | `relay/channel/task/jimengdimensio` | `jimeng-video-seedance-2.0-*` | `/v1/videos/generations` | Bearer | Seedance/即梦尺寸化视频，默认 ratio/resolution/duration | Sora-compatible 输出 |
| MiniMax Hailuo | `MiniMax` | `relay/channel/task/hailuo` | `MiniMax-Hailuo-2.3`, `T2V-01`, `I2V-01`, `S2V-01` 等 | `/v1/video_generation` | Bearer | 文生、图生、首尾帧、主体参考；模型有默认分辨率/时长 | `/v1/query/video_generation` 查询，成功后再 retrieve file 得下载 URL |
| Vidu | `Vidu` | `relay/channel/task/vidu` | `viduq2`, `viduq1`, `vidu2.0`, `vidu1.5` | `/ent/v2/text2video|img2video|start-end2video|reference2video` | `Token` | 文生、图生、首尾帧、参考图；按图片数自动 action | `tasks/{id}/creations`，提取 `creations[0].url` |
| Xinghe | `XingheVideo` | `relay/channel/task/xinghe` | `xinghe-mini`, `xinghe-fast`, `xinghe-2.0` | `/api/generate-video` | Bearer | 默认 duration/ratio/resolution；metadata 扩展 | `task_status` 和 nested metadata/result_urls |
| AGGC | `AGGC` | `relay/channel/task/aggc` | `seedance-2.0` | `/api/v1/prot/generate` | `x-api-key` | 多输入：images/image_urls/video_urls/audio_urls；参数规整到 params | Sora-compatible 输出，提取 output URL |
| Yobox | `Yobox` | `relay/channel/task/yobox` | `seedance2`, `seedance-2.0`, `seedance-2.0-fast` | `/async/tasks` | Bearer | Seedance 任务；图生、参考图、音频/分辨率参数 | 多种嵌套 outputs 兼容，失败原因提取 |
| Suno | `suno` platform | `relay/channel/task/suno` | `suno_music`, `suno_lyrics` | `/suno/submit/{action}` | Bearer | 音乐/歌词，不是视频，但走 task 框架 | 使用专用批量轮询，不走通用 `ParseTaskResult` |

### Tencent VOD（腾讯云点播可灵）配置

- 渠道类型：`Tencent VOD`（`ChannelTypeTencentVOD = 63`）
- 默认 Base URL：`https://vod.tencentcloudapi.com`
- 密钥格式：`SecretId|SecretKey|SubAppId`
- 腾讯云服务/版本：`vod` / `2018-07-17`
- 提交动作：`CreateAigcVideoTask`
- 查询动作：`DescribeTaskDetail`

支持的 New API 模型名：

```text
kling-vod-1.6
kling-vod-2.0
kling-vod-2.1
kling-vod-2.5
kling-vod-2.6
kling-vod-o1
kling-vod-3.0
kling-vod-3.0-omni
```

这些模型分别映射到腾讯云 `ModelName=Kling` 与对应的 `ModelVersion`。也可以使用渠道模型映射，把自定义公开模型名映射到上述名称或直接映射到版本号（如 `3.0`）。

通用请求示例：

```json
{
  "model": "kling-vod-3.0",
  "prompt": "一只橘猫坐在钢琴前演奏，镜头缓慢推进",
  "duration": 5,
  "size": "1280x720",
  "metadata": {
    "resolution": "720P",
    "negative_prompt": "blurry, low quality",
    "audio_generation": false,
    "storage_mode": "Temporary"
  }
}
```

主要 `metadata` 字段：

| 字段 | 说明 |
| --- | --- |
| `resolution` | `720P` 或 `1080P` |
| `aspect_ratio` | 文生视频支持 `16:9`、`9:16`、`1:1` |
| `negative_prompt` | 反向提示词 |
| `enhance_prompt` | 是否自动优化提示词 |
| `audio_generation` | 是否生成音频 |
| `storage_mode` | `Temporary`（默认，URL 有效期由腾讯云决定）或 `Permanent` |
| `image_usage` | `FirstFrame`（默认）或 `Reference`；多参考图必须使用 `Reference` |
| `last_frame_url` / `image_tail` | 尾帧 URL；腾讯云当前要求 Kling 2.1 + 1080P |
| `input_region` | 输入文件区域，如 `Mainland` 或 `Oversea` |
| `scene_type` | Kling 特殊场景，如 `motion_control`、`avatar_i2v`、`lip_sync` |
| `seed` | 随机种子 |
| `ext_info` | 腾讯云模型特殊参数 JSON 字符串 |

渠道只负责上游请求和任务状态转换，不内置腾讯云售卖价格。启用模型前必须在 New API 中配置模型价格或计费表达式，避免缺少价格导致请求被拒绝。

## 视频标准入参到上游的常见映射

| 标准字段 | 常见上游字段 | 说明 |
| --- | --- | --- |
| `prompt` | `prompt`, `input.prompt`, `content[].text` | 必填程度看上游；图片/视频输入可能允许弱 prompt |
| `model` | `model` | 若配置了模型映射，用 `info.UpstreamModelName` |
| `images[0]` | `image`, `img_url`, `first_frame_url`, `start_frames[0]` | 图生视频首帧 |
| `images[1]` | `last_frame_url`, `image_tail`, `end_frames[0]` | 首尾帧尾帧 |
| `images[2+]` | `reference_images`, `image_references`, `subject_reference` | 参考图/主体参考 |
| `size` | `resolution`, `ratio`, `aspectRatio`, `size` | 有的用 `720p`，有的用 `720P`，有的用 `1280*720` |
| `duration`/`seconds` | `duration`, `durationSeconds`, `seconds` | `TaskSubmitReq.UnmarshalJSON` 已兼容数字/字符串 duration |
| `metadata` | 任意上游专属对象 | 推荐承载负向词、seed、水印、音频 URL、callback、prompt 改写等 |

## 选择参考实现建议

- **上游完全 OpenAI 视频兼容**：参考 `task/sora`，尽量原样透传 body；仅处理公开 task id 与 remix。
- **Google Veo/operation 风格**：参考 `task/gemini` 或 `task/vertex`。
- **国内异步视频，提交 task_id + 查询 task_status**：参考 `task/ali`、`task/doubao`、`task/vidu`。
- **AK/SK 或自定义签名**：参考 `task/kling`、`task/jimeng`。
- **模型参数影响计费**：参考 `task/gemini/billing.go`、`task/ali.ProcessAliOtherRatios`、`task/doubao.GetVideoInputRatio`。
- **多种响应嵌套都要兼容**：参考 `task/yobox`、`task/aggc` 的解析和测试。
