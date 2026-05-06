# 视频生成 API 文档

> 基于当前仓库代码实现整理，覆盖 `/v1/videos`、`/v1/video/generations`、`/kling/v1/videos/*`、`/jimeng/` 等视频生成相关路由。
>
> 本文档以当前服务实现为准，适用于 `new-api` 的视频异步任务提交、查询与内容拉取场景。

---

## 1. 概览

本项目的视频生成接口是**异步任务模型**：

1. 客户端提交生成请求；
2. 服务立即返回公开任务 ID（通常为 `task_xxx`）；
3. 客户端轮询查询任务状态；
4. 任务成功后，再拉取视频内容或直接读取结果地址。

### 1.1 主要路由族

| 路由族 | 说明 |
| --- | --- |
| `/v1/videos` | OpenAI 兼容视频接口，推荐优先使用 |
| `/v1/video/generations` | 旧版统一视频接口，兼容保留 |
| `/kling/v1/videos/*` | Kling 官方风格接口 |
| `/jimeng/` | 即梦官方风格接口入口 |

### 1.2 认证方式

除视频内容拉取路由外，视频相关接口默认要求：

```http
Authorization: Bearer <token>
```

其中：

- `/v1/videos/:task_id/content` 支持 `TokenOrUserAuth()`，即 **API Token** 或 **已登录用户会话**；
- 其余视频提交/查询路由使用 API Token。

### 1.3 异步任务公共特征

- 对外公开的任务 ID 通常是 `task_xxx` 格式；
- 内部会保存真实上游任务 ID，但对外优先返回公开任务 ID；
- 渠道选择由 `Distribute()` 根据请求体中的 `model`、令牌分组、可用渠道等动态决定；
- 同一条 `/v1/videos` 路由可代理到不同视频渠道（如 Sora / OpenAI / Gemini / Vertex / 其他视频渠道）；
- 提交接口会在响应头中附带：

```http
X-New-Api-Other-Ratios: {"seconds":4,"size":1}
```

该头部表示本次任务计费用到的附加倍率（如时长、分辨率等）。

---

## 2. 通用对象

## 2.1 统一视频提交请求（运行时）

运行时视频任务提交底层使用统一结构 `TaskSubmitReq`，支持以下字段：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `prompt` | string | 是 | 视频提示词 |
| `model` | string | 是 | 模型名称 |
| `mode` | string | 否 | 模式/子模式 |
| `image` | string | 否 | 单图输入（部分渠道兼容） |
| `images` | string[] | 否 | 多图输入 |
| `size` | string | 否 | 尺寸/分辨率 |
| `duration` | int | 否 | 时长（秒） |
| `seconds` | string | 否 | 时长，兼容字符串格式 |
| `input_reference` | string | 否 | 参考图，常用于 OpenAI/Sora 风格接口 |
| `metadata` | object / string | 否 | 渠道扩展参数 |

### 请求体格式支持

| Content-Type | 是否支持 | 说明 |
| --- | --- | --- |
| `application/json` | 是 | 推荐 |
| `application/x-www-form-urlencoded` | 是 | 支持基础字段 |
| `multipart/form-data` | 是 | 适合上传参考图/文件 |

### 任务类型判定

服务会根据请求内容自动识别：

- **文生视频**：未提供 `input_reference` / `images`；
- **图生视频**：提供了 `input_reference` / `images` / `image` 中的图片输入。

### `metadata` 用法

`metadata` 用于承载不同渠道的扩展参数，例如：

- Kling 的 `negative_prompt`、`aspect_ratio`、`camera_control`
- Gemini / Vertex 的分辨率、纵横比扩展参数
- Jimeng 官方原始字段透传

建议：

- 通用字段放顶层；
- 渠道专属字段放 `metadata`。

---

## 2.2 OpenAI 风格视频任务对象

`/v1/videos` 提交与查询通常返回以下对象：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | 公开任务 ID |
| `task_id` | string | 兼容旧字段，通常与 `id` 相同 |
| `object` | string | 固定为 `video` |
| `model` | string | 模型名称 |
| `status` | string | 任务状态 |
| `progress` | int | 进度百分比 |
| `created_at` | int64 | 创建时间（Unix 秒） |
| `completed_at` | int64 | 完成时间（可选） |
| `expires_at` | int64 | 过期时间（可选） |
| `seconds` | string | 时长（可选） |
| `size` | string | 尺寸（可选） |
| `remixed_from_video_id` | string | remix 来源视频 ID（可选） |
| `error` | object | 错误对象（可选） |
| `metadata` | object | 扩展元数据（可选） |

### 常见状态值

| 状态 | 说明 |
| --- | --- |
| `queued` | 已排队 |
| `in_progress` | 处理中 |
| `completed` | 已完成 |
| `failed` | 已失败 |
| `unknown` | 未知状态 |

> 说明：不同上游的原始状态值可能略有差异，系统会尽量转换为统一状态；个别渠道可能保留部分上游语义。

### 响应示例

```json
{
  "id": "task_9d9f0a7d6c6b4f83a2d4e9b8f7d12345",
  "task_id": "task_9d9f0a7d6c6b4f83a2d4e9b8f7d12345",
  "object": "video",
  "model": "sora-2",
  "status": "queued",
  "progress": 0,
  "created_at": 1760000000
}
```

---

## 2.3 通用任务查询响应（旧版接口）

旧版 `/v1/video/generations/:task_id` 查询接口返回通用任务包装结构：

```json
{
  "code": "success",
  "message": "",
  "data": {
    "id": 1,
    "created_at": 1760000000,
    "updated_at": 1760000010,
    "task_id": "task_xxx",
    "platform": "25",
    "user_id": 1001,
    "group": "default",
    "channel_id": 12,
    "quota": 500000,
    "action": "generate",
    "status": "SUCCESS",
    "fail_reason": "",
    "result_url": "https://your-server/v1/videos/task_xxx/content",
    "submit_time": 1760000000,
    "start_time": 1760000001,
    "finish_time": 1760000030,
    "progress": "100%",
    "properties": {
      "upstream_model_name": "sora-2",
      "origin_model_name": "sora-2"
    },
    "data": {}
  }
}
```

其中 `data` 中常见字段：

| 字段 | 说明 |
| --- | --- |
| `task_id` | 公开任务 ID |
| `platform` | 渠道/平台标识 |
| `action` | 任务动作，如 `generate` / `text_generate` / `remix` |
| `status` | 内部任务状态，如 `QUEUED` / `IN_PROGRESS` / `SUCCESS` / `FAILURE` |
| `result_url` | 结果 URL；某些渠道会给出代理地址 |
| `data` | 上游原始响应快照 |

---

## 2.4 错误响应

视频接口当前实现中，错误响应可能出现两种风格。

### A. 任务错误风格（TaskError）

```json
{
  "code": "invalid_request",
  "message": "prompt is required",
  "data": null
}
```

### B. OpenAI 风格错误

```json
{
  "error": {
    "message": "Invalid request body",
    "type": "invalid_request_error"
  }
}
```

### 常见错误状态码

| 状态码 | 说明 |
| --- | --- |
| `400` | 参数错误、任务不存在、请求体非法 |
| `401` | 未认证 |
| `403` | 无权限、渠道/资源不可用 |
| `404` | 路由不存在，或内容拉取时任务不存在 |
| `429` | 上游或分组限流/负载饱和 |
| `500` | 服务内部错误 |
| `502` / `503` | 上游服务异常或无可用渠道 |

---

## 3. OpenAI 兼容视频接口

## 3.1 POST `/v1/videos`

### 说明

统一视频任务提交接口，推荐优先使用。

### 认证

```http
Authorization: Bearer <token>
```

### 支持的请求体格式

- `application/json`
- `application/x-www-form-urlencoded`
- `multipart/form-data`

### 请求字段

沿用 [2.1 统一视频提交请求](#21-统一视频提交请求运行时)。

### Sora 模型限制（当前实现）

当 `model` 以 `sora-2` 开头时，服务会执行额外校验：

#### `sora-2`
仅允许：

- `720x1280`
- `1280x720`

#### `sora-2-pro`
允许：

- `720x1280`
- `1280x720`
- `1792x1024`
- `1024x1792`

### 示例 1：JSON 文生视频

```bash
curl -X POST http://localhost:3000/v1/videos \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sora-2",
    "prompt": "一只猫在舞台上弹钢琴",
    "size": "1280x720",
    "seconds": "5"
  }'
```

### 示例 2：multipart 图生视频

```bash
curl -X POST http://localhost:3000/v1/videos \
  -H "Authorization: Bearer sk-xxx" \
  -F "model=sora-2-pro" \
  -F "prompt=让这张图片中的人物向镜头慢慢走来" \
  -F "size=1024x1792" \
  -F "seconds=5" \
  -F "input_reference=@./image.png"
```

### 示例 3：带 metadata 的统一请求

```json
{
  "model": "veo-3.0-generate-001",
  "prompt": "一辆红色跑车在雨夜街头高速掠过",
  "duration": 5,
  "size": "1280x720",
  "metadata": {
    "aspect_ratio": "16:9",
    "resolution": "720p"
  }
}
```

### 成功响应

```json
{
  "id": "task_9d9f0a7d6c6b4f83a2d4e9b8f7d12345",
  "task_id": "task_9d9f0a7d6c6b4f83a2d4e9b8f7d12345",
  "object": "video",
  "model": "sora-2",
  "status": "queued",
  "progress": 0,
  "created_at": 1760000000
}
```

### 成功响应头

```http
X-New-Api-Other-Ratios: {"seconds":5,"size":1}
```

### 说明

- 服务会返回**公开任务 ID**，而不是上游真实任务 ID；
- 实际走哪个上游渠道，由 `model` 与分发策略决定；
- 同一个路由可以代理到 Sora / Gemini / Vertex / 其他视频渠道。

---

## 3.2 GET `/v1/videos/{task_id}`

### 说明

查询视频任务状态，返回 OpenAI 风格视频对象。

### 认证

```http
Authorization: Bearer <token>
```

### 路径参数

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `task_id` | 是 | 提交接口返回的公开任务 ID |

### 请求示例

```bash
curl -X GET http://localhost:3000/v1/videos/task_xxx \
  -H "Authorization: Bearer sk-xxx"
```

### 响应示例（处理中）

```json
{
  "id": "task_xxx",
  "task_id": "task_xxx",
  "object": "video",
  "model": "sora-2",
  "status": "in_progress",
  "progress": 35,
  "created_at": 1760000000
}
```

### 响应示例（已完成）

```json
{
  "id": "task_xxx",
  "task_id": "task_xxx",
  "object": "video",
  "model": "sora-2",
  "status": "completed",
  "progress": 100,
  "created_at": 1760000000,
  "completed_at": 1760000030,
  "seconds": "5",
  "size": "1280x720"
}
```

### 说明

- 对 Gemini / Vertex 等支持实时查询的渠道，服务会尽量主动从上游刷新最新状态；
- 对其他渠道，返回值以本地保存的任务状态和上游响应快照为主。

---

## 3.3 GET `/v1/videos/{task_id}/content`

### 说明

拉取视频二进制内容或代理下载最终视频。

### 认证

支持以下任一方式：

- `Authorization: Bearer <token>`
- 已登录用户会话

### 路径参数

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `task_id` | 是 | 公开任务 ID |

### 请求示例

```bash
curl -L http://localhost:3000/v1/videos/task_xxx/content \
  -H "Authorization: Bearer sk-xxx" \
  -o output.mp4
```

### 成功响应

- `200 OK`
- `Content-Type` 为上游视频 MIME 类型，如 `video/mp4`
- 响应体为视频二进制流

### 失败场景

| 场景 | 返回 |
| --- | --- |
| 任务不存在 | `404` |
| 任务未完成 | `400` |
| 上游内容拉取失败 | `502` / `500` |

### 说明

- 若任务状态不是成功态，接口会拒绝返回视频内容；
- 某些渠道返回真实 URL，服务会代理转发；
- 某些渠道返回 data URL/base64 内容，服务会解码后直接输出视频流。

---

## 3.4 POST `/v1/videos/{video_id}/remix`

### 说明

基于已存在的视频任务进行 remix / 二次生成。

### 认证

```http
Authorization: Bearer <token>
```

### 路径参数

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `video_id` | 是 | 原视频任务 ID（公开任务 ID） |

### 请求体

当前 remix 路径下最关键字段为：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `prompt` | 是 | remix 提示词 |
| `metadata` | 否 | 可选扩展参数 |

### 请求示例

```bash
curl -X POST http://localhost:3000/v1/videos/task_origin_xxx/remix \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "保留主体动作，将场景改成黄昏海边"
  }'
```

### 响应示例

```json
{
  "id": "task_new_xxx",
  "task_id": "task_new_xxx",
  "object": "video",
  "model": "sora-2",
  "status": "queued",
  "progress": 0,
  "created_at": 1760000100,
  "remixed_from_video_id": "task_origin_xxx"
}
```

### 说明

- remix 会先查找原始任务；
- 当前实现会尽量继承原任务的模型、渠道与计费倍率；
- 若原任务不存在或原渠道被禁用，请求会失败。

---

## 4. 旧版统一视频接口

## 4.1 POST `/v1/video/generations`

### 说明

旧版统一视频提交接口。底层仍走统一任务提交流程，与 `/v1/videos` 的执行链路基本一致。

### 认证

```http
Authorization: Bearer <token>
```

### 请求体

运行时建议按 [2.1 统一视频提交请求](#21-统一视频提交请求运行时) 传参。

### 示例

```bash
curl -X POST http://localhost:3000/v1/video/generations \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sora-2",
    "prompt": "一艘飞船穿越云海",
    "seconds": "5"
  }'
```

### 成功响应

通常返回公开任务 ID 对应的任务对象，字段与 OpenAI 风格对象接近；推荐新接入优先使用 `/v1/videos`。

---

## 4.2 GET `/v1/video/generations/{task_id}`

### 说明

旧版统一视频任务查询接口。

### 认证

```http
Authorization: Bearer <token>
```

### 返回结构

返回 `TaskResponse<TaskDto>`，而不是 `/v1/videos/{task_id}` 的 OpenAI 视频对象。

### 请求示例

```bash
curl -X GET http://localhost:3000/v1/video/generations/task_xxx \
  -H "Authorization: Bearer sk-xxx"
```

### 响应示例

```json
{
  "code": "success",
  "message": "",
  "data": {
    "task_id": "task_xxx",
    "status": "IN_PROGRESS",
    "progress": "50%",
    "result_url": ""
  }
}
```

---

## 5. Kling 官方风格接口

Kling 路由会先通过 `KlingRequestConvert()` 转换为统一任务请求，然后再进入统一视频任务链路。

---

## 5.1 POST `/kling/v1/videos/text2video`

### 说明

Kling 文生视频接口。

### 认证

```http
Authorization: Bearer <token>
```

### 请求字段

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model_name` | string | 否 | 模型名，如 `kling-v1` |
| `prompt` | string | 是 | 提示词 |
| `negative_prompt` | string | 否 | 反向提示词 |
| `cfg_scale` | number | 否 | CFG 参数 |
| `mode` | string | 否 | 模式，如 `std` |
| `camera_control` | object | 否 | 镜头控制 |
| `aspect_ratio` | string | 否 | 纵横比，如 `16:9` |
| `duration` | string | 否 | 时长 |
| `callback_url` | string | 否 | 回调地址 |
| `external_task_id` | string | 否 | 外部任务 ID |

### 请求示例

```json
{
  "model_name": "kling-v1",
  "prompt": "一只橘猫坐在钢琴前演奏，镜头缓慢推进",
  "negative_prompt": "blurry, low quality",
  "mode": "std",
  "aspect_ratio": "16:9",
  "duration": "5"
}
```

### 成功响应

通常返回统一生成后的公开任务信息：

```json
{
  "id": "task_xxx",
  "task_id": "task_xxx",
  "object": "video",
  "status": "queued",
  "progress": 0,
  "created_at": 1760000000
}
```

---

## 5.2 POST `/kling/v1/videos/image2video`

### 说明

Kling 图生视频接口。

### 请求字段

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model_name` | string | 否 | 模型名，如 `kling-v2-master` |
| `image` | string | 是 | 图片 URL / Base64 |
| `prompt` | string | 否 | 提示词 |
| `negative_prompt` | string | 否 | 反向提示词 |
| `cfg_scale` | number | 否 | CFG 参数 |
| `mode` | string | 否 | 模式 |
| `camera_control` | object | 否 | 镜头控制 |
| `aspect_ratio` | string | 否 | 纵横比 |
| `duration` | string | 否 | 时长 |
| `callback_url` | string | 否 | 回调地址 |
| `external_task_id` | string | 否 | 外部任务 ID |

### 请求示例

```json
{
  "model_name": "kling-v2-master",
  "image": "https://example.com/input.png",
  "prompt": "让画面中的角色向前走两步并挥手",
  "aspect_ratio": "16:9",
  "duration": "5"
}
```

---

## 5.3 GET `/kling/v1/videos/text2video/{task_id}`

### 说明

查询 Kling 文生视频任务状态。

### 返回

底层走统一任务查询逻辑，通常返回通用任务响应结构。

---

## 5.4 GET `/kling/v1/videos/image2video/{task_id}`

### 说明

查询 Kling 图生视频任务状态。

### 返回

底层走统一任务查询逻辑，通常返回通用任务响应结构。

---

## 6. 即梦官方风格接口

`/jimeng/` 路由会读取官方请求体，并根据 `Action` 参数自动改写为统一视频提交或查询路径。

---

## 6.1 POST `/jimeng/?Action=CVSync2AsyncSubmitTask&Version=2022-08-31`

### 说明

提交即梦异步视频任务。

### 认证

```http
Authorization: Bearer <token>
```

### Query 参数

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `Action` | 是 | 固定为 `CVSync2AsyncSubmitTask` |
| `Version` | 否 | 一般使用 `2022-08-31` |

### 请求体

请求体遵循即梦官方字段，当前中间件会提取：

- `req_key` → 作为统一路由中的 `model`
- `prompt` → 作为统一路由中的 `prompt`
- 整个原始请求 → 放入 `metadata`

若原始请求中存在 `image` 字段，则会按图生视频处理。

### 请求示例

```bash
curl -X POST "http://localhost:3000/jimeng/?Action=CVSync2AsyncSubmitTask&Version=2022-08-31" \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "req_key": "jimeng-video-model",
    "prompt": "一个人在城市天台上眺望远方",
    "image": "https://example.com/input.png"
  }'
```

### 成功响应

底层响应仍是统一视频任务响应，通常返回公开任务 ID。

---

## 6.2 POST `/jimeng/?Action=CVSync2AsyncGetResult&Version=2022-08-31`

### 说明

查询即梦任务结果。

### Query 参数

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `Action` | 是 | 固定为 `CVSync2AsyncGetResult` |
| `Version` | 否 | 一般使用 `2022-08-31` |

### 请求体

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `task_id` | 是 | 公开任务 ID |

### 请求示例

```bash
curl -X POST "http://localhost:3000/jimeng/?Action=CVSync2AsyncGetResult&Version=2022-08-31" \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "task_xxx"
  }'
```

### 说明

中间件会自动把该请求改写为：

```http
GET /v1/video/generations/{task_id}
```

因此返回结构与旧版统一查询接口一致。

---

## 7. 路由总表

| 方法 | 路径 | 说明 | 返回风格 |
| --- | --- | --- | --- |
| `POST` | `/v1/videos` | OpenAI 兼容统一视频提交 | OpenAI 视频对象 |
| `GET` | `/v1/videos/{task_id}` | OpenAI 兼容视频任务查询 | OpenAI 视频对象 |
| `GET` | `/v1/videos/{task_id}/content` | 视频内容拉取 | 视频二进制流 |
| `POST` | `/v1/videos/{video_id}/remix` | 基于原视频任务 remix | OpenAI 视频对象 |
| `POST` | `/v1/video/generations` | 旧版统一视频提交 | 统一任务提交响应 |
| `GET` | `/v1/video/generations/{task_id}` | 旧版统一视频查询 | `TaskResponse<TaskDto>` |
| `POST` | `/kling/v1/videos/text2video` | Kling 文生视频 | 统一任务提交响应 |
| `POST` | `/kling/v1/videos/image2video` | Kling 图生视频 | 统一任务提交响应 |
| `GET` | `/kling/v1/videos/text2video/{task_id}` | Kling 文生视频查询 | 通用任务响应 |
| `GET` | `/kling/v1/videos/image2video/{task_id}` | Kling 图生视频查询 | 通用任务响应 |
| `POST` | `/jimeng/?Action=CVSync2AsyncSubmitTask` | 即梦提交 | 统一任务提交响应 |
| `POST` | `/jimeng/?Action=CVSync2AsyncGetResult` | 即梦查询 | `TaskResponse<TaskDto>` |

---

## 8. 接入建议

### 推荐新接入使用

优先使用：

1. `POST /v1/videos`
2. `GET /v1/videos/{task_id}`
3. `GET /v1/videos/{task_id}/content`

优点：

- 统一 OpenAI 风格；
- 更适合多渠道代理；
- 响应结构更稳定；
- 公开任务 ID 与内容拉取路径更清晰。

### 兼容场景

- 已接入 Kling 官方格式的客户端：继续使用 `/kling/v1/videos/*`
- 已接入即梦官方格式的客户端：继续使用 `/jimeng/`
- 旧系统兼容：保留 `/v1/video/generations`

---

## 9. 备注

1. `/v1/videos` 与 `/v1/video/generations` 底层共享同一套任务提交流程，但返回风格不同；
2. `/v1/videos/{task_id}` 返回 OpenAI 风格对象，而 `/v1/video/generations/{task_id}` 返回通用任务包装对象；
3. `/v1/videos/{task_id}/content` 只在任务成功后可用；
4. 渠道选择依赖 `model`、令牌分组、渠道配置与分发策略；
5. 某些渠道会在提交时预估时长/分辨率倍率，并通过 `X-New-Api-Other-Ratios` 暴露给调用方；
6. 实际可用模型列表取决于当前实例已配置的渠道与模型映射。
