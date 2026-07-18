# Seedance 2.0 视频生成 API（多分辨率 · 480p~4K · 固定 15 秒）

> 来源：[TokenStack Seedance 官方接入文档](https://new.tokenstack.cc/docs.html#seedance-multi)，整理日期：2026-07-18。选型说明见 [README](./README.md)。

又一套 **Seedance 2.0** 视频接口，提供 **480p / 720p / 1080p / 4K 多档分辨率**、**mini / fast / pro 多档速度画质**，**全部固定出 15 秒**。8 个 model 请求格式完全一样，只差分辨率和档位。同样走异步：提交返回任务 `id`，轮询查询结果。

**⚠️ 这套和上面几套 Seedance 不一样，三处最容易踩：**

- **参考素材字段名不同：**用 `reference_image_urls` / `reference_videos` / `reference_audios`（**不是**上面那套的 `images` / `videos` / `audios`）。
- **比例是顶层必填：**`aspect_ratio`（`16:9` / `9:16`）必须传。
- **`seconds` 一律填 `"1"`：**本系列**全部固定出 15 秒**，`seconds` 填 `"1"`（代表 1 个固定时长单位），**不是**填真实秒数（填 `"15"` 可能报错或被忽略）。

**🚀 对接速查（给你 / 你的程序员）**

- **提交：**`POST https://www.tokenstack.cc/v1/videos`　**查询：**`GET /v1/videos/{taskId}`
- **模型名：**见下方**「可用模型表」**（8 个，480p~4K × mini/fast/pro），**以控制台实际开通为准**。
- **请求体骨架：**`{ model, prompt, aspect_ratio, seconds, size?, reference_image_urls?, reference_videos?, reference_audios? }`（平铺 JSON）
- **参考素材：**图 ≤ 9 张、视频 ≤ 3 个（每个 3–10 秒）、音频 ≤ 3 个（合计 ≤ 15 秒），全部传**公网 http/https URL**。

## 可用模型（分辨率 × 档位，全部固定 15 秒）

本系列 **8 个 model 请求格式完全一样**，区别只在**分辨率**和**档位**（速度 / 画质 / 价格梯度）。把示例里的 `model` 换成你要的那个即可，**全部固定出 15 秒**、`seconds` 一律填 `"1"`。

| model（调用名）              | 分辨率 | 档位                 | 时长  |
|------------------------------|--------|----------------------|-------|
| `seedance-2.0-480p-mini-15s` | 480p   | mini（最省）         | 15 秒 |
| `seedance-2.0-480p-fast-15s` | 480p   | fast（快）           | 15 秒 |
| `seedance-2.0-480p-15s`      | 480p   | 标准                 | 15 秒 |
| `seedance-2.0-720p-mini-15s` | 720p   | mini（最省）         | 15 秒 |
| `seedance-2.0-720p-fast-15s` | 720p   | fast（快，**推荐**） | 15 秒 |
| `seedance-2.0-720p-pro-15s`  | 720p   | pro（高画质）        | 15 秒 |
| `seedance-2.0-1080p-15s`     | 1080p  | 标准                 | 15 秒 |
| `seedance-2.0-4k-15s`        | 4K     | 标准                 | 15 秒 |

💡 **档位（mini / fast / pro）= 速度、画质、价格的梯度**；分辨率越高、档位越贵越慢。差价以控制台价格为准（**别写死，会变**）。拿不准先用 `seedance-2.0-720p-fast-15s`（均衡）。**以上为当前开通的 8 个，控制台可能增减，以控制台实际列表为准。**

## 接口列表

| 接口 | 方法 | 端点 | 说明 |
|----|----|----|----|
| 提交视频任务 | POST | `/v1/videos` | 立即返回任务 `id`，不阻塞 |
| 查询任务状态 | GET | `/v1/videos/{taskId}` | 轮询进度，完成后返回视频地址 |

**⚠️ 端点提醒：**`/v1/videos` 是共用端点（多个视频模型走这里），**靠 `model` 字段路由**。本模型请求体是**平铺 JSON + `reference_*` 字段名**，和 Seedance 15s（Sora 格式）、Seedance 多模式（input/parameters）都不一样，别套错模板。

**Base URL：**`https://www.tokenstack.cc`　**鉴权：**`Authorization: Bearer sk-你的TokenStack密钥`（每个请求都带）　**请求格式：**`application/json`

## 提交视频任务

提交后立即返回任务 `id`（同时也返回 `task_id`，两者值一致），**不阻塞等待**。请求体 `application/json`。

### API 端点

```http
POST https://www.tokenstack.cc/v1/videos
```

### 请求头

- `Authorization: Bearer sk-你的TokenStack密钥`（必填）
- `Content-Type: application/json`（必填）

### 必填参数

| 参数 | 类型 | 说明 |
|----|----|----|
| `model` | string | 模型名，见可用模型表（如 `seedance-2.0-720p-fast-15s`）；**以控制台开通的为准** |
| `prompt` | string | 视频描述：画面、镜头、风格 |
| `aspect_ratio` | string | 画面比例，`16:9`（横）/ `9:16`（竖） |
| `seconds` | string | **一律填 `"1"`**（本系列全部固定出 15 秒，不是填真实秒数） |

### 可选参数

| 参数 | 类型 | 说明 |
|----|----|----|
| `size` | string | 可不传（**分辨率主要由 model 名决定**：480p/720p/1080p/4K）；需微调时传 `1280x720` 这类值 |
| `reference_image_urls` | string\[\] | 参考图，**最多 9 张**，公网 http/https URL，用于锁定人物 / 商品一致性 |
| `reference_videos` | string\[\] | 参考视频，**最多 3 个**，每个 **3–10 秒**，公网 URL，用于参考运镜 / 动作 |
| `reference_audios` | string\[\] | 参考音频，**最多 3 个**，**合计 ≤ 15 秒**，公网 URL，用于参考氛围音乐 |

💡 该模型另有几个**关人脸审核 / 换脸强度**的高级字段，默认不公开写进文档；确有需要的客户在后台单独拿参数。

### 请求示例

```bash
curl https://www.tokenstack.cc/v1/videos \
  -H "Authorization: Bearer sk-你的TokenStack密钥" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2.0-720p-fast-15s",
    "prompt": "一只橘猫在窗台伸懒腰，窗外下小雨，电影感镜头",
    "aspect_ratio": "16:9",
    "seconds": "1",
    "reference_image_urls": ["https://你的图床.com/ref1.jpg"]
  }'
```

### 成功响应（立即返回）

```json
{
  "id": "task_sHhjEI5N9bqfO9brAPRk1WovzEbis6LM",
  "task_id": "task_sHhjEI5N9bqfO9brAPRk1WovzEbis6LM",
  "status": "queued",
  "progress": 0,
  "created_at": 1780308826,
  "seconds": "1",
  "size": "1280x720"
}
```

拿到 `id`（`task_` 开头）后去查询任务状态轮询。`id` 和 `task_id` 值一样，用哪个都行。

## 查询任务状态

用提交时返回的 `id` 轮询。**轮询节奏：首次等 1 秒，之后每 3 秒一次，最长等 15 分钟。**

### API 端点

```http
GET https://www.tokenstack.cc/v1/videos/{taskId}
```

### 请求示例

```bash
curl https://www.tokenstack.cc/v1/videos/task_sHhjEI5N9bqf \
  -H "Authorization: Bearer sk-你的TokenStack密钥"
```

### 状态枚举

| 类型   | 可能的状态值                                                    |
|--------|-----------------------------------------------------------------|
| 进行中 | `queued` / `pending` / `processing` / `running` / `in_progress` |
| 成功   | `completed` / `succeeded` / `success`                           |
| 失败   | `failed` / `error` / `cancelled` / `canceled`                   |

⚠️ 成功 / 失败各有**好几个同义状态值**，判断时把同组的都算上（别只判 `completed`），否则会漏判。

### 成功响应

```json
{
  "id": "task_d123456",
  "status": "completed",
  "progress": 100,
  "video_url": "https://img.tokenstack.cc/videos/xxx.mp4",
  "url": "https://img.tokenstack.cc/videos/xxx.mp4",
  "result_url": "https://img.tokenstack.cc/videos/xxx.mp4",
  "urls": ["https://img.tokenstack.cc/videos/xxx.mp4"],
  "created_at": 1780304070,
  "completed_at": 1780304261,
  "seconds": "1",
  "size": "1280x720"
}
```

**💡 视频链接取哪个字段：**可能出现在 `urls[0]` / `video_url` / `url` / `result_url` / `metadata.url` 任一处，**优先取 `urls[0]`**，取不到再依次兜底。结果 URL 有有效期，及时下载转存。

### 完整轮询示例（Python）

```python
import time, requests

BASE = "https://www.tokenstack.cc/v1"
KEY  = "sk-你的TokenStack密钥"
TASK_ID = "task_xxx"
headers = {"Authorization": f"Bearer {KEY}"}

time.sleep(1)                       # 首次等 1 秒
deadline = time.time() + 15 * 60    # 最长等 15 分钟
while time.time() < deadline:
    data = requests.get(f"{BASE}/videos/{TASK_ID}", headers=headers, timeout=30).json()
    print(f"状态: {data['status']}  进度: {data.get('progress')}%")
    if data["status"] in ("completed", "succeeded", "success"):
        url = (data.get("urls") or [None])[0] or data.get("video_url") or data.get("url") or data.get("result_url")
        print("视频地址:", url); break
    if data["status"] in ("failed", "error", "cancelled", "canceled"):
        raise RuntimeError(data.get("error") or "生成失败")
    time.sleep(3)                   # 之后每 3 秒一次
else:
    raise TimeoutError("轮询超时（15 分钟）")
```

### 常见错误

| HTTP | 含义 | 处理 |
|----|----|----|
| `401` | 密钥无效 | 检查 Authorization 头 |
| `400` | 参数错误 | 检查必填字段（`model` / `prompt` / `aspect_ratio`）；**`seconds` 记得填 `"1"`** |
| `402` / `403` | 余额不足 / 权限受限 | 充值或确认该模型已开通 |
| `429` | 频率过高 | 降并发、加大间隔 |
| `500` / `502` / `503` | 服务端异常 | 指数退避重试 |

**⚠️ 已知局限 / 对接要点**

- **参考字段名别拿错：**这套是 `reference_image_urls`/`reference_videos`/`reference_audios`，和 15s 那套的 `images`/`videos`/`audios` 不通用。
- **`seconds` 一律填 `"1"`：**本系列全部固定 15 秒，填真实秒数（如 `"15"`）可能报错或被忽略。
- **素材必须公网 URL：**本地文件先转图床 / 对象存储；参考视频每个 3–10 秒、音频合计 ≤ 15 秒。
- **模型可用列表以控制台为准：**别在文档里写死，控制台会随开通情况变化。
