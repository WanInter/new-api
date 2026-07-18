# Bytefor 视频生成 API 文档

本文档根据 Bytefor 视频生成服务的接口资料整理，覆盖统一视频生成接口、任务查询、余额与费用预估，以及火山方舟兼容接口。

- API Base URL：`https://k7q9m2x4a8z3.bytefor.com`
- 当前 New API 适配器使用：`Authorization: Bearer <API_KEY>`

> 说明：原始资料未明确通用错误响应格式。文中的任务 ID、时间戳、费用和资源 URL 均为示例值。

## 1. 接口概览

| 场景 | 方法 | 路径 |
| --- | --- | --- |
| 提交视频生成任务 | `POST` | `/v1/videos/generations` |
| 查询视频生成结果 | `GET` | `/v1/videos/generations/{taskId}` |
| 火山方舟兼容：创建任务 | `POST` | `/api/v3/contents/generations/tasks` |
| 火山方舟兼容：查询任务 | `GET` | `/api/v3/contents/generations/tasks/{taskId}` |
| OpenAI 兼容入口 | `POST` | `/v1/images/generations` |
| 查询账号余额 | `GET` | `/api/v1/balance` |
| 测算生成费用 | `POST` | `/api/v1/estimate` |

视频生成采用异步任务模式：先提交任务并保存响应中的任务 ID，再通过对应的查询接口轮询任务状态，直至任务成功或失败。

> 原始资料只列出了 OpenAI 兼容入口 `/v1/images/generations`，未提供该接口的请求参数和响应结构。

## 2. 可用模型

| Model ID | 说明 | 分辨率 | 队列 |
| --- | --- | --- | --- |
| `bytefor-2.0-fast-real-priority` | 支持真人 | `720P` | 优先 |
| `bytefor-2.0-fast` | 非真人 | `720P` | 优先 |
| `bytefor-2.0` | 非真人 | `720P` / `1080P` / `4K` | 优先 |
| `bytefor-2.0-pro` | 非真人 | `720P` / `1080P` | 优先 |
| `bytefor-2.0-real-priority` | 支持真人 | `720P` / `1080P` / `4K` | 优先 |

可用模型以服务端实时返回或最新配置为准。

### 2.1 New API 渠道配置

Bytefor 复用现有 `DoubaoVideo` 渠道类型，不需要新增渠道类型：

| 配置项 | 值 |
| --- | --- |
| 渠道类型 | `DoubaoVideo` |
| Base URL | `https://k7q9m2x4a8z3.bytefor.com` |
| 密钥 | Bytefor API Key |
| 模型 | 按上表选择一个或多个 `bytefor-*` 模型 |

Bytefor 模型会自动使用原生 `/v1/videos/generations` 提交和查询接口；同一渠道适配器中的 Doubao 模型仍使用火山方舟兼容接口。

> 启用模型前还需要在 New API 后台配置模型价格或计费规则。当前适配器不会直接使用 Bytefor 响应里的 `cost` 作为用户扣费金额。

## 3. 统一视频生成接口

### 3.1 提交视频生成任务

```http
POST /v1/videos/generations
Content-Type: application/json
```

#### 请求参数

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 模型 ID，例如 `bytefor-2.0-real-priority` |
| `prompt` | string | 是 | 视频提示词 |
| `size` | string | 否 | 画面比例：`16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9` |
| `resolution` | string | 否 | 分辨率：`720P`、`1080P`、`4K`；实际可用值取决于模型 |
| `duration` | string | 否 | 视频时长，支持 `4s` 至 `15s`，例如 `4s`、`8s`、`15s` |
| `images` | string[] | 否 | 统一素材数组，支持图片、音频、视频 URL 或 `face:` 人脸素材编码 |

#### 素材引用规则

所有素材按使用顺序放入 `images` 数组，不需要拆分为图片、音频或视频字段。提示词通过素材类型和序号引用数组元素：

| 提示词引用 | 数组位置 |
| --- | --- |
| `@图片1` | `images[0]` |
| `@图片2` | `images[1]` |
| `@音频1` | 下一个音频 URL 在 `images` 中的位置 |
| `@视频1` | 下一个视频 URL 在 `images` 中的位置 |

例如，当数组依次包含两张图片和一段音频时，`@图片1`、`@图片2`、`@音频1` 分别指向 `images[0]`、`images[1]`、`images[2]`。提示词应使用“第一张图”“第二张图”“第一段音频”等描述说明素材的作用。

#### 无素材请求示例

```json
{
  "model": "bytefor-2.0-fast-real-priority",
  "prompt": "A cinematic realistic video of a person walking",
  "size": "16:9",
  "resolution": "720P",
  "duration": "4s",
  "images": []
}
```

#### 素材引用请求示例

```json
{
  "model": "bytefor-2.0-real-priority",
  "prompt": "参考@图片1中的人物形象，使用@图片2作为场景背景，人物跟随@音频1的节奏自然说话和动作，生成电影感真实视频。",
  "size": "16:9",
  "resolution": "720P",
  "duration": "4s",
  "images": [
    "https://cdn.example.com/person.png",
    "https://cdn.example.com/background.png",
    "https://cdn.example.com/voice.mp3"
  ]
}
```

#### 提交响应示例

```json
{
  "created": 1781690400,
  "data": [
    {
      "url": "",
      "revised_prompt": "A cinematic realistic video..."
    }
  ],
  "model": "bytefor-2.0-real-priority",
  "task_id": "TASK-xxxx",
  "status": "pending",
  "cost": 2.5
}
```

提交后保存 `task_id`，用于查询任务结果。任务刚提交时，`data[0].url` 可以为空。

### 3.2 查询任务结果

```http
GET /v1/videos/generations/{taskId}
```

路径参数：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `taskId` | string | 是 | 提交接口返回的 `task_id` |

完成响应示例：

```json
{
  "created": 1781690400,
  "data": [
    {
      "url": "https://cdn.example.com/video.mp4",
      "revised_prompt": "A cinematic realistic video..."
    }
  ],
  "model": "bytefor-2.0-real-priority",
  "task_id": "TASK-xxxx",
  "status": "completed",
  "progress": 100
}
```

任务完成后，从 `data[0].url` 获取视频地址。

## 4. 余额与费用测算

判断账号能否提交任务时，应以可用余额 `available` 为准：

```text
available = balance - frozen
```

推荐在提交生成任务前调用 `/api/v1/estimate`。该接口同时返回本次费用、可用余额以及余额是否充足。

### 4.1 查询账号余额

```http
GET /api/v1/balance
```

响应示例：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "balance": 100,
    "frozen": 5,
    "available": 95,
    "totalRecharged": 200,
    "totalConsumed": 105
  }
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `balance` | number | 当前账户余额 |
| `frozen` | number | 已冻结金额 |
| `available` | number | 可用余额，即 `balance - frozen` |
| `totalRecharged` | number | 累计充值金额 |
| `totalConsumed` | number | 累计消费金额 |

### 4.2 测算本次生成费用

```http
POST /api/v1/estimate
Content-Type: application/json
```

#### 请求参数

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 模型 ID，例如 `bytefor-2.0-real-priority` |
| `size` | string | 否 | 画面比例：`16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9` |
| `resolution` | string | 否 | 分辨率：`720P`、`1080P`、`4K` |
| `duration` | string | 否 | 视频时长，支持 `4s` 至 `15s` |

请求示例：

```json
{
  "model": "bytefor-2.0-fast-real-priority",
  "size": "16:9",
  "resolution": "720P",
  "duration": "4s"
}
```

响应示例：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "cost": 12,
    "discount": 0.85,
    "available": 95,
    "sufficient": true
  }
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `cost` | number | 本次生成任务的预计费用 |
| `discount` | number | 当前折扣系数 |
| `available` | number | 当前可用余额 |
| `sufficient` | boolean | 可用余额是否足以支付本次生成 |

## 5. 火山方舟兼容接口

火山方舟兼容接口同样采用异步任务模式：创建接口返回 `id`，查询接口使用该 `id` 轮询；任务成功后，从响应的 `video_url` 获取视频地址。

任务状态流转：

```text
queued -> running -> succeeded
                    -> failed
```

任务失败时，响应状态为 `failed`，并返回错误信息。

### 5.1 创建视频生成任务

```http
POST /api/v3/contents/generations/tasks
Content-Type: application/json
```

#### 请求参数

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 后台配置的 Model ID |
| `content` | object[] | 是 | 输入内容数组，支持 `text`、`image_url`、`video_url`、`audio_url`、`draft_task` |
| `resolution` | string | 否 | `480p`、`720p`、`1080p`、`4k` |
| `ratio` | string | 否 | `16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9`、`adaptive` |
| `duration` | integer | 否 | 生成时长，支持 4 至 15 秒；默认值为 `5`，传 `-1` 兼容默认时长 |
| `priority` | integer | 否 | `0` 至 `9`，数值越大优先级越高 |
| `callback_url` | string | 否 | 回调地址；未配置时通过查询接口轮询结果 |

截图中的请求示例还使用了 `generate_audio` 布尔字段，用于控制是否生成音频；该字段未出现在参数表中，接入前建议确认服务端支持范围。

#### `content` 内容格式

`content` 是对象数组，不同素材使用不同 `type`：

| `type` | 数据字段 | 常见 `role` | 用途 |
| --- | --- | --- | --- |
| `text` | `text` | 无 | 文本提示词 |
| `image_url` | `image_url.url` | `first_frame`、`last_frame`、`reference_image` | 首帧、尾帧或参考图 |
| `video_url` | `video_url.url` | `reference_video` | 参考视频 |
| `audio_url` | `audio_url.url` | `reference_audio` | 参考音频 |
| `draft_task` | 资料未展示 | 资料未展示 | 草稿任务 |

音频不能单独传入，必须同时至少提供一张图片或一个视频。

常见组合：

| 场景 | 内容类型或角色 |
| --- | --- |
| 文生视频 | `text` |
| 首帧图生视频 | `image_url` + `first_frame` |
| 首尾帧图生视频 | `first_frame` + `last_frame` |
| 参考图 | `reference_image` |
| 参考视频 | `reference_video` |
| 参考音频 | `reference_audio` |

#### 完整请求示例

```json
{
  "model": "bytefor-2.0-fast-real-priority",
  "content": [
    {
      "type": "text",
      "text": "A cinematic realistic video of a person walking"
    },
    {
      "type": "image_url",
      "role": "reference_image",
      "image_url": {
        "url": "https://example.com/reference.png"
      }
    }
  ],
  "resolution": "720p",
  "ratio": "16:9",
  "duration": 5,
  "generate_audio": false,
  "priority": 0
}
```

#### 创建响应示例

```json
{
  "id": "TASK-xxxx",
  "model": "Seedance 2.0 Real Priority",
  "status": "queued",
  "created_at": 1781690400,
  "cost": 2.5
}
```

保存响应中的 `id`，用于查询任务状态。

### 5.2 场景示例

#### 示例 1：文生视频

```json
{
  "model": "bytefor-2.0-fast",
  "content": [
    {
      "type": "text",
      "text": "A cinematic shot of a futuristic city street at night, slow camera movement, realistic lighting."
    }
  ],
  "resolution": "720p",
  "ratio": "16:9",
  "duration": 5,
  "generate_audio": false
}
```

#### 示例 2：首帧图片生成视频

```json
{
  "model": "bytefor-2.0-real-priority",
  "content": [
    {
      "type": "text",
      "text": "Use the person in the first frame. The person walks forward naturally and smiles to the camera."
    },
    {
      "type": "image_url",
      "role": "first_frame",
      "image_url": {
        "url": "https://cdn.example.com/first-frame.png"
      }
    }
  ],
  "resolution": "720p",
  "ratio": "16:9",
  "duration": 5
}
```

#### 示例 3：首尾帧图片生成视频

```json
{
  "model": "bytefor-2.0-real-priority",
  "content": [
    {
      "type": "text",
      "text": "Generate a smooth transition from the first frame to the last frame, cinematic realistic style."
    },
    {
      "type": "image_url",
      "role": "first_frame",
      "image_url": {
        "url": "https://cdn.example.com/start.png"
      }
    },
    {
      "type": "image_url",
      "role": "last_frame",
      "image_url": {
        "url": "https://cdn.example.com/end.png"
      }
    }
  ],
  "resolution": "720p",
  "ratio": "16:9",
  "duration": 5
}
```

#### 示例 4：参考图和参考音频

```json
{
  "model": "bytefor-2.0-real-priority",
  "content": [
    {
      "type": "text",
      "text": "Use the reference image for character appearance and the audio as rhythm reference."
    },
    {
      "type": "image_url",
      "role": "reference_image",
      "image_url": {
        "url": "https://cdn.example.com/reference.png"
      }
    },
    {
      "type": "audio_url",
      "role": "reference_audio",
      "audio_url": {
        "url": "https://cdn.example.com/voice.mp3"
      }
    }
  ],
  "resolution": "720p",
  "ratio": "9:16",
  "duration": 8,
  "generate_audio": true,
  "priority": 0
}
```

#### 示例 5：参考视频和参考音频

```json
{
  "model": "bytefor-2.0-real-priority",
  "content": [
    {
      "type": "text",
      "text": "Use the reference video for motion style and the audio as rhythm reference. Generate a realistic short video."
    },
    {
      "type": "video_url",
      "role": "reference_video",
      "video_url": {
        "url": "https://cdn.example.com/reference.mp4"
      }
    },
    {
      "type": "audio_url",
      "role": "reference_audio",
      "audio_url": {
        "url": "https://cdn.example.com/voice.mp3"
      }
    }
  ],
  "resolution": "720p",
  "ratio": "16:9",
  "duration": 8,
  "generate_audio": true
}
```

### 5.3 查询视频生成任务

```http
GET /api/v3/contents/generations/tasks/{taskId}
```

路径参数：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `taskId` | string | 是 | 创建接口返回的任务 ID |

成功响应示例：

```json
{
  "id": "TASK-xxxx",
  "model": "Seedance 2.0 Real Priority",
  "status": "succeeded",
  "created_at": 1781690400,
  "updated_at": 1781690470,
  "video_url": "https://cdn.example.com/video.mp4",
  "content": {
    "video_url": {
      "url": "https://cdn.example.com/video.mp4"
    }
  }
}
```

任务成功后，可从顶层 `video_url` 或 `content.video_url.url` 获取视频地址。

## 6. 接入检查清单

正式接入前，建议向服务提供方确认以下信息：

1. 生产密钥是否固定使用 Bearer 鉴权。
2. 通用错误响应结构及 HTTP 状态码约定。
3. `POST /v1/images/generations` 的完整请求和响应协议。
4. `callback_url` 的回调请求结构、签名方式和重试策略。
5. `face:` 人脸素材编码的生成方式和格式限制。
6. 各模型可用的时长、比例、分辨率和素材组合限制。
7. `generate_audio` 与 `draft_task` 的完整字段说明。
