# p-seedance-2.0 视频生成 API 接入文档

本文档用于客户通过我方平台接入 `p-seedance-2.0` 视频生成模型。

## 基础信息

- API Base URL：`http://154.40.44.244:3000`
- 鉴权方式：`Authorization: Bearer YOUR_API_KEY`
- 模型名称：`p-seedance-2.0`
- 计费方式：`3.5 元/条`
- 创建任务接口：`POST /v1/videos`
- 查询任务接口：`GET /v1/videos/{task_id}`

## 素材能力

模型支持参考图片、参考音频、参考视频：

| 素材类型 | 单类上限 | 推荐格式 |
| --- | ---: | --- |
| 参考图片 | 最多 9 张 | JPG、PNG、WEBP |
| 参考音频 | 最多 3 段 | MP3，单段建议 2-15 秒 |
| 参考视频 | 最多 3 段 | MP4 |

重要限制：

- 单次请求三类参考素材总数不能超过 12 个。
- 支持能力是“最多 9 图、3 音频、3 视频”，但不能三类同时拉满。
- 合法示例：`9图 + 3音频 = 12`、`9图 + 3视频 = 12`、`6图 + 3音频 + 3视频 = 12`。
- 非法示例：`9图 + 3音频 + 3视频 = 15`，超过总素材上限。

## 创建视频任务

请求地址：

```http
POST http://154.40.44.244:3000/v1/videos
Authorization: Bearer YOUR_API_KEY
Content-Type: application/json
```

### 请求参数

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 固定传 `p-seedance-2.0` |
| `prompt` | string | 是 | 视频生成提示词，可在提示词中描述参考图、参考音频、参考视频的对应关系 |
| `seconds` | string / number | 否 | 视频时长，例如 `5`、`10`、`15` |
| `duration` | string / number | 否 | 与 `seconds` 等价，二选一即可 |
| `aspect_ratio` | string | 否 | 画面比例，支持 `16:9`、`9:16`、`1:1` |
| `reference_image_urls` | string[] | 否 | 参考图片 URL 数组，最多 9 张 |
| `reference_audios` | string[] | 否 | 参考音频 URL 数组，最多 3 段 |
| `reference_videos` | string[] | 否 | 参考视频 URL 数组，最多 3 段 |

建议客户只使用上表里的三个素材字段，不要把同一批素材重复放到 `images`、`image_urls`、`audios`、`audio_urls` 等兼容字段里。

## 请求示例：9 图 + 3 音频

```bash
curl -X POST "http://154.40.44.244:3000/v1/videos" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "p-seedance-2.0",
    "prompt": "参考图1-9作为画面风格和场景顺序，参考音频1-3作为节奏和音效参考，生成一段电影感视频。无字幕，无水印，无文字。",
    "seconds": 5,
    "aspect_ratio": "16:9",
    "reference_image_urls": [
      "https://example.com/image1.jpg",
      "https://example.com/image2.jpg",
      "https://example.com/image3.jpg",
      "https://example.com/image4.jpg",
      "https://example.com/image5.jpg",
      "https://example.com/image6.jpg",
      "https://example.com/image7.jpg",
      "https://example.com/image8.jpg",
      "https://example.com/image9.jpg"
    ],
    "reference_audios": [
      "https://example.com/audio1.mp3",
      "https://example.com/audio2.mp3",
      "https://example.com/audio3.mp3"
    ]
  }'
```

成功提交后会返回任务 ID：

```json
{
  "id": "task_xxx",
  "task_id": "task_xxx",
  "object": "video.generation",
  "model": "p-seedance-2.0",
  "status": "queued",
  "progress": 0
}
```

## 请求示例：6 图 + 3 音频 + 3 视频

```json
{
  "model": "p-seedance-2.0",
  "prompt": "参考图1-6作为人物和场景参考，参考音频1-3作为声音节奏参考，参考视频1-3作为运动方式参考，生成电影感视频。无字幕，无水印。",
  "seconds": 5,
  "aspect_ratio": "16:9",
  "reference_image_urls": [
    "https://example.com/image1.jpg",
    "https://example.com/image2.jpg",
    "https://example.com/image3.jpg",
    "https://example.com/image4.jpg",
    "https://example.com/image5.jpg",
    "https://example.com/image6.jpg"
  ],
  "reference_audios": [
    "https://example.com/audio1.mp3",
    "https://example.com/audio2.mp3",
    "https://example.com/audio3.mp3"
  ],
  "reference_videos": [
    "https://example.com/video1.mp4",
    "https://example.com/video2.mp4",
    "https://example.com/video3.mp4"
  ]
}
```

## 查询任务结果

请求地址：

```http
GET http://154.40.44.244:3000/v1/videos/{task_id}
Authorization: Bearer YOUR_API_KEY
```

示例：

```bash
curl "http://154.40.44.244:3000/v1/videos/task_xxx" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

处理中返回示例：

```json
{
  "id": "task_xxx",
  "task_id": "task_xxx",
  "object": "video.generation",
  "model": "p-seedance-2.0",
  "status": "in_progress",
  "progress": 50
}
```

成功返回示例：

```json
{
  "id": "task_xxx",
  "task_id": "task_xxx",
  "object": "video.generation",
  "model": "p-seedance-2.0",
  "status": "completed",
  "progress": 100,
  "url": "https://example.com/result.mp4",
  "video_url": "https://example.com/result.mp4",
  "result_url": "https://example.com/result.mp4"
}
```

失败返回示例：

```json
{
  "id": "task_xxx",
  "task_id": "task_xxx",
  "object": "video.generation",
  "model": "p-seedance-2.0",
  "status": "failed",
  "progress": 100,
  "error": {
    "code": "upstream_failed",
    "message": "upstream video generation failed; please retry later"
  }
}
```

## 轮询建议

- 提交任务后，每 10-20 秒查询一次任务状态。
- 当 `status` 为 `queued`、`in_progress`、`processing` 时继续等待。
- 当 `status` 为 `completed` 时，从 `video_url`、`result_url` 或 `url` 字段获取视频地址。
- 当 `status` 为 `failed` 时，本次任务失败，可重新提交或联系平台管理员。

## 常见问题

### 1. 为什么 9 图 + 3 音频 + 3 视频不行？

因为三类素材合计是 15 个，超过单次请求总素材上限 12 个。

### 2. 可以只传图片或只传音频吗？

可以。参考素材都是可选字段，只要提示词完整即可提交任务。

### 3. 音频需要注意什么？

建议使用 MP3，单段音频建议控制在 2-15 秒。过短或过长可能被上游拒绝。

### 4. 素材 URL 有什么要求？

素材 URL 必须是公网可访问的直链，平台需要能通过 URL 下载到图片、音频或视频文件。

