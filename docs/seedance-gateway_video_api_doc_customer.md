# seedance-gateway 视频生成接入文档

本文档用于下游系统接入本平台视频生成 API。所有请求均调用本平台地址，不需要、也不应关心上游服务信息。

## 基础信息

- Base URL：以平台分配地址为准，例如 `http://154.40.44.244:3000`
- 鉴权方式：`Authorization: Bearer sk-你的令牌`
- 模型名称：`seedance-gateway`
- 任务类型：异步视频生成
- 结果格式：`mp4`

## 提交视频任务

`POST /v1/videos`

### 请求头

```http
Authorization: Bearer sk-你的令牌
Content-Type: application/json
```

### 请求体

```json
{
  "model": "seedance-gateway",
  "prompt": "15秒电影感打戏，两名武者在雨夜屋顶高速交手，刀光与拳脚交错，镜头环绕跟拍，动作连贯有力度。",
  "metadata": {
    "duration": 15,
    "resolution": "720p",
    "aspectRatio": "16:9",
    "generateAudio": true
  },
  "referenceImages": ["https://your-cdn.example.com/reference.jpg"],
  "referenceVideos": [],
  "referenceAudios": []
}
```

## 参数说明

| 字段                     | 类型          | 必填 | 说明                                                  |
| ------------------------ | ------------- | ---- | ----------------------------------------------------- |
| `model`                  | string        | 是   | 固定填写 `seedance-gateway`                           |
| `prompt`                 | string        | 是   | 视频提示词，建议写清镜头、动作、风格、时长            |
| `metadata.duration`      | number/string | 否   | 视频时长，支持 `4` 到 `15` 秒；不传默认按 `15` 秒处理 |
| `metadata.resolution`    | string        | 否   | 分辨率，目前填写 `720p`                               |
| `metadata.aspectRatio`   | string        | 否   | 画幅比例：`16:9`、`9:16`、`1:1`、`auto`               |
| `metadata.generateAudio` | boolean       | 否   | 是否生成声音，默认建议传 `true`                       |
| `referenceImages`        | string[]      | 否   | 参考图片公网 URL 数组                                 |
| `referenceVideos`        | string[]      | 否   | 参考视频公网 URL 数组                                 |
| `referenceAudios`        | string[]      | 否   | 参考音频公网 URL 数组                                 |

素材要求：

- 参考素材必须是公网可访问 URL。
- 不支持 `base64`、本地文件路径、内网地址。
- 图片、视频、音频参考素材合计建议不超过 `9` 个。
- 使用参考素材时，建议在 `prompt` 里写清楚引用关系，例如：`参考第一张图的人物形象，让人物在雨夜屋顶打斗。`

## 返回示例

```json
{
  "id": "task_xxx",
  "task_id": "task_xxx",
  "object": "video",
  "model": "seedance-gateway",
  "status": "in_progress",
  "progress": 0,
  "url": null,
  "video_url": null
}
```

提交成功只表示任务已进入队列，最终结果以查询接口为准。

## 查询任务

`GET /v1/videos/{task_id}`

### 请求头

```http
Authorization: Bearer sk-你的令牌
```

### 成功示例

```json
{
  "id": "task_xxx",
  "task_id": "task_xxx",
  "object": "video",
  "model": "seedance-gateway",
  "status": "completed",
  "progress": 100,
  "url": "https://example.com/result.mp4",
  "video_url": "https://example.com/result.mp4"
}
```

### 失败示例

```json
{
  "id": "task_xxx",
  "task_id": "task_xxx",
  "object": "video",
  "model": "seedance-gateway",
  "status": "failed",
  "progress": 0,
  "error": {
    "message": "生成失败，请稍后重试"
  }
}
```

## 状态说明

| 状态          | 说明     |
| ------------- | -------- |
| `queued`      | 已排队   |
| `in_progress` | 生成中   |
| `completed`   | 生成成功 |
| `failed`      | 生成失败 |

建议每 `5-10` 秒查询一次任务状态。

## curl 示例

```bash
curl -X POST "http://154.40.44.244:3000/v1/videos" \
  -H "Authorization: Bearer sk-你的令牌" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-gateway",
    "prompt": "15秒电影感打戏，两名武者在雨夜屋顶高速交手，镜头环绕跟拍，动作连贯有力度。",
    "metadata": {
      "duration": 15,
      "resolution": "720p",
      "aspectRatio": "16:9",
      "generateAudio": true
    }
  }'
```

```bash
curl -X GET "http://154.40.44.244:3000/v1/videos/task_xxx" \
  -H "Authorization: Bearer sk-你的令牌"
```

## JavaScript 示例

```js
const baseUrl = "http://154.40.44.244:3000";
const apiKey = "sk-你的令牌";

const submitRes = await fetch(`${baseUrl}/v1/videos`, {
  method: "POST",
  headers: {
    Authorization: `Bearer ${apiKey}`,
    "Content-Type": "application/json",
  },
  body: JSON.stringify({
    model: "seedance-gateway",
    prompt:
      "15秒电影感打戏，两名武者在雨夜屋顶高速交手，镜头环绕跟拍，动作连贯有力度。",
    metadata: {
      duration: 15,
      resolution: "720p",
      aspectRatio: "16:9",
      generateAudio: true,
    },
  }),
});

const task = await submitRes.json();

const queryRes = await fetch(
  `${baseUrl}/v1/videos/${task.task_id || task.id}`,
  {
    headers: {
      Authorization: `Bearer ${apiKey}`,
    },
  },
);

const result = await queryRes.json();
console.log(result);
```
