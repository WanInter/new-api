# Bagege Seedance v2 Mini 视频模型接入文档

## 1. 基本信息

- 服务地址：`http://154.40.44.244:3000`
- 接口风格：OpenAI 兼容视频任务接口
- 鉴权方式：`Authorization: Bearer sk-xxxx`
- 提交任务：`POST /v1/videos`
- 查询任务：`GET /v1/videos/{task_id}`
- 下载视频：`GET /v1/videos/{task_id}/content`
- 计费方式：三个模型均为固定按次计费，当前价格 `0.12/条`

> 注意：视频生成是异步任务。提交后先返回 `task_id`，需要继续轮询任务状态。

## 2. 模型列表

| 模型 ID | 类型 | 说明 |
| --- | --- | --- |
| `bytedance-seedance-v2-mini-txt2vid` | 文生视频 | 仅用提示词生成视频 |
| `bytedance-seedance-v2-mini-img2vid` | 首尾帧/图生视频 | 支持首帧图和尾帧图 |
| `bytedance-seedance-v2-mini-reference` | 全能参考 | 支持图片、视频、音频 URL 列表参考 |

## 3. 通用请求字段

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 模型 ID |
| `prompt` | string | 是 | 视频生成提示词 |
| `seconds` | string/number | 否 | 视频时长，适配器会转为上游 `duration` |
| `size` | string | 否 | 输出尺寸，如 `1280x720`、`720x1280` |
| `generate_audio` | boolean | 否 | 是否生成音频，默认可传 `false` |
| `duration` | string/number | 否 | 也可直接传上游字段 |
| `aspect_ratio` | string | 否 | 也可直接传 `16:9`、`9:16`、`1:1` |
| `resolution` | string | 否 | 也可直接传 `720p`、`1080p` |
| `end_user_id` | string | 否 | 上游终端用户 ID，可选 |

`size` 会自动转换：

- `1280x720` -> `aspect_ratio: "16:9"`，`resolution: "720p"`
- `720x1280` -> `aspect_ratio: "9:16"`，`resolution: "720p"`

## 4. 模型专属参数

### 4.1 文生视频

模型：`bytedance-seedance-v2-mini-txt2vid`

可用字段：

- `prompt`
- `seconds` / `duration`
- `size` / `aspect_ratio`
- `resolution`
- `generate_audio`
- `end_user_id`

### 4.2 首尾帧/图生视频

模型：`bytedance-seedance-v2-mini-img2vid`

可用字段：

- `prompt`
- `image_url`
- `end_image_url`
- `input_reference`：兼容字段，会转为 `image_url`
- `images[0]`：兼容字段，可转为 `end_image_url`
- `seconds` / `duration`
- `size` / `aspect_ratio`
- `resolution`
- `generate_audio`
- `end_user_id`

### 4.3 全能参考

模型：`bytedance-seedance-v2-mini-reference`

可用字段：

- `prompt`
- `image_urls`
- `video_urls`
- `audio_urls`
- `images`：兼容字段，会转为 `image_urls`
- `videos`：兼容字段，会转为 `video_urls`
- `audios`：兼容字段，会转为 `audio_urls`
- `input_reference`：兼容字段，会合并进 `image_urls`
- `seconds` / `duration`
- `size` / `aspect_ratio`
- `resolution`
- `generate_audio`
- `end_user_id`

## 5. 请求示例

### 5.1 文生视频

```bash
curl -X POST http://154.40.44.244:3000/v1/videos \
  -H "Authorization: Bearer sk-xxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "bytedance-seedance-v2-mini-txt2vid",
    "prompt": "生成一条15秒真人写实电影动作视频，两个成年武术演员在训练馆进行动作戏切磋，真实光影，动作连贯，无血腥，无文字，无字幕，无logo，无水印。",
    "seconds": "15",
    "size": "1280x720",
    "generate_audio": false
  }'
```

### 5.2 首尾帧视频

```bash
curl -X POST http://154.40.44.244:3000/v1/videos \
  -H "Authorization: Bearer sk-xxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "bytedance-seedance-v2-mini-img2vid",
    "prompt": "根据首帧和尾帧生成自然过渡的视频，电影感镜头运动，无文字水印。",
    "seconds": "5",
    "size": "1280x720",
    "input_reference": "https://example.com/first.png",
    "images": [
      "https://example.com/end.png"
    ],
    "generate_audio": false
  }'
```

### 5.3 全能参考

```bash
curl -X POST http://154.40.44.244:3000/v1/videos \
  -H "Authorization: Bearer sk-xxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "bytedance-seedance-v2-mini-reference",
    "prompt": "参考图片、视频和音频生成一条风格一致的视频，真实光影，无文字水印。",
    "seconds": "5",
    "size": "1280x720",
    "images": [
      "https://example.com/ref1.png",
      "https://example.com/ref2.png"
    ],
    "videos": [
      "https://example.com/ref.mp4"
    ],
    "audios": [
      "https://example.com/ref.mp3"
    ],
    "generate_audio": false
  }'
```

## 6. 提交响应

提交成功后返回任务对象：

```json
{
  "id": "task_xxx",
  "task_id": "task_xxx",
  "object": "video",
  "model": "bytedance-seedance-v2-mini-txt2vid",
  "status": "queued",
  "progress": 0,
  "created_at": 1783004799
}
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `id` / `task_id` | 对外任务 ID，后续轮询使用 |
| `status` | `queued`、`processing`、`completed`、`failed` |
| `progress` | 进度，0-100 |

## 7. 查询任务

```bash
curl http://154.40.44.244:3000/v1/videos/task_xxx \
  -H "Authorization: Bearer sk-xxxx"
```

完成响应示例：

```json
{
  "id": "task_xxx",
  "object": "video",
  "model": "bytedance-seedance-v2-mini-txt2vid",
  "status": "completed",
  "progress": 100,
  "url": "https://example.com/result.mp4",
  "video_url": "https://example.com/result.mp4",
  "result": "https://example.com/result.mp4",
  "error": null
}
```

客户端优先读取：

1. `video_url`
2. `url`
3. `result`
4. `results[0]`

## 8. 下载视频

方式一：直接下载轮询结果里的 `video_url`。

方式二：通过平台代理下载（无需鉴权）：

```bash
curl -L http://154.40.44.244:3000/v1/videos/task_xxx/content \
  -o result.mp4
```

## 9. 实测记录

已实测通过：

- 模型：`bytedance-seedance-v2-mini-txt2vid`
- 请求：15 秒真人写实动作视频
- 任务：`task_ujOBpI547ZTWfgZvc65NNlRzYvH6LzjM`
- 状态：`completed`
- 本地校验时长：约 `15.104s`
- `/v1/videos/{task_id}/content`：已验证返回 `200 video/mp4`

## 10. 注意事项

- 所有参考素材必须是公网可访问 URL。
- 不建议上传过期签名 URL，否则上游拉取素材时可能失败。
- 真人/人物类内容更容易触发上游审核，建议避免血腥、严重受伤、武器、暴力伤害描述。
- 视频任务耗时较长，建议客户端轮询间隔设置为 3-5 秒。
- 如果状态为 `failed`，读取响应中的 `error.message`。
