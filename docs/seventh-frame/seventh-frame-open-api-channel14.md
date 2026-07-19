# 第七帧开放 API 文档 - 稳定通道（通道14）

文档生成时间：2026-07-18T15:53:39.907Z

本文档仅适用于当前已上架的 稳定通道（通道14）（channel14）。如果后台下架通道或模型，请重新下载最新文档。

## 接入信息

- Base URL：`https://diqizhen.jytt4.cn/api/v1`
- 认证方式：`Authorization: Bearer <API_KEY>`
- 当前通道：`channel14`
- API Key：请使用平台为该通道开通的 Key。不同通道独立计费，通道 Key 不要混用。
- 请求格式：JSON，文件上传接口支持 `multipart/form-data` 或原始二进制上传。
- 参考素材必须先调用文件上传接口，并把返回的完整 `file` 对象放入生成接口的 `assets` 数组。
- 如果本站使用本地磁盘存储，`file.url` 必须是上游模型服务可公网访问的地址；否则会出现 `Failed connect to 0.0.0.0:3000`、`Connection refused` 等参考图拉取失败。

## 已上架模型

| model | 名称 | 可选时长 | 可选分辨率 | 素材限制 | 全局默认计费 |
| --- | --- | --- | --- | --- | --- |
viraldance900--person-stripe--6c832bb1--voice-tone--a0c4ee78 | 93全能2.0 720p（不卡人脸） | 4s, 5s, 6s, 7s, 8s, 9s, 10s, 11s, 12s, 13s, 14s, 15s | 720p | 图片 9 / 音频 3 / 视频 0 | 30/条

## 计费说明

上表显示的是当前已上架模型的全局默认计费。开放 API 的实际扣费以请求中的 `Authorization: Bearer <API_KEY>` 为准：

- 如果后台给该 API Key 设置了专属模型价格，则按该 API Key 专属价扣积分。
- 如果该 API Key 没有专属价格，则按全局默认价扣积分。
- 接入方可以携带自己的 API Key 调用 `GET https://diqizhen.jytt4.cn/api/v1/models` 查询该 Key 当前生效的模型价格。
- 创建任务成功后，响应里的 `billing.chargedPoints` 才是本次生成实际扣除的积分。

可选画幅：16:9, 9:16, 1:1, 4:3, 3:4

## 1. 查询模型

```bash
curl -X GET "https://diqizhen.jytt4.cn/api/v1/models" \
  -H "Authorization: Bearer <API_KEY>"
```

响应会返回所有已上架模型。接入 稳定通道（通道14） 时，请筛选 `channel === "channel14"` 的模型。

## 2. 上传素材

图片、音频、视频都必须先上传到文件接口，再把返回的完整 `file` 对象原样传给生成接口的 `assets`。不要只传 `file.url`，也不要只传 `{ "type": "...", "url": "..." }`。

```bash
curl -X POST "https://diqizhen.jytt4.cn/api/v1/files" \
  -H "Authorization: Bearer <API_KEY>" \
  -F "file=@./reference.png"
```

成功响应：

```json
{
  "file": {
    "object": "file",
    "id": "b76bf07d-2a43-4b6e-9919-7ee381650a7b",
    "type": "image",
    "name": "reference.png",
    "url": "https://your-domain.example.com/media/uploads/openapi_xxx/2026-07-08/b76bf07d.png",
    "pathname": "uploads/openapi_xxx/2026-07-08/b76bf07d.png",
    "size": 102400,
    "contentType": "image/png",
    "uploadedAt": "2026-07-08T00:00:00.000Z",
    "createdAt": "2026-07-08T00:00:00.000Z"
  }
}
```

## 3. 创建视频生成任务

```bash
curl -X POST "https://diqizhen.jytt4.cn/api/v1/video/generations" \
  -H "Authorization: Bearer <API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "channel14",
    "model": "viraldance900--person-stripe--6c832bb1--voice-tone--a0c4ee78",
    "prompt": "生成一段电影感产品展示视频，镜头缓慢推进，光影高级",
    "duration": 4,
    "aspectRatio": "16:9",
    "resolution": "720p",
    "assets": [
      {
        "object": "file",
        "id": "b76bf07d-2a43-4b6e-9919-7ee381650a7b",
        "type": "image",
        "name": "reference.png",
        "url": "https://your-domain.example.com/media/uploads/openapi_xxx/2026-07-08/b76bf07d.png",
        "pathname": "uploads/openapi_xxx/2026-07-08/b76bf07d.png",
        "size": 102400,
        "contentType": "image/png",
        "uploadedAt": "2026-07-08T00:00:00.000Z"
      }
    ]
  }'
```

请求字段：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| channel | string | 是 | 固定为 `channel14` |
| model | string | 否 | 使用上方已上架模型 ID；不传时使用通道默认模型 |
| prompt | string | 是 | 生成提示词；本站最多 30000 字符 |
| duration | number | 否 | 必须在模型可选时长内 |
| aspectRatio | string | 否 | 可选值：16:9, 9:16, 1:1, 4:3, 3:4 |
| resolution | string | 否 | 必须在模型可选分辨率内 |
| seed | string | 否 | 随机种子 |
| assets | array | 否 | 参考图片、音频、视频；必须传上传接口返回的完整 `file` 对象数组 |

成功响应：

```json
{
  "generation": {
    "object": "video.generation",
    "id": "task_xxx",
    "status": "queued",
    "progress": 0,
    "channel": "channel14",
    "model": "viraldance900--person-stripe--6c832bb1--voice-tone--a0c4ee78",
    "billing": {
      "chargedPoints": 24
    }
  }
}
```

`chargedPoints` 是本次任务的实际扣费结果；不同 API Key 设置了专属价格时，同一个模型返回的扣费积分可能不同。

## 4. 查询任务结果

```bash
curl -X GET "https://diqizhen.jytt4.cn/api/v1/video/generations/<GENERATION_ID>" \
  -H "Authorization: Bearer <API_KEY>"
```

任务状态：

| status | 说明 |
| --- | --- |
| queued | 已排队 |
| running | 生成中 |
| succeeded | 已完成，读取 `outputVideoUrl` |
| failed | 失败，读取 `errorMessage` |
| blocked | 通道或上游配置阻塞 |

## 5. 查询账户用量

```bash
curl -X GET "https://diqizhen.jytt4.cn/api/v1/usage?channel=channel14" \
  -H "Authorization: Bearer <API_KEY>"
```

## 错误格式

```json
{
  "error": {
    "code": "invalid_request",
    "message": "channel 必须是 channel1 到 channel16 之一。"
  }
}
```

常见错误码：

| code | HTTP | 说明 |
| --- | --- | --- |
| invalid_api_key | 401 | API Key 缺失、无效或通道不匹配 |
| invalid_request | 400 | 参数不合法 |
| unsupported_media_type | 400 | 文件或素材类型不支持 |
| insufficient_quota | 402 | 通道积分不足 |
| not_found | 404 | 任务不存在 |
| upstream_error | 502 | 上游模型服务异常 |

## 接入建议

- 每个平台为不同通道分别保存 API Key。
- 正式环境固定使用 HTTPS 公网域名，并设置服务端 `APP_PUBLIC_BASE_URL`。
- 接入方上传后可以先从外网访问一次 `file.url`，确认返回 200 后再创建生成任务。
- 生成前先调用 `GET /models`，确认模型仍在上架列表。
- 生成后每 5-10 秒轮询一次任务详情。
- 不要把 API Key 暴露在前端浏览器代码中，应由你自己的服务端代调用。
