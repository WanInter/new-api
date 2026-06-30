# seedance-2.0-fast

- 模型名称: `seedance-2.0-fast`
- 调用方式: 自定义
- 请求方法: `POST`

## 模型介绍

# 异步视频生成 API 文档

## 接口概览

- **Base URL**：`https://max.yoboxai.com`
- **认证方式**：`Authorization: Bearer YOUR_API_KEY`
- **Content-Type**：`application/json`
- **任务模式**：异步任务。提交任务后返回 `task_id`，客户端轮询查询结果。
- **计费说明**：按视频时长与画质档位计费，画质由 `input.resolution` 决定。

## 支持模型

| 模型                | 说明                                 |
| ------------------- | ------------------------------------ |
| `seedance-2.0-fast` | 快速版，适合常规视频任务，性价比高   |
| `seedance-2.0`      | 标准版，支持更高分辨率，画面效果更好 |

## 1. 提交视频任务

```bash
curl -X POST "https://max.yoboxai.com/async/tasks" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2.0",
    "input": {
      "prompt": "让参考图中的人物跳舞，电影质感，高清",
      "duration": 4,
      "aspect_ratio": "9:16",
      "resolution": "720p",
      "audio": true,
      "image_references": [
        {
          "url": "https://example.com/ref.png",
          "strength": "MID"
        }
      ],
      "n": 1
    }
  }'
```

### 请求参数

| 参数                 | 类型    | 必填 | 说明                                       |
| -------------------- | ------- | ---: | ------------------------------------------ |
| `model`              | string  |   是 | 仅支持 `seedance-2.0-fast`、`seedance-2.0` |
| `input`              | object  |   是 | 视频生成参数                               |
| `input.prompt`       | string  |   是 | 视频提示词                                 |
| `input.duration`     | integer |   是 | 视频时长，通常 3 到 15 秒                  |
| `input.aspect_ratio` | string  |   是 | 宽高比，如 `16:9`、`9:16`、`1:1`           |
| `input.resolution`   | string  |   是 | 画质档位，如 `480p`、`720p`、`1080p`、`4k` |
| `input.audio`        | boolean |   否 | 是否生成音频，默认 `true`                  |
| `input.n`            | integer |   否 | 生成数量，当前建议固定传 `1`               |

## 2. 提交成功响应

```json
{
  "success": true,
  "message": "",
  "data": {
    "task_id": "task_xxxxxxxxxxxxxxxx",
    "status": "SUBMITTED",
    "action": "default",
    "progress": 0,
    "platform": "generic",
    "model": "seedance-2.0"
  }
}
```

请保存：

```text
data.task_id
```

用于后续查询任务。

## 3. 查询任务状态

```bash
curl -X GET "https://max.yoboxai.com/async/tasks/task_xxxxxxxxxxxxxxxx" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

建议每 `5` 秒查询一次，直到任务进入 `SUCCESS` 或 `FAILURE`。

## 4. 查询成功响应

```json
{
  "success": true,
  "message": "",
  "data": {
    "task_id": "task_xxxxxxxxxxxxxxxx",
    "platform": "generic",
    "action": "default",
    "status": "SUCCESS",
    "progress": 100,
    "submit_time": 1760000000,
    "start_time": 0,
    "finish_time": 1760000100,
    "fail_reason": "",
    "data": {
      "id": "task_xxxxxxxxxxxxxxxx",
      "status": "completed",
      "phase": "completed",
      "created_at": 1760000000,
      "completed_at": 1760000100,
      "outputs": ["https://cdn.example.com/videos/task_xxxxxxxxxxxxxxxx_0.mp4"],
      "error": null
    }
  }
}
```

视频地址在：

```text
data.data.outputs[0]
```

## 5. 任务状态说明

| 状态          | 说明     |
| ------------- | -------- |
| `SUBMITTED`   | 已提交   |
| `QUEUED`      | 排队中   |
| `IN_PROGRESS` | 生成中   |
| `SUCCESS`     | 生成成功 |
| `FAILURE`     | 生成失败 |

上游原始状态可查看：

```text
data.data.status
data.data.phase
```

## 6. 画质与计费档位

| resolution | 说明       |
| ---------- | ---------- |
| `480p`     | 标准画质   |
| `720p`     | 高清画质   |
| `1080p`    | 全高清画质 |
| `4k`       | 4K 画质    |

## 7. 纯文生视频示例

```bash
curl -X POST "https://max.yoboxai.com/async/tasks" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2.0-fast",
    "input": {
      "prompt": "一只白色机械鸟飞过雨后的城市天际线，镜头缓慢跟随，电影感",
      "duration": 6,
      "aspect_ratio": "16:9",
      "resolution": "720p",
      "audio": true,
      "n": 1
    }
  }'
```

## 8. 参考图生成示例

```bash
curl -X POST "https://max.yoboxai.com/async/tasks" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2.0",
    "input": {
      "prompt": "让参考图中的角色在城市街头自然行走，电影质感，镜头缓慢推进",
      "duration": 4,
      "aspect_ratio": "9:16",
      "resolution": "720p",
      "audio": true,
      "image_references": [
        {
          "url": "https://example.com/ref.jpg",
          "strength": "MID"
        }
      ],
      "n": 1
    }
  }'
```

## 9. 首尾帧过渡示例

```bash
curl -X POST "https://max.yoboxai.com/async/tasks" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2.0-fast",
    "input": {
      "prompt": "从第一张画面自然过渡到第二张画面，电影感运镜",
      "duration": 6,
      "aspect_ratio": "16:9",
      "resolution": "720p",
      "audio": false,
      "start_frames": [
        "https://example.com/start.jpg"
      ],
      "end_frames": [
        "https://example.com/end.jpg"
      ],
      "n": 1
    }
  }'
```

## 10. 媒体输入格式

图片、视频、音频字段都使用数组。

字符串形式：

```json
"https://example.com/image.jpg"
```

对象形式：

```json
{
  "url": "https://example.com/image.jpg",
  "strength": "MID"
}
```

支持字段：
| 字段 | 说明 |
|---|---|
| `image_references` | 参考图 |
| `start_frames` | 首帧 |
| `end_frames` | 尾帧 |
| `video_references` | 参考视频 |
| `audio_references` | 参考音频 |

不支持本地路径或 `file://`，请使用公网 URL、data URI 或 base64。

## 11. 常见错误

### API Key 无效

```json
{
  "success": false,
  "message": "invalid api key"
}
```

### 参数错误

```json
{
  "success": false,
  "message": "{\"error\":\"参数错误说明\"}"
}
```

### 任务失败

```json
{
  "success": true,
  "message": "",
  "data": {
    "status": "FAILURE",
    "fail_reason": "任务失败原因",
    "data": {
      "status": "failed",
      "error": "上游失败原因"
    }
  }
}
```

## 12. 注意事项

- 仅可使用模型：`seedance-2.0-fast`、`seedance-2.0`，传入其他模型会触发参数错误。
- `resolution` 请直接传 `480p`、`720p`、`1080p`、`4k`。
- `duration` 请传整数秒。
- `n` 当前建议固定传 `1`。
- 生成完成前，查询结果可能没有 `outputs`。
- 视频 URL 读取路径：`data.data.outputs[0]`。
- 拿到视频链接后请自行转存，防止链接过期。
