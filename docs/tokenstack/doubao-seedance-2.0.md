# Doubao Seedance 2.0 视频生成 API

> 来源：[TokenStack Seedance 官方接入文档](https://new.tokenstack.cc/docs.html#doubao-seedance)，整理日期：2026-07-18。选型说明见 [README](./README.md)。

基于火山引擎 **Doubao Seedance 2.0** 的视频生成接口，走 **JSON** 格式。支持**文生视频**、**图生视频**、**首尾帧**、**多素材参考**，以及**全能参考（omni_reference）**——一次性混合图 + 视频 + 音频参考素材综合生成。任务为异步执行，提交后返回任务 `id`，通过轮询查询结果。

**ℹ️ TokenStack 有两个 Seedance 视频系列，按需求选对 `model`：**

|  | 本页 · Doubao Seedance 2.0 | Seedance 2.0 15s → |
|----|----|----|
| **模型名** | `doubao-seedance-2-0-260128` 等 | `seedance-2-0-15s-slow` |
| **请求格式** | **JSON**（首尾帧、全能参考等丰富参数） | **Sora 兼容**（`model`/`prompt`/`images`…，更简单） |
| **时长 / 计费** | 时长可选，**按秒** | 固定 **15 秒、按次**（一口价） |
| **特点** | 首尾帧、多素材、全能参考（图+视频+音频） | 出片慢（~13 分钟）、可过肖像保护 |

## 模型概览

| 模型 | 定位 | 时长范围 | 宽高比 | 计费 |
|----|----|----|----|----|
| `doubao-seedance-2-0-260128` | 标准版，质量优先 | 4–15 秒 | 6 种（见参数表） | 按秒 |
| `doubao-seedance-2-0-fast-260128` | 快速版，速度优先、单价更低 | 4–15 秒 | 6 种（见参数表） | 按秒 |

## 支持的生成模式

| 模式 | 用途 | 怎么触发 |
|----|----|----|
| **文生视频** | 纯文字生成 | `mode=t2v` |
| **图生视频** | 单图首帧 | `mode=i2v` + `image_url` |
| **首尾帧** | 指定首帧和尾帧 | `mode=i2v_first_last` + `image_url` + `end_image_url` |
| **多参考图** | 多张图参考 | `mode=reference_images` + `reference_images` |
| **多素材参考** | 图/视频/音频混合参考 | `mode=reference_material` + `content` 数组 |
| **全能参考 ⭐** | 一次性综合参考图+视频+音频 | `function_mode=omni_reference` + `content` 数组 |

## 接口列表

| 接口 | 方法 | 端点 | 说明 |
|----|----|----|----|
| 提交视频任务 | POST | `/v1/videos` | 立即返回任务 `id` |
| 查询任务状态 | GET | `/v1/videos/{video_id}` | 轮询查询任务进度，完成后返回视频地址 |

**⚠️ 端点提醒：**`/v1/videos` 是 Sora 兼容格式的共用端点（Omni 10s、Grok、Seedance 2.0 15s 也走这里），**靠 `model` 字段路由**。本系列 `model` 填 Doubao Seedance 2.0 系列名（如 `doubao-seedance-2-0-260128`，见下方），别和另一套 `seedance-2-0-15s-slow` 搞混。

**Base URL：**`https://www.tokenstack.cc`

**鉴权方式：**`Authorization: Bearer sk-你的TokenStack密钥`，标准 OpenAI 兼容格式。

**请求格式：**`application/json`。

## 提交视频任务

提交后立即返回任务 `id`，**不会阻塞等待**视频生成完成。

### API 端点

```http
POST https://www.tokenstack.cc/v1/videos
```

### 请求头

- `Authorization: Bearer sk-你的TokenStack密钥`（必填）
- `Content-Type: application/json`（必填）

### 请求参数

请求体格式：`application/json`

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|----|----|----|----|----|
| `model` | string | 是 | — | `doubao-seedance-2-0-260128`（标准）/ `doubao-seedance-2-0-fast-260128`（快速） |
| `prompt` | string | 是 | — | 视频提示词 |
| `mode` | string | 否 | `t2v` | `t2v` / `i2v` / `i2v_first_last` / `reference_images` / `reference_material` |
| `function_mode` | string | 否 | — | **全能参考**：填 `omni_reference` 时，模型会综合 `content` 里的图 / 视频 / 音频素材一起参考生成 |
| `duration` | integer | 否 | `5` | 视频时长（秒），范围 **4–15**。也可用 `seconds` 别名 |
| `aspect_ratio` | string | 否 | `adaptive` | 宽高比：`adaptive` / `21:9` / `16:9` / `4:3` / `1:1` / `3:4` / `9:16`。也可用 `ratio` 别名 |
| `image_url` | string | 否 | — | 单图首帧 URL（i2v） |
| `end_image_url` | string | 否 | — | 尾帧 URL（i2v_first_last）。也可用 `last_image_url` |
| `reference_images` | string\[\] | 否 | — | 多参考图 URL 数组（reference_images 模式）。也可用 `image_urls` |
| `content` | array | 否 | — | 多素材数组（reference_material / 全能参考用），元素见下方说明 |
| `generate_audio` | boolean | 否 | — | 是否生成音频 |
| `watermark` | boolean | 否 | — | 是否加水印 |

所有参考素材都用**公网可访问的 URL**，本地文件先用图片上传 API 转 URL。

### `content` 数组元素结构

| 字段 | 说明 |
|----|----|
| `type` | `text` / `image_url` / `video_url` / `audio_url` |
| `text` / `image_url` / `video_url` / `audio_url` | 对应内容；媒体用对象形式 `{ "url": "https://..." }` |
| `role` | 素材用途标记：`reference_image` / `reference_video` / `reference_audio` |
| `name` | 素材编号，可在 `prompt` 里点名引用（如「参考素材 1 的人物」） |

### 请求示例

#### 示例 1：文生视频（t2v）

```bash
curl https://www.tokenstack.cc/v1/videos \
  -H "Authorization: Bearer sk-你的TokenStack密钥" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "doubao-seedance-2-0-260128",
    "mode": "t2v",
    "prompt": "城市夜景延时摄影，车流光轨，霓虹闪烁",
    "duration": 5,
    "aspect_ratio": "16:9"
  }'
```

#### 示例 2：首尾帧（i2v_first_last）

```bash
curl https://www.tokenstack.cc/v1/videos \
  -H "Authorization: Bearer sk-你的TokenStack密钥" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "doubao-seedance-2-0-fast-260128",
    "mode": "i2v_first_last",
    "prompt": "从白天自然过渡到夜晚，镜头位置不变",
    "image_url": "https://example.com/day.jpg",
    "end_image_url": "https://example.com/night.jpg",
    "duration": 5,
    "aspect_ratio": "16:9"
  }'
```

#### 示例 3：全能参考（function_mode=omni_reference，图+视频+音频混合）

```bash
curl https://www.tokenstack.cc/v1/videos \
  -H "Authorization: Bearer sk-你的TokenStack密钥" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "doubao-seedance-2-0-260128",
    "function_mode": "omni_reference",
    "prompt": "参考素材1的人物形象、素材2的运镜、素材3的背景音乐，生成产品广告",
    "content": [
      { "type": "text", "text": "保持人物形象一致，运镜参考视频，配上参考音频的节奏" },
      { "type": "image_url", "image_url": { "url": "https://example.com/person.png" }, "role": "reference_image", "name": "1" },
      { "type": "video_url", "video_url": { "url": "https://example.com/camera.mp4" }, "role": "reference_video", "name": "2" },
      { "type": "audio_url", "audio_url": { "url": "https://example.com/music.mp3" }, "role": "reference_audio", "name": "3" }
    ],
    "duration": 8,
    "aspect_ratio": "16:9"
  }'
```

### 提交响应

```json
{
  "id": "video_dbsd123",
  "object": "video",
  "model": "doubao-seedance-2-0-260128",
  "status": "queued",
  "created_at": 1781234567
}
```

## 查询任务状态

通过提交时返回的 `id` 轮询查询任务进度。

### API 端点

```http
GET https://www.tokenstack.cc/v1/videos/{video_id}
```

### 请求示例

```bash
curl https://www.tokenstack.cc/v1/videos/video_dbsd123 \
  -H "Authorization: Bearer sk-你的TokenStack密钥"
```

### 完成响应

```json
{
  "id": "video_dbsd123",
  "object": "video",
  "model": "doubao-seedance-2-0-260128",
  "status": "completed",
  "video_url": "https://img.tokenstack.cc/videos/video_dbsd123.mp4",
  "completed_at": 1781234890
}
```

视频地址在 `video_url`（个别情况字段名是 `url`），两者取其一即可。

### 任务状态

| 状态          | 含义                                |
|---------------|-------------------------------------|
| `queued`      | 等待队列中                          |
| `in_progress` | 生成中                              |
| `completed`   | 生成完成，可从 `video_url` 下载视频 |
| `failed`      | 生成失败，`error` 字段含失败原因    |

**注意事项：**

- **异步任务必须轮询：**提交后立即返回 `id`，用 `GET /v1/videos/{video_id}` 轮询直到 `completed` 或 `failed`，建议间隔 5–15 秒。
- **全能参考用 `content`：**填 `function_mode=omni_reference` 时，把图 / 视频 / 音频素材放进 `content` 数组，每项用 `role` 标用途、`name` 起编号，`prompt` 里可点名引用。
- **JSON 请求体：**本系列使用 JSON + 公网 URL，请勿混用其他 Seedance 系列的请求模板。
- **素材用 URL：**需公网可访问，本地文件先用图片上传 API 转 URL。
- **结果 URL 有有效期：**看到 `completed` 后及时下载。
