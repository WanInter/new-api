# dreamina-seedance-2-0-mini-hc

- 模型名称: `dreamina-seedance-2-0-mini-hc`
- 调用方式: 自定义
- 请求方法: `POST`

## 模型介绍

# dreamina-seedance-2-0-hc

高并发1000

- 模型名称: `dreamina-seedance-2-0-hc`
- 调用方式: 异步任务
- 请求方法: `POST`

## 模型介绍

**素材与视频生成流程**

## 通用说明

所有接口都使用 Bearer Token 鉴权：

```http
Authorization: Bearer $API_KEY
Content-Type: application/json
```

下文示例里的 `$BASE_URL` 是你的服务地址，例如：

```text
https://api.example.com
```

---

## 1. 创建素材

用于把图片或视频加入素材库，成功后会返回素材 ID。后续视频生成时使用：

```text
asset://素材ID
```

### 请求

```bash
curl -X POST "$BASE_URL/v1/sd/assets" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "dreamina-seedance-2-0-260128",
    "URL": "https://example.com/reference.png",
    "Name": "reference-image-001",
    "AssetType": "Image"
  }'
```

如果是视频素材：

```json
{
  "model": "dreamina-seedance-2-0-260128",
  "URL": "https://example.com/reference.mp4",
  "Name": "reference-video-001",
  "AssetType": "Video"
}
```

### 返回示例

```json
{
  "success": true,
  "data": {
    "Id": "asset-20260707120000-abcd1",
    "Status": "Processing",
    "TaskId": "20260707120000123456789",
    "base_resp": {
      "status_code": 0,
      "status_msg": "success"
    }
  }
}
```

这里需要保存：

```text
Id = asset-20260707120000-abcd1
```

---

## 2. 查询素材状态

素材创建后通常需要几秒钟处理，状态变成 `Active` 后再用于视频生成。

### 请求

```bash
curl -X GET "$BASE_URL/v1/sd/assets/asset-20260707120000-abcd1" \
  -H "Authorization: Bearer $API_KEY"
```

如果查询不到，或者提示需要模型，可以补 `model` 参数：

```bash
curl -X GET "$BASE_URL/v1/sd/assets/asset-20260707120000-abcd1?model=dreamina-seedance-2-0-260128" \
  -H "Authorization: Bearer $API_KEY"
```

### 返回示例

```json
{
  "success": true,
  "data": {
    "Id": "asset-20260707120000-abcd1",
    "Name": "reference-image-001",
    "URL": "https://xxx",
    "AssetType": "Image",
    "Status": "Active",
    "TaskId": "20260707120000123456789",
    "base_resp": {
      "status_code": 0,
      "status_msg": "success"
    }
  }
}
```

当 `Status` 是 `Active` 时，可以进入下一步。

---

## 3. 使用素材生成视频

### 图片参考生成视频

把素材 ID 写成：

```text
asset://asset-20260707120000-abcd1
```

请求示例：

```bash
curl -X POST "$BASE_URL/v1/video/generate" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "dreamina-seedance-2-0-260128",
    "content": [
      {
        "type": "text",
        "text": "参考这张图片中的人物形象，让她在海边自然地回头微笑，真实电影镜头，光线柔和"
      },
      {
        "type": "image_url",
        "image_url": {
          "url": "asset://asset-20260707120000-abcd1"
        },
        "role": "reference_image"
      }
    ],
    "duration": 4,
    "resolution": "720p",
    "ratio": "16:9",
    "watermark": false
  }'
```

### 视频参考生成视频

```bash
curl -X POST "$BASE_URL/v1/video/generate" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "dreamina-seedance-2-0-260128",
    "content": [
      {
        "type": "text",
        "text": "参考这个视频的动作节奏和人物姿态，生成同风格的电影感镜头"
      },
      {
        "type": "video_url",
        "video_url": {
          "url": "asset://asset-20260707120000-abcd1"
        },
        "role": "reference_video"
      }
    ],
    "duration": 4,
    "resolution": "720p",
    "ratio": "16:9",
    "watermark": false
  }'
```

### 返回示例

```json
{
  "task": {
    "id": "mvt-1234567890abcdef",
    "status": "pending",
    "model": "dreamina-seedance-2-0-260128",
    "outputs": [],
    "error": null,
    "created_at": "2026-07-07T12:00:00Z"
  }
}
```

这里保存：

```text
task.id = mvt-1234567890abcdef
```

---

## 4. 查询视频生成结果

```bash
curl -X GET "$BASE_URL/v1/video/tasks/mvt-1234567890abcdef" \
  -H "Authorization: Bearer $API_KEY"
```

### 成功返回示例

```json
{
  "task": {
    "id": "mvt-1234567890abcdef",
    "status": "completed",
    "model": "dreamina-seedance-2-0-260128",
    "duration_seconds": 4,
    "outputs": ["https://example.com/result.mp4"],
    "error": null,
    "created_at": "2026-07-07T12:00:00Z",
    "completed_at": "2026-07-07T12:03:00Z",
    "usage": {
      "completion_tokens": 86400,
      "total_tokens": 86400
    }
  }
}
```

当 `task.status` 是 `completed` 时，`outputs[0]` 就是生成的视频地址。
