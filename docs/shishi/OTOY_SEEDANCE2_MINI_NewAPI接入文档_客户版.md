# OTOY Seedance v2 Mini Reference 视频模型接入文档

本文档适用于通过 NewAPI 中转调用 `mini1.8` 渠道下的新视频模型。

## 1. 基础信息

| 项目 | 内容 |
| --- | --- |
| 平台 Base URL | `http://154.40.44.244:3000` |
| 鉴权方式 | `Authorization: Bearer YOUR_API_KEY` |
| 提交任务 | `POST /v1/video/generations` |
| 查询任务 | `GET /v1/video/generations/{task_id}` |
| OpenAI 视频兼容提交 | `POST /v1/videos` |
| OpenAI 视频兼容查询 | `GET /v1/videos/{task_id}` |
| 模型 ID | `otoy-image-to-video-seedance-2-0-mini-reference-to-video` |
| 渠道名称 | `mini1.8` |
| 上游模型类型 | 图生视频 / 参考图生视频 |

> 注意：`mini1.8` 是后台渠道名称。当前客户请求体里的 `model` 应传上面的完整模型 ID。

## 2. 请求头

```http
Authorization: Bearer YOUR_API_KEY
Content-Type: application/json
```

## 3. 提交视频任务

### 接口

```http
POST http://154.40.44.244:3000/v1/video/generations
```

### 三张参考图生成 15 秒视频示例

```json
{
  "model": "otoy-image-to-video-seedance-2-0-mini-reference-to-video",
  "type": "image-to-video",
  "prompt": "参考图对应关系：@Image1、@Image2、@Image3 分别是三位成年真人参考演员。生成一段15秒电影感动作戏：三位成年演员在空旷训练馆中进行武术对打排练，动作包括闪避、格挡、短促交手、后撤重整和最后定格。画面真实电影质感，镜头稳定，节奏清楚，动作有力量但保持安全距离，明显是编排好的动作戏和训练对打。不要血腥，不要受伤特写，不要真实伤害，不要残忍暴力，不要武器刺穿，不要断肢，不要字幕、logo、水印。",
  "image_urls": [
    "https://example.com/ref-1.jpg",
    "https://example.com/ref-2.jpg",
    "https://example.com/ref-3.jpg"
  ],
  "resolution": "720p",
  "duration": "15",
  "aspect_ratio": "16:9",
  "generate_audio": false
}
```

### 成功提交响应

```json
{
  "id": "task_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "task_id": "task_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "object": "video",
  "model": "otoy-image-to-video-seedance-2-0-mini-reference-to-video",
  "status": "queued",
  "progress": 0,
  "created_at": 1782991153
}
```

返回的 `id` / `task_id` 是平台任务 ID，后续用它查询结果。

## 4. 查询任务

### 推荐查询接口

```http
GET http://154.40.44.244:3000/v1/video/generations/{task_id}
```

### 查询中响应

```json
{
  "code": "success",
  "message": "",
  "data": {
    "task_id": "task_xxx",
    "status": "IN_PROGRESS",
    "progress": "50%"
  }
}
```

### 完成响应

```json
{
  "code": "success",
  "message": "",
  "data": {
    "task_id": "task_xxx",
    "status": "SUCCESS",
    "progress": "100%",
    "result_url": "https://aigc.cglol.com/media/2026-07-02/result.mp4",
    "data": {
      "status": "completed",
      "url": "https://aigc.cglol.com/media/2026-07-02/result.mp4",
      "video_url": "https://aigc.cglol.com/media/2026-07-02/result.mp4",
      "videos": [
        "https://aigc.cglol.com/media/2026-07-02/result.mp4"
      ],
      "outputs": [
        {
          "type": "video",
          "format": "mp4",
          "url": "https://aigc.cglol.com/media/2026-07-02/result.mp4",
          "downloadUrl": "https://aigc.cglol.com/media/2026-07-02/result.mp4"
        }
      ]
    }
  }
}
```

客户侧优先读取：

1. `data.result_url`
2. `data.data.video_url`
3. `data.data.url`
4. `data.data.outputs[0].url`
5. `data.data.videos[0]`

## 5. OpenAI 视频兼容接口

也可以使用：

```http
POST http://154.40.44.244:3000/v1/videos
GET  http://154.40.44.244:3000/v1/videos/{task_id}
```

兼容请求示例：

```json
{
  "model": "otoy-image-to-video-seedance-2-0-mini-reference-to-video",
  "prompt": "三位成年演员在训练馆中进行电影武打排练，动作清晰，无血腥。",
  "image_urls": [
    "https://example.com/ref-1.jpg",
    "https://example.com/ref-2.jpg",
    "https://example.com/ref-3.jpg"
  ],
  "seconds": "15",
  "size": "1280x720"
}
```

兼容字段说明：

| 兼容字段 | 自动转换 |
| --- | --- |
| `seconds` | 转为上游 `duration` |
| `size: "1280x720"` | 推导为 `aspect_ratio: "16:9"`、`resolution: "720p"` |
| `size: "720x1280"` | 推导为 `aspect_ratio: "9:16"`、`resolution: "720p"` |

更推荐直接传 `duration`、`aspect_ratio`、`resolution`，避免歧义。

## 6. 参数说明

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 固定传 `otoy-image-to-video-seedance-2-0-mini-reference-to-video` |
| `prompt` | string | 是 | 视频提示词。可用 `@Image1`、`@Image2` 等引用参考图 |
| `type` | string | 否 | 建议传 `image-to-video` |
| `image_urls` | array<string> | 否 | 参考图片 URL，最多 9 张 |
| `video_urls` | array<string> | 否 | 参考视频 URL，最多 3 个 |
| `audio_urls` | array<string> | 否 | 参考音频 URL，最多 3 个 |
| `resolution` | string | 否 | `480p` 或 `720p`，建议 `720p` |
| `duration` | string/int | 否 | `4` 到 `15` 秒，或 `auto` |
| `aspect_ratio` | string | 否 | `16:9`、`9:16`、`1:1`、`21:9` 或 `auto` |
| `generate_audio` | boolean | 否 | 是否生成同步音频，默认可按业务传 `false` 或 `true` |
| `end_user_id` | string | 否 | 终端用户 ID，可用于追踪 |

## 7. 素材限制

### 图片参考

- 支持格式：`JPEG`、`PNG`、`WebP`
- 单张最大：`30MB`
- 最多：`9` 张
- Prompt 引用方式：`@Image1`、`@Image2`、`@Image3`

### 视频参考

- 支持格式：`MP4`、`MOV`
- 最多：`3` 个
- 总时长：`2-15 秒`
- 总大小：小于 `50MB`
- Prompt 引用方式：`@Video1`、`@Video2`

### 音频参考

- 支持格式：`MP3`、`WAV`
- 最多：`3` 个
- 总时长：不超过 `15 秒`
- 单文件最大：`15MB`
- 如果传 `audio_urls`，必须同时至少传一个参考图或参考视频
- Prompt 引用方式：`@Audio1`、`@Audio2`

### 总素材数量

图片、视频、音频加起来建议不要超过 `12` 个。

## 8. 图片 URL 与压缩说明

通过本平台调用时：

- 不会压缩用户图片。
- 不会改尺寸、改格式或二次转码。
- 平台适配器会把 `image_urls` 原图下载后上传到上游素材服务，解决上游无法访问部分外链的问题。

因此客户传入的图片 URL 至少要满足：

- NewAPI 服务器可以直接下载。
- URL 不需要登录、Cookie、Referer 防盗链或临时鉴权。
- 不要使用马上过期的临时链接。

如果图片 URL 无法被服务器访问，可能报：

```json
{
  "message": "We couldn't access the file from the provided link. Please ensure the URL is correct and the file is publicly accessible, then try again."
}
```

## 9. 轮询建议

视频是异步任务，建议：

- 提交成功后等待 `5-10` 秒再开始查询。
- 查询间隔建议 `5-10` 秒。
- 不建议 1 秒内高频查询。

上游偶尔会返回：

```json
{
  "message": "状态查询过于频繁。请等待 1 秒后再试。",
  "retry_after": 1
}
```

平台适配器已经将该情况处理为 `processing`，不会再误判为失败。

## 10. cURL 示例

### 提交任务

```bash
curl -X POST "http://154.40.44.244:3000/v1/video/generations" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "otoy-image-to-video-seedance-2-0-mini-reference-to-video",
    "type": "image-to-video",
    "prompt": "参考图对应关系：@Image1、@Image2、@Image3。三位成年演员在训练馆中进行电影武打排练，动作清晰，无血腥，无真实伤害，不要字幕、logo、水印。",
    "image_urls": [
      "https://example.com/ref-1.jpg",
      "https://example.com/ref-2.jpg",
      "https://example.com/ref-3.jpg"
    ],
    "resolution": "720p",
    "duration": "15",
    "aspect_ratio": "16:9",
    "generate_audio": false
  }'
```

### 查询任务

```bash
curl "http://154.40.44.244:3000/v1/video/generations/task_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

## 11. Python 示例

```python
import time
import requests

BASE_URL = "http://154.40.44.244:3000"
API_KEY = "YOUR_API_KEY"

headers = {
    "Authorization": f"Bearer {API_KEY}",
    "Content-Type": "application/json",
}

payload = {
    "model": "otoy-image-to-video-seedance-2-0-mini-reference-to-video",
    "type": "image-to-video",
    "prompt": "参考图对应关系：@Image1、@Image2、@Image3。三位成年演员在训练馆中进行电影武打排练，动作清晰，无血腥。",
    "image_urls": [
        "https://example.com/ref-1.jpg",
        "https://example.com/ref-2.jpg",
        "https://example.com/ref-3.jpg",
    ],
    "resolution": "720p",
    "duration": "15",
    "aspect_ratio": "16:9",
    "generate_audio": False,
}

create_resp = requests.post(
    f"{BASE_URL}/v1/video/generations",
    headers=headers,
    json=payload,
    timeout=120,
)
create_resp.raise_for_status()
task_id = create_resp.json()["task_id"]
print("task_id:", task_id)

while True:
    time.sleep(10)
    status_resp = requests.get(
        f"{BASE_URL}/v1/video/generations/{task_id}",
        headers={"Authorization": f"Bearer {API_KEY}"},
        timeout=60,
    )
    status_resp.raise_for_status()
    body = status_resp.json()
    data = body.get("data", body)
    status = data.get("status")
    print(status, data.get("progress"))

    if status in ("SUCCESS", "completed"):
        print("video:", data.get("result_url") or data.get("url"))
        break
    if status in ("FAILURE", "failed"):
        raise RuntimeError(data.get("fail_reason") or data)
```

## 12. 常见错误

| 错误 | 原因 | 处理 |
| --- | --- | --- |
| `model_price_error` | 后台未配置模型价格 | 管理员在模型定价里配置该模型价格 |
| `Cannot POST /v1/videos` | 直连上游时用了错误接口 | 通过本平台 NewAPI 调用，或走适配器 |
| `We couldn't access the file...` | 图片/视频/音频 URL 上游不可访问 | 换公网可访问 URL，或让平台镜像上传 |
| `状态查询过于频繁` | 轮询太快 | 查询间隔调到 `5-10` 秒 |
| `FAILURE` | 上游任务失败或安全审核未过 | 查看 `fail_reason` / `data.error.message` |

## 13. 已验证结果

本次实测参数：

```json
{
  "model": "otoy-image-to-video-seedance-2-0-mini-reference-to-video",
  "type": "image-to-video",
  "image_count": 3,
  "duration": "15",
  "aspect_ratio": "16:9",
  "resolution": "720p",
  "generate_audio": false
}
```

实测任务：

| 项目 | 值 |
| --- | --- |
| NewAPI 任务 ID | `task_gT5dFpuGjQGl8mUZ1gKUokBIJagoi2xH` |
| 上游任务 ID | `job_1782991153352_xvfuhp08p` |
| 状态 | `SUCCESS` |
| 视频 URL | `https://aigc.cglol.com/media/2026-07-02/1782991475323_2ufili.mp4` |
| 时长 | `15.042s` |
| 文件大小 | `6,669,715 bytes` |
| 音频轨 | 无，因本次传 `generate_audio: false` |

