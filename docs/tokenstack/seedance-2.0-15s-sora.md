# Seedance 2.0 视频生成 API（Sora 格式）

> 来源：[TokenStack Seedance 官方接入文档](https://new.tokenstack.cc/docs.html#seedance15s)，整理日期：2026-07-18。选型说明见 [README](./README.md)。

基于 **Seedance 2.0** 的视频生成接口，走 **OpenAI Sora 兼容格式**。一次出 **15 秒**视频，支持**参考图 + 参考视频 + 参考音频**（**各档上限不同，见模型概览**）——分别用于锁定人物 / 商品一致性、参考运镜、参考氛围音乐。任务为异步执行，提交后返回任务 `id`，通过轮询查询结果。

**🚀 对接速查（给你 / 你的程序员）**

- **模型名：**`seedance-2-0-15s-slow` / `seedance-2-0-15s-high` / `seedance-2-0-15s-fast`（按速度 / 画质选，见模型概览）
- **端点：**`POST https://www.tokenstack.cc/v1/videos`（渠道类型 = OpenAI，Sora 格式）
- **计费：**一次性 **15 秒**按次计费，**不支持按秒**
- **参考素材：**图 / 视频 / 音频均传公网 URL，**上限按档不同**（slow=4图/1音/3视频，fast·high=9图/3音，见模型概览的参考上限表），`prompt` 里用 `@Image1` / `@Video1` / `@Audio1` 引用
- **请求体（JSON）：**

```json
{
  "model": "seedance-2-0-15s-slow",
  "prompt": "...",
  "images": ["<公网URL1>", "<公网URL2>"],
  "seconds": "15",
  "size": "1280x720"
}
```

**❗图片必须是公网 http/https URL**——base64 会被拒（报 `image_url.url must be public http/https URL`）。

## 模型概览

| 模型 | 定位 | 时长 | 分辨率 | 计费 |
|----|----|----|----|----|
| `seedance-2-0-15s-fast` | 出片快，**速度优先** | 15 秒 | `1280x720`（720p） | 一次性 15s 按次 |
| `seedance-2-0-15s-slow` | 慢速（约 13 分钟），**性价比** | 15 秒 | `1280x720`（720p） | 一次性 15s 按次 |
| `seedance-2-0-15s-high` | **高画质**，质量优先 | 15 秒 | `1280x720`（720p） | 一次性 15s 按次 |

三个 model **接口格式完全一样**，速度 / 画质不同，**参考素材上限也不同（见下表）**——把示例里的 `model` 换成你要的那个即可。

## 各档参考素材上限（实测）

| 模型                    | 图片参考      | 音频参考      | 视频参考      |
|-------------------------|---------------|---------------|---------------|
| `seedance-2-0-15s-slow` | 最多 **4** 张 | 最多 **1** 个 | 最多 **3** 个 |
| `seedance-2-0-15s-fast` | 最多 **9** 张 | 最多 **3** 个 | —             |
| `seedance-2-0-15s-high` | 最多 **9** 张 | 最多 **3** 个 | —             |

⚠️ 三档上限不一样：**slow** 图少（4 张）、音频 1 个，但**支持视频参考**（3 个）；**fast / high** 图多（9 张）、音频 3 个，**视频参考暂未实测**（表里标 —，以实际为准）。素材超过上限会被截断或报错。**下文参数里写的通用上限，一律以本表为准。**

**⚠️ 已知局限（请记牢）**

- **肖像保护：**大部分情况可以过。
- **图片要公网 URL：**必须是外网可直接访问的 http/https 直链，**不能是本地路径、内网地址、或需要登录 Cookie 的地址**，base64 也会被拒——这是你接入要解决的第一件事（本地图先传到图床 / 对象存储拿到公网 URL 再传）。
- **出片时间因 model 而异：**`-slow` 约 13 分钟、`-fast` 更快、`-high` 画质更好（耗时可能更长）。都务必异步轮询，别同步死等。

## 接口列表

| 接口 | 方法 | 端点 | 说明 |
|----|----|----|----|
| 提交视频任务 | POST | `/v1/videos` | 立即返回任务 `id`，不阻塞等待 |
| 查询任务状态 | GET | `/v1/videos/{video_id}` | 轮询查询进度，完成后返回视频地址 |

**⚠️ 端点提醒：**`/v1/videos` 是 Sora 兼容格式的共用端点（Omni 10s、Grok Imagine 也走这里），**靠 `model` 字段路由**。`model` 填 `seedance-2-0-15s-slow` / `-high` / `-fast` 之一，填错会路由到别的模型。

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
- `X-Idempotency-Key: 任意唯一串`（选填，建议带）——同一分镜重试时保持一致，避免网络重试导致重复提交、重复扣费

### 请求参数

请求体格式：`application/json`

| 参数 | 类型 | 必填 | 说明 |
|----|----|----|----|
| `model` | string | 是 | `seedance-2-0-15s-slow` / `seedance-2-0-15s-high` / `seedance-2-0-15s-fast`，按所需档位选择 |
| `prompt` | string | 是 | 视频提示词，描述想要的画面、镜头、风格 |
| `images` | array | 否 | 参考图，**公网 http/https URL 数组**（上限按档：**slow≤4、fast/high≤9**，见参考上限表），用于保持人物 / 商品一致性。数组里的图**按顺序映射成 `@Image1`、`@Image2`…**，在 `prompt` 里用它们指明每张图的用途。**base64 会被拒**，本地图先转图床 URL |
| `videos` | array | 否 | 参考视频，**公网 URL 数组**（**slow≤3**；fast/high 暂未实测），用于参考运镜 / 动作。按顺序映射成 `@Video1`、`@Video2`、`@Video3`，在 `prompt` 里引用 |
| `audios` | array | 否 | 参考音频，**公网 URL 数组**（上限按档：**slow≤1、fast/high≤3**），用于参考氛围音乐 / 音效。按顺序映射成 `@Audio1`、`@Audio2`、`@Audio3`，在 `prompt` 里引用 |
| `seconds` | string | 否 | 时长固定 **15** 秒，传 `"15"` 即可。**一次性 15s 按次计费，不支持按秒** |
| `size` | string | 否 | 分辨率，填 `1280x720`（720p 横屏） |

**💡 参考素材怎么用（图 / 视频 / 音频）**

- 三类素材各自独立编号，按数组顺序：`images` → `@Image1`、`@Image2`…、`videos` → `@Video1`…、`audios` → `@Audio1`…（**各类上限按档不同，见上方参考上限表**）。
- **在 `prompt` 里用这些编号点名每个素材的用途**——例：「`@Image1` 是主角，脸型发型以它为准；`@Image2` 是场景；参考 `@Video1` 的运镜；氛围音乐参考 `@Audio1`」。点得越清楚，出片越贴合参考。
- 三类素材**全部用公网 http/https URL**，本地文件先转图床 / 对象存储再传；base64 会被拒。

### 请求示例

#### 示例 1：参考图生视频（最常用）

```bash
curl https://www.tokenstack.cc/v1/videos \
  -H "Authorization: Bearer sk-你的TokenStack密钥" \
  -H "Content-Type: application/json" \
  -H "X-Idempotency-Key: scene-001" \
  -d '{
    "model": "seedance-2-0-15s-slow",
    "prompt": "@Image1 是主角，脸型、发型、服装以 @Image1 为准；@Image2 是场景。15 秒横屏漫剧，镜头缓慢推进，电影感光影。",
    "images": ["https://你的图床.com/role.jpg", "https://你的图床.com/scene.jpg"],
    "seconds": "15",
    "size": "1280x720"
  }'
```

#### 示例 2：图 + 视频 + 音频综合参考

```bash
curl https://www.tokenstack.cc/v1/videos \
  -H "Authorization: Bearer sk-你的TokenStack密钥" \
  -H "Content-Type: application/json" \
  -H "X-Idempotency-Key: scene-002" \
  -d '{
    "model": "seedance-2-0-15s-slow",
    "prompt": "@Image1 是主角，脸型发型以它为准；@Image2 是场景。参考 @Video1 的运镜；氛围音乐参考 @Audio1。15 秒横屏漫剧，电影感光影。",
    "images": ["https://你的图床.com/role.jpg", "https://你的图床.com/scene.jpg"],
    "videos": ["https://你的图床.com/camera-ref.mp4"],
    "audios": ["https://你的图床.com/bgm.mp3"],
    "seconds": "15",
    "size": "1280x720"
  }'
```

### 成功响应

```json
{
  "id": "video_xxxxxxxxxxxxx",
  "object": "video",
  "model": "seedance-2-0-15s-slow",
  "status": "queued"
}
```

拿到 `id` 后通过查询接口轮询。`slow` 档通常约 13 分钟，其他档位耗时以实际情况为准。

## 查询任务状态

通过提交时返回的 `id` 轮询查询任务进度。生成可能耗时较长，务必异步轮询，不要同步等待。

### API 端点

```http
GET https://www.tokenstack.cc/v1/videos/{video_id}
```

### 请求示例

```bash
curl https://www.tokenstack.cc/v1/videos/video_xxxxxxxxxxxxx \
  -H "Authorization: Bearer sk-你的TokenStack密钥"
```

### 完成响应

```json
{
  "id": "video_xxxxxxxxxxxxx",
  "object": "video",
  "model": "seedance-2-0-15s-slow",
  "status": "completed",
  "video_url": "https://img.tokenstack.cc/videos/video_xxxxxxxxxxxxx.mp4",
  "completed_at": 1780000300
}
```

### 响应字段

| 字段 | 类型 | 说明 |
|----|----|----|
| `id` | string | 任务 ID（与提交时返回的一致），提交后务必保存用于轮询 |
| `status` | string | 生成中：`queued` / `in_progress`（或 `pending` / `running`）；成功：`completed`（或 `succeeded`）；失败：`failed` |
| `video_url` | string | 成功后的视频地址，可直接在线播放 / 下载。**若返回为透传格式，则取 `resource_list[0].resource_url`**（含 sig/exp，有有效期，及时转存） |
| `fail_reason` | string | `failed` 时的失败原因，可直接展示给用户 |

**注意事项：**

- **异步必轮询：**提交后用 `GET /v1/videos/{video_id}` 轮询到 `completed` 或 `failed`，**建议间隔 5-10 秒**；`slow` 档通常约 13 分钟。
- **图片必须公网 URL：**base64 会被拒（`image_url.url must be public http/https URL`），本地图先转图床 URL。
- **模型名必须使用本系列三档之一：**`/v1/videos` 是共用端点，填错会路由到别的模型。
- **计费固定 15 秒按次**，不按秒；任务失败一般不计费。
- **结果 URL 有有效期：**看到 `completed` 后及时下载保存。
