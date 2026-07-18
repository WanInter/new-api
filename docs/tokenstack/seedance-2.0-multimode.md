# Seedance 2.0 视频生成 API（多模式）

> 来源：[TokenStack Seedance 官方接入文档](https://new.tokenstack.cc/docs.html#seedance-sale)，整理日期：2026-07-18。选型说明见 [README](./README.md)。

基于 **Seedance 2.0** 的视频生成接口，支持**文生视频（t2v）**、**首帧生视频（i2v）**、**参考生视频（r2v）**——尤其适合拿**角色设定图 / 九宫格图**生成该角色的视频。任务为异步执行，提交后返回 `id`（`task_` 开头），通过轮询查询结果。

**🚀 对接速查（给你 / 你的程序员）**

- **模型名：**`seedance-2-0-sale`
- **提交：**`POST https://www.tokenstack.cc/v1/videos`　**查询：**`GET /v1/videos/{id}`（用 `id`、不是 `task_id`）
- **异步流程：**提交拿 `id`（`task_` 开头，**不是 `task_id`**）→ 轮询 `status` → 取 `object`（结果链接）。**轮询间隔 ≥ 20 秒**
- **请求体结构：**`{ model, prompt, input:{ prompt, media? }, parameters:{…} }`（注意是 `input`/`parameters` 包裹，和别的视频模型不一样）
- ⚠️ **顶层和 `input` 里都要放 `prompt`：**除了 `input.prompt`，**顶层必须再放一个 `prompt`**（两边填**一样**的内容），否则提交直接报 `400 {"message":"prompt is required"}`。原因：校验只认**顶层** `prompt`，`input.prompt` 是传给上游生成用的。
- **图片 / 素材必须是公网 http/https URL**，不收 base64 / 本地文件

## 三种生成模式

| 模式 | 用途 | `media` 传什么 |
|----|----|----|
| **t2v**（文生） | 纯文字生视频 | 不传 `media` |
| **i2v**（首帧生） | 给一张首帧图，从它开始动 | `[{"type":"first_frame","url":…}]`（尺寸随图，不用 `ratio`） |
| **r2v**（参考生） | **核心**：拿角色图 / 九宫格图生成该角色视频 | `[{"type":"reference_image","url":…}, …]`（可多张） |

## `parameters` 参数

| 参数 | 取值 | 说明 |
|----|----|----|
| `resolution` | `"720P"` / `"1080P"` | 大写 P |
| `duration` | `5` / `10` / `15` | 整数，秒。**具体支持哪几档以实测为准**（见已知局限） |
| `ratio` | `16:9` / `9:16` / `1:1` / `4:3` / `3:4` | t2v、r2v 用；i2v 不用（尺寸随首帧图） |
| `prompt_extend` | `true` / `false` | 是否让上游自动扩写提示词 |
| `watermark` | `true` / `false` | 水印 |

**⚠️ 端点提醒：**`/v1/videos` 是共用端点（多个视频模型走这里），**靠 `model` 字段路由**。本模型请求体是 `{model, input, parameters}` 结构，和 Seedance 2.0（15s） 的 Sora 扁平格式不一样，**别套错模板**。

**Base URL：**`https://www.tokenstack.cc`　**鉴权：**`Authorization: Bearer sk-你的TokenStack密钥`（每个请求都带）

## 提交视频任务

提交后立即返回 `id`（`task_` 开头），**不阻塞等待**。请求体 `application/json`。

### API 端点

```http
POST https://www.tokenstack.cc/v1/videos
```

### 请求参数

| 参数 | 类型 | 必填 | 说明 |
|----|----|----|----|
| `model` | string | 是 | 固定填 `seedance-2-0-sale` |
| `prompt`（顶层） | string | **是** | **⚠️ 必填，缺了直接报错。**和下面 `input.prompt` 填**一样**的内容 |
| `input.prompt` | string | 是 | 提示词（和顶层 `prompt` 一致） |
| `input.media` | array | 否 | 图生 / 参考类才传。元素 `{type, url}`：`type` = `first_frame`（首帧）/ `reference_image`（参考图 / 角色图，可多张）。**url 必须公网** |
| `parameters` | object | 否 | 见上方 `parameters` 表（resolution / duration / ratio / prompt_extend / watermark） |

### 请求示例

#### 1. 文生视频 t2v（不传 media）

```bash
curl https://www.tokenstack.cc/v1/videos \
  -H "Authorization: Bearer sk-你的TokenStack密钥" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2-0-sale",
    "prompt": "一只小猫在荔枝园里奔跑，电影感",
    "input": { "prompt": "一只小猫在荔枝园里奔跑，电影感" },
    "parameters": { "resolution":"1080P", "ratio":"16:9", "duration":15, "prompt_extend":false, "watermark":false }
  }'
```

#### 2. 首帧生视频 i2v（1 张首帧，不用 ratio）

```bash
curl https://www.tokenstack.cc/v1/videos \
  -H "Authorization: Bearer sk-你的TokenStack密钥" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2-0-sale",
    "prompt": "图片中的人物开始跳舞",
    "input": {
      "prompt": "图片中的人物开始跳舞",
      "media": [ { "type":"first_frame", "url":"https://你的图床/first.jpg" } ]
    },
    "parameters": { "resolution":"1080P", "duration":10, "prompt_extend":true, "watermark":false }
  }'
```

#### 3. 参考生视频 r2v（角色图 / 九宫格图，可多张）

```bash
curl https://www.tokenstack.cc/v1/videos \
  -H "Authorization: Bearer sk-你的TokenStack密钥" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2-0-sale",
    "prompt": "图1中的人物穿上图2的装饰，在城市街头行走",
    "input": {
      "prompt": "图1中的人物穿上图2的装饰，在城市街头行走",
      "media": [
        { "type":"reference_image", "url":"https://你的图床/role.png" },
        { "type":"reference_image", "url":"https://你的图床/outfit.png" }
      ]
    },
    "parameters": { "resolution":"1080P", "ratio":"16:9", "duration":10, "prompt_extend":false, "watermark":false }
  }'
```

### 成功响应

```json
{ "id":"task_xxx", "task_id":"task_xxx", "object":"", "status":"queued", "progress":0, "created_at":0 }
```

**⚠️ 查询只认 `id`（`task_` 开头那个），别用 `task_id`！**

- 保存响应里的 `id`（形如 `task_xxx`），下一步查询就用它。
- **别拿 `task_id` 去查**——轮询时它可能变成上游内部的 UUID（形如 `e86b9cc9-5587-…` 带横杠），用它查会 **404 / 查不到**。
- 一句话口诀：**查询永远用 `id`（`task_` 开头），看到带横杠的 UUID 就忽略。**

## 查询任务状态

用提交返回的 `id`（`task_` 开头那个，**不是 `task_id`**）轮询，**间隔 ≥ 20 秒**（官方要求，别更快）。

### API 端点

```http
GET https://www.tokenstack.cc/v1/videos/{id}
```

↑ `{id}` 填提交返回的 `id`（`task_` 开头），**不是 `task_id`**

### 请求示例

```bash
curl https://www.tokenstack.cc/v1/videos/task_xxx \
  -H "Authorization: Bearer sk-你的TokenStack密钥"
```

### 响应（三种状态）

```json
// 生成中
{ "id":"...", "object":"", "seconds":0, "status":"RUNNING", "created_at":1780213835 }

// 完成 —— 视频在 object，seconds 是成片时长
{ "id":"...", "object":"https://视频地址", "seconds":15, "status":"SUCCEEDED", "created_at":1780213835 }

// 失败 —— 原因直接拼在 status 里
{ "id":"...", "object":"", "seconds":0,
  "status":"FAILED: 内容审核未通过（输入可能含不适当内容）" }
```

### 判断逻辑（重要）

| 状态 | 怎么判断 | 取什么 |
|----|----|----|
| **成功** | `status === "SUCCEEDED"` | 取 `object`（视频链接）、`seconds`（时长） |
| **失败** | `status` **以 `"FAILED"` 开头** | 原因就在 `status` 字符串里 |
| **进行中** | `PENDING` / `RUNNING` | 等 ≥20 秒再查 |

**注意事项：**

- **轮询 ≥ 20 秒**一次，加最大轮询时长兜底（慢速出片可能十几分钟）。
- **失败判断认 `status` 前缀 `FAILED`**，原因（含内容审核拦截）就在字符串里，可直接展示给用户。
- 结果链接 `object` 有时效，看到 `SUCCEEDED` 后尽快下载 / 转存。
