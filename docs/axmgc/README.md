# Axmgc 视频渠道接入文档

## 渠道配置

| 项目 | 值 |
| --- | --- |
| 渠道类型 | `Axmgc` |
| 默认 Base URL | `https://axmgc.com` |
| 上游模型 | `seedance-2-720p-933` |
| 密钥 | 以 `hm_` 开头的 API Key |
| 鉴权 | `Authorization: Bearer <API_KEY>` |
| 任务提交 | `POST /v1/video/generations`（JSON） |
| 任务查询 | `GET /v1/video/generations/{video_id}` |

在渠道模型列表中添加公开模型名 `seedance-2-720p-933`。该名称会原样转发给
Axmgc；只有需要切换其他上游型号时才配置显式模型映射。
供应商文档中的 ¥4.50/次是上游价格，不会自动成为 New API 的用户售价；
启用渠道前还需在模型定价设置中配置该公开模型的本地按次价格。

视频为固定 15 秒；调用方提交的其他时长会在转发前统一改为 15 秒。渠道支持
最多 9 张图片、3 个视频和 3 个音频参考素材。
素材引用必须在 `content` 中出现在文本提示词之前，按顺序分别对应
`@Image1` 至 `@Image9`、`@Video1` 至 `@Video3` 和 `@Audio1` 至
`@Audio3`。

建议调用方为每个任务设置 `X-Idempotency-Key`；同一任务的重试应保留该
值，避免重复创建视频。

## 模型发现

```bash
curl https://axmgc.com/v1/models \
  -H 'Authorization: Bearer hm_xxx'
```

本次接入使用以下模型：

| 上游模型 ID | 输出 | 时长 | 参考素材上限 | 上游文档价格 |
| --- | --- | --- | --- | --- |
| `seedance-2-720p-933` | 720p | 固定 15 秒 | 9 图片 / 3 视频 / 3 音频 | ¥4.50/次 |

## JSON 提交：公网素材 URL

New API 直接将 JSON `content` 转发到 Axmgc 的 JSON 生成接口。素材 URL 必须能被
Axmgc 上游直接访问，不能使用本地路径、内网地址或依赖登录 Cookie 的 URL。

```bash
curl -X POST https://your-new-api.example/v1/videos \
  -H 'Authorization: Bearer sk-xxx' \
  -H 'Content-Type: application/json' \
  -H 'X-Idempotency-Key: scene-001' \
  -d '{
    "model": "seedance-2-720p-933",
    "content": [
      {"type": "image_url", "image_url": {"url": "https://cdn.example.com/role.png"}},
      {"type": "image_url", "image_url": {"url": "https://cdn.example.com/scene.jpg"}},
      {"type": "video_url", "video_url": {"url": "https://cdn.example.com/camera.mp4"}},
      {"type": "audio_url", "audio_url": {"url": "https://cdn.example.com/bgm.mp3"}},
      {"type": "text", "text": "@Image1 是主角，@Image2 是场景，参考 @Video1 的运镜和 @Audio1 的音乐氛围。15 秒横屏，电影感。"}
    ],
    "aspect_ratio": "16:9",
    "resolution": "720p",
    "duration": 15
  }'
```

URL 字段兼容以下两种形式，推荐第一种标准写法：

```json
{"type":"image_url","image_url":{"url":"https://cdn.example.com/role.png"}}
```

```json
{"type":"image_url","url":"https://cdn.example.com/role.png"}
```

对于本项目的 Axmgc 渠道，公开 API 使用第一种标准写法。适配器也接受本项目
常用的顶层 `prompt`、`images`、`videos` 和 `audios` 字段，并在转发时转换为标准
`content` URL 对象。`content` 中的素材必须在 `text` 前出现。

## 本地文件

Axmgc 渠道不接受 multipart 和本地文件上传。请先将素材上传到可公开访问的存储，
然后使用 URL `content`；也可以复用已在 Axmgc 账户中创建的 `asset_id`。

## 上传资产后复用

Axmgc 的 `POST /v1/assets` 上传接口会返回 `asset_id`。生成时可在 `content` 中使用
`image_asset`、`video_asset`、`audio_asset`；这些资产必须属于当前渠道 API Key 对应的
Axmgc 账户。New API 不提供资产上传接口，也不管理资产生命周期。

```json
{"type":"image_asset","image_asset":{"asset_id":"asset_xxx"}}
```

## 任务状态和结果

提交或查询会返回类似以下结构：

```json
{
  "id": "video_xxx",
  "model": "seedance-2-720p-933",
  "status": "succeeded",
  "resource_list": [
    {
      "resource_type": "video",
      "resource_url": "https://axmgc.com/v1/video-proxy/xxx?sig=xxx&exp=1780000000"
    }
  ],
  "fail_reason": null
}
```

状态包括 `pending`、`submitted`、`running`、`succeeded` 和 `failed`。只有
`succeeded` 有可用视频。`resource_list[].resource_url` 是带时效签名的播放或
下载 URL；调用方应在任务成功后立即持久化到自己的长期存储。若需要下载，向 URL
追加 `download=1`；已有查询参数时使用 `&download=1`。

```bash
curl https://axmgc.com/v1/video/generations/video_xxx \
  -H 'Authorization: Bearer hm_xxx'
```

New API 会将上游 `video_xxx` 保存在任务私有数据中，对调用方仅返回其公开
`task_xxx` ID。查询统一使用：

```text
GET /v1/video/generations/{task_xxx}
```

## Python 快速接入

下面示例直接调用 Axmgc 上游，展示与 New API 一致的 JSON 内容结构。通过 New API
调用时，请使用本站 Base URL 和令牌，并按上文保存提交响应中的公开 `task_xxx`。

```python
import time

import requests

BASE = "https://axmgc.com"
KEY = "hm_xxx"
headers = {
    "Authorization": f"Bearer {KEY}",
    "X-Idempotency-Key": "scene-001",
}
payload = {
    "model": "seedance-2-720p-933",
    "content": [
        {"type": "image_url", "image_url": {"url": "https://cdn.example.com/role.png"}},
        {"type": "image_url", "url": "https://cdn.example.com/scene.jpg"},
        {"type": "text", "text": "@Image1 是主角，@Image2 是场景。15 秒横屏，镜头推进。"},
    ],
    "aspect_ratio": "16:9",
    "resolution": "720p",
    "duration": 15,
}

response = requests.post(
    f"{BASE}/v1/video/generations",
    headers=headers,
    json=payload,
    timeout=120,
)
response.raise_for_status()
task = response.json()

for _ in range(150):
    if task["status"] not in {"pending", "submitted", "running"}:
        break
    time.sleep(8)
    response = requests.get(
        f"{BASE}/v1/video/generations/{task['id']}",
        headers={"Authorization": f"Bearer {KEY}"},
        timeout=30,
    )
    response.raise_for_status()
    task = response.json()

if task["status"] != "succeeded":
    raise RuntimeError(task.get("fail_reason") or f"task ended as {task['status']}")

video_url = task["resource_list"][0]["resource_url"]
separator = "&" if "?" in video_url else "?"
print(video_url)
print(video_url + separator + "download=1")
```
