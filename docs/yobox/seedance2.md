# seedance2

- 模型名称: `seedance2`
- 调用方式: 异步任务
- 请求方法: `POST`

## 模型介绍

````markdown
# Seedance2 视频生成 API 调用文档

## 接入信息

- **Base URL**：`https://max.yoboxai.com`
- **接口地址**：`/async/tasks`
- **认证方式**：`Authorization: Bearer <你的 API Key>`
- **Content-Type**：`application/json`
- **计费方式**：按次计费，`seconds` 仅代表视频时长，不按秒扣费。

## 可用模型

| 模型      | 版本说明   |
| --------- | ---------- |
| seedance2 | 快速标准版 |

---

## 两种合法请求格式（均可正常调用）

### 格式一：基础平参数模式（实测稳定可用）

#### CURL 请求

```bash
curl -X POST "https://max.yoboxai.com/async/tasks" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance2",
    "prompt": "让参考图中的人物跳舞，电影质感，高清",
    "size": "720x1280",
    "seconds": "15",
    "input_reference": "https://rep.yoboxapp.com/temp-images/1782212880049-b6a4d26d34fb4c8f.png"
  }'
```
````

#### 请求参数

| 参数            | 类型   | 必填 | 取值说明                            |
| --------------- | ------ | ---: | ----------------------------------- |
| model           | string |   是 | seedance2 / seedance2-pro           |
| prompt          | string |   是 | 视频动作与画面描述                  |
| size            | string |   否 | 分辨率：720x1280、1280x720、720x720 |
| seconds         | string |   否 | 时长字符串，范围4~15，默认4         |
| input_reference | string |   否 | 单张参考图片URL                     |

#### 成功响应

```json
{
  "success": true,
  "message": "",
  "data": {
    "task_id": "task_sRhILcOqQmjbiXho6AWS1ZychHDK7oW4",
    "status": "SUBMITTED",
    "action": "default",
    "progress": 0,
    "platform": "generic",
    "model": "seedance2"
  }
}
```

---

### 格式二：多图 content 数组模式（支持多张参考图）

> 规则说明：外层必须额外带上 `prompt`，内容和 content 内 text 保持一致。

#### CURL 请求

```bash
curl -X POST "https://max.yoboxai.com/async/tasks" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance2",
    "prompt": "@image1；@image2；参考角色外观和场景氛围，生成一段慢镜头短片",
    "seconds": "12",
    "ratio": "16:9",
    "resolution": "480P",
    "generate_audio": false,
    "content": [
      {
        "type": "text",
        "text": "@image1；@image2；参考角色外观和场景氛围，生成一段慢镜头短片"
      },
      {
        "type": "image_url",
        "role": "reference_image",
        "image_url": {
          "url": "https://example.com/assets/character.png"
        }
      },
      {
        "type": "image_url",
        "role": "reference_image",
        "image_url": {
          "url": "https://example.com/assets/scene.png"
        }
      }
    ]
  }'
```

#### 参数补充说明

1. 外层顶层必须配置 `prompt`，文字内容与 content[0].text 完全一致；
2. content 内可传入多张 `image_url` 作为多图参考；
3. 支持 ratio、resolution、generate_audio 扩展字段；
4. seconds 必须为字符串格式，取值范围 4 ~ 15。

---

## 2. 查询任务进度与结果

### CURL 请求

```bash
curl -X GET "https://max.yoboxai.com/async/tasks/task_sRhILcOqQmjbiXho6AWS1ZychHDK7oW4" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

### 返回结果

```json
{
  "success": true,
  "message": "",
  "data": {
    "task_id": "task_sRhILcOqQmjbiXho6AWS1ZychHDK7oW4",
    "platform": "generic",
    "action": "default",
    "status": "SUCCESS",
    "progress": 100,
    "submit_time": 1781180000,
    "start_time": 0,
    "finish_time": 1781180600,
    "fail_reason": "",
    "data": {
      "id": "task_sRhILcOqQmjbiXho6AWS1ZychHDK7oW4",
      "object": "video",
      "model": "seedance2",
      "status": "completed",
      "progress": 100,
      "video_url": "https://xxx/output.mp4",
      "seconds": 12,
      "usage": {
        "seconds": 12,
        "video_count": 1
      }
    }
  }
}
```

视频地址读取路径：`data.data.video_url`

---

## 3. 任务状态枚举

| 状态        | 含义       |
| ----------- | ---------- |
| SUBMITTED   | 任务已提交 |
| QUEUED      | 排队等待   |
| IN_PROGRESS | 视频生成中 |
| SUCCESS     | 生成成功   |
| FAILURE     | 任务失败   |

轮询建议：每5秒调用一次查询接口。

---

## 4. 分辨率与比例配置

### 方案A：size 字段

| size 参数 | 画面比例        |
| --------- | --------------- |
| 720x1280  | 9:16 竖屏短视频 |
| 1280x720  | 16:9 横屏       |
| 720x720   | 1:1 方形画面    |

### 方案B：ratio + resolution（content模式专用）

- ratio：`16:9` / `9:16` / `1:1`
- resolution：`480P` / `720P`

```

```
