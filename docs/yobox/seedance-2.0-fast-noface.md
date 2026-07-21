# seedance-2.0-fast-noface

- 模型名称: `seedance-2.0-fast-noface`
- 调用方式: 异步任务
- 请求方法: `POST`

## 模型介绍

# seedance-2.0-noface 异步视频生成 API 最新文档

## 更新说明（本次变更重点）

1. 模型名称调整：仅支持 `seedance-2.0-noface`、`seedance-2.0-fast-noface`，**彻底移除4K分辨率档位**
2. 媒体引用规格更新：
   - `image_references`：最多 **9 张** 参考图，支持 strength 权重
   - 音视频混合约束：`video_references` + `audio_references` 合计总数 ≤ 3 段
     - `video_references`：最多3段，单组总时长 ≤ 15 秒，无 strength 参数
     - `audio_references`：最多3段，与视频共享3段总配额
   - `start_frames`：最多 **1 张**
   - `end_frames`：最多 **1 张**，不可单独使用，必须搭配 start_frames
3. 删除全部4K相关分辨率、时长限制逻辑

## 接口概览

- **Base URL**：`https://max.yoboxai.com`
- **认证方式**：`Authorization: Bearer YOUR_API_KEY`
- **Content-Type**：`application/json`
- **任务模式**：异步任务。提交任务后返回 `task_id`，客户端轮询查询结果。
- **计费说明**：按视频时长与画质档位分级计费，画质由 `input.resolution` 决定。
- **视频时长范围**：固定 **4～15 秒**（不支持 3 秒及以下短时长）

## 支持模型与分辨率/时长限制

### 模型能力明细

| 模型                       | 支持分辨率 | 时长限制            |
| -------------------------- | ---------- | ------------------- |
| `seedance-2.0-fast-noface` | 480p、720p | 4～15 秒            |
| `seedance-2.0-noface`      | 480p、720p | 4～15 秒            |
| `seedance-2.0-noface`      | 1080p      | **仅支持 4～12 秒** |

## 1. 提交视频任务

```bash
curl -X POST "https://max.yoboxai.com/async/tasks" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2.0-noface",
    "input": {
      "prompt": "让参考图中的人物跳舞，电影质感，高清",
      "duration": 4,
      "aspect_ratio": "9:16",
      "resolution": "720p",
      "audio": true,
      "image_references": [
        {
          "url": "https://example.com/ref1.png",
          "strength": "MID"
        }
      ],
      "n": 1
    }
  }'
```

### 请求参数

| 参数                     | 类型    | 必填 | 说明                                                                                         |
| ------------------------ | ------- | ---: | -------------------------------------------------------------------------------------------- |
| `model`                  | string  |   是 | 仅支持 `seedance-2.0-fast-noface`、`seedance-2.0-noface`                                     |
| `input`                  | object  |   是 | 视频生成核心参数                                                                             |
| `input.prompt`           | string  |   是 | 视频生成提示词                                                                               |
| `input.duration`         | integer |   是 | 视频时长，**合法范围 4～15 秒**，需严格匹配分辨率时长限制                                    |
| `input.aspect_ratio`     | string  |   是 | 画面宽高比，支持 `16:9`、`9:16`、`1:1`                                                       |
| `input.resolution`       | string  |   是 | 画质档位，仅支持 480p / 720p / 1080p，无4K档位，严格匹配对应模型时长规格（见上方能力表）     |
| `input.audio`            | boolean |   否 | 是否生成视频音频，默认 `true`                                                                |
| `input.image_references` | array   |   否 | 参考图片，**最多 9 张**，支持 strength 权重调节                                              |
| `input.video_references` | array   |   否 | 参考视频，单类型最多 3 段、所有音视频素材合计≤3段；单组视频总时长≤15秒，不支持 strength 参数 |
| `input.audio_references` | array   |   否 | 参考音频，单类型最多 3 段，与参考视频共享3段总配额，不可超出合计上限                         |
| `input.start_frames`     | array   |   否 | 视频首帧参考图，**最多 1 张**                                                                |
| `input.end_frames`       | array   |   否 | 视频尾帧过渡图，**最多 1 张，必须搭配 start_frames 使用，不可单独调用**                      |
| `input.n`                | integer |   否 | 生成数量，当前固定传 `1`                                                                     |

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
    "model": "seedance-2.0-noface"
  }
}
```

请保存 `data.task_id`，用于后续轮询查询任务结果。

## 3. 查询任务状态

```bash
curl -X GET "https://max.yoboxai.com/async/tasks/task_xxxxxxxxxxxxxxxx" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

建议每 `5` 秒轮询一次，直至任务状态为 `SUCCESS` 成功 / `FAILURE` 失败。

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

视频结果地址：`data.data.outputs[0]`

## 5. 任务状态说明

| 状态          | 说明       |
| ------------- | ---------- |
| `SUBMITTED`   | 任务已提交 |
| `QUEUED`      | 任务排队中 |
| `IN_PROGRESS` | 视频生成中 |
| `SUCCESS`     | 生成成功   |
| `FAILURE`     | 生成失败   |

上游原始状态可读取：

```text
data.data.status
data.data.phase
```

## 6. 分辨率&时长合规细则

### seedance-2.0-fast-noface（极速版）

- 支持分辨率：`480p`、`720p`
- 支持时长：**4～15 秒**（全分辨率通用）

### seedance-2.0-noface（标准版）

- 480p / 720p：支持 **4～15 秒**
- 1080p：仅支持 **4～12 秒**
- 已移除4K分辨率，传入4K直接参数报错

## 7. 多素材合规调用示例（9张参考图+音视频混合素材，无无效参数）

```bash
curl -X POST "https://max.yoboxai.com/async/tasks" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2.0-noface",
    "input": {
      "prompt": "人物跟随视频动作，保持原图样貌，电影运镜",
      "duration": 6,
      "aspect_ratio": "9:16",
      "resolution": "720p",
      "audio": true,
      "image_references": [
        {"url": "https://example.com/img1.jpg","strength":"MID"},
        {"url": "https://example.com/img2.jpg","strength":"MID"},
        {"url": "https://example.com/img3.jpg","strength":"MID"},
        {"url": "https://example.com/img4.jpg","strength":"MID"},
        {"url": "https://example.com/img5.jpg","strength":"MID"},
        {"url": "https://example.com/img6.jpg","strength":"MID"},
        {"url": "https://example.com/img7.jpg","strength":"MID"},
        {"url": "https://example.com/img8.jpg","strength":"MID"},
        {"url": "https://example.com/img9.jpg","strength":"MID"}
      ],
      "video_references": [
        {"url": "https://example.com/vid1.mp4"},
        {"url": "https://example.com/vid2.mp4"}
      ],
      "audio_references": [
        {"url": "https://example.com/audio.mp3"}
      ],
      "n": 1
    }
  }'
```

> 说明1：video_references + audio_references 合计3段，符合配额上限；视频参考不支持 strength 权重参数
> 说明2：image_references 共9张，达到图片素材上限

### 纯音频3段示例（无视频，占用全部音视频配额）

```json
"video_references": [],
"audio_references": [
  {"url": "https://example.com/a1.mp3"},
  {"url": "https://example.com/a2.mp3"},
  {"url": "https://example.com/a3.mp3"}
]
```

### 纯视频3段示例（无音频，占用全部音视频配额）

```json
"video_references": [
  {"url": "https://example.com/v1.mp4"},
  {"url": "https://example.com/v2.mp4"},
  {"url": "https://example.com/v3.mp4"}
],
"audio_references": []
```

## 8. 纯文生视频示例

```bash
curl -X POST "https://max.yoboxai.com/async/tasks" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2.0-fast-noface",
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

## 9. 单参考图生成示例

```bash
curl -X POST "https://max.yoboxai.com/async/tasks" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2.0-noface",
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

## 10. 首尾帧过渡示例（合规必填配对）

```bash
curl -X POST "https://max.yoboxai.com/async/tasks" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2.0-fast-noface",
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

> 强制规则：`end_frames` 必须和 `start_frames` 同时传入，单独传入报错。

## 11. 媒体输入格式 & 硬性数量限制

支持公网 URL、data URI、base64，**不支持本地 file 路径**

### 字段规格汇总

| 字段               | 说明       | 单字段最大数量 | 全局合计约束   | 特殊规则                        |
| ------------------ | ---------- | -------------: | -------------- | ------------------------------- |
| `image_references` | 参考图片   |              9 | 无，独立配额   | 支持 strength 权重调节          |
| `video_references` | 参考视频   |              3 | video+audio ≤3 | 总时长≤15s，**不支持 strength** |
| `audio_references` | 参考音频   |              3 | video+audio ≤3 | 无额外参数                      |
| `start_frames`     | 起始帧图片 |              1 | 独立配额       | 可单独使用                      |
| `end_frames`       | 结束帧图片 |              1 | 独立配额       | **必须搭配 start_frames 使用**  |

### 格式示例

字符串简写：

```json
"https://example.com/image.jpg"
```

带权重对象（仅图片生效）：

```json
{
  "url": "https://example.com/image.jpg",
  "strength": "MID"
}
```

## 12. 常见错误

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

常见参数错误场景：

1. model 传入旧版 seedance-2.0 / seedance-2.0-fast
2. resolution 传入 4k
3. 1080p 分辨率 duration 超过12秒
4. duration <4 或 >15
5. image_references 数量大于9
6. video_references + audio_references 总数大于3
7. 仅传 end_frames 无 start_frames
8. 参考视频总时长超15秒

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

## 13. 重要注意事项

1. 仅支持 `seedance-2.0-fast-noface`、`seedance-2.0-noface` 模型，旧模型名称直接报错；
2. 已下线4K分辨率，传入resolution:4k直接拦截参数报错；
3. 视频时长强制 **4～15秒**，禁止 3 秒及以下数值，1080p仅限4-12秒；
4. 素材配额新规：参考图独立上限9张；视频+音频素材合计不超过3段；
5. `end_frames` 禁止单独调用，必须配对 `start_frames`，否则任务拦截失败；
6. `video_references` 不识别 strength 权重参数，传入无效且不报错，请勿配置；
7. 生成中状态无 outputs 字段，需等待任务 SUCCESS 后读取视频链接；
8. 视频链接有时效性，获取后请及时自行转存，避免链接过期失效。
