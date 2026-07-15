# 渠道 5 Hermes Video 接入文档（客户版）

更新时间：2026-07-15

## 1. 基本信息

| 项目     | 内容                                                          |
| -------- | ------------------------------------------------------------- |
| 服务地址 | `http://154.40.44.244:3000`                                   |
| 接口风格 | OpenAI 兼容异步视频任务                                       |
| 鉴权方式 | `Authorization: Bearer YOUR_OPEN_API_KEY`                     |
| 创建视频 | `POST /v1/videos`                                             |
| 查询任务 | `GET /v1/videos/{task_id}`                                    |
| 工作台   | `http://154.40.44.244:8099/newapi-image-video-workbench.html` |

请求头：

```http
Authorization: Bearer YOUR_OPEN_API_KEY
Content-Type: application/json
```

## 2. 当前开放模型

本渠道只开放以下两个模型：

| 模型 ID     | 类型       |      价格 | 时长       | 说明                                                      |
| ----------- | ---------- | --------: | ---------- | --------------------------------------------------------- |
| `ax2.0-9tu` | 多模态视频 |   3 元/条 | 固定 15 秒 | 至少一段非空文本，可选图片、视频、音频参考；图片最多 9 张 |
| `sdquan-2`  | 图生视频   | 7.8 元/条 | 固定 15 秒 | 必须至少 1 张图片参考；图片最多 9 张，可选视频、音频参考  |

不要调用本渠道其他上游模型；未开放模型会返回不支持。

## 3. 请求字段

| 字段             | 类型    | 必填 | 说明                                                          |
| ---------------- | ------- | ---- | ------------------------------------------------------------- |
| `model`          | string  | 是   | 只能传 `ax2.0-9tu` 或 `sdquan-2`                              |
| `prompt`         | string  | 是   | 顶层提示词，建议与 `content` 里的 text 保持一致               |
| `content`        | array   | 是   | 多模态输入数组，至少一段非空 `text`                           |
| `aspect_ratio`   | string  | 否   | `16:9` / `9:16` / `1:1` / `4:3` / `3:4` / `21:9`，默认 `16:9` |
| `duration`       | number  | 否   | 固定按 15 秒执行；可传 `15`                                   |
| `generate_audio` | boolean | 否   | 是否生成声音，默认 `false`                                    |
| `seed`           | number  | 否   | 随机种子，仅辅助风格稳定                                      |
| `watermark`      | boolean | 否   | 是否加水印，默认 `false`                                      |

## 4. content 类型

| type        | 示例字段                                                 | 限制                            |
| ----------- | -------------------------------------------------------- | ------------------------------- |
| `text`      | `{"type":"text","text":"..."}`                           | 必须至少 1 段非空文本           |
| `image_url` | `{"type":"image_url","image_url":{"url":"https://..."}}` | 最多 9 张；`sdquan-2` 至少 1 张 |
| `video_url` | `{"type":"video_url","video_url":{"url":"https://..."}}` | 最多 3 个                       |
| `audio_url` | `{"type":"audio_url","audio_url":{"url":"https://..."}}` | 最多 3 个                       |

素材引用规则：

- 图片按出现顺序引用为 `@Image1`、`@Image2`
- 视频按出现顺序引用为 `@Video1`
- 音频按出现顺序引用为 `@Audio1`
- prompt 里应写清每个素材的用途，例如角色、场景、道具、运镜、音乐

## 5. ax2.0-9tu 示例

```bash
curl http://154.40.44.244:3000/v1/videos \
  -H "Authorization: Bearer YOUR_OPEN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "ax2.0-9tu",
    "prompt": "15秒横屏视频。古风少年站在山路上，镜头缓慢推进，衣袍轻微飘动，光线自然，无文字水印。",
    "content": [
      {
        "type": "text",
        "text": "15秒横屏视频。古风少年站在山路上，镜头缓慢推进，衣袍轻微飘动，光线自然，无文字水印。"
      }
    ],
    "aspect_ratio": "16:9",
    "duration": 15,
    "generate_audio": false,
    "watermark": false
  }'
```

带图片参考：

```json
{
  "model": "ax2.0-9tu",
  "prompt": "15秒横屏视频。@Image1 是主角参考，保持人物服装和脸部特征；背景为山间营地，镜头缓慢推进，无文字水印。",
  "content": [
    {
      "type": "image_url",
      "image_url": {
        "url": "https://your-cdn.example.com/role.jpg"
      }
    },
    {
      "type": "text",
      "text": "15秒横屏视频。@Image1 是主角参考，保持人物服装和脸部特征；背景为山间营地，镜头缓慢推进，无文字水印。"
    }
  ],
  "aspect_ratio": "16:9",
  "duration": 15,
  "generate_audio": false,
  "watermark": false
}
```

## 6. sdquan-2 示例

`sdquan-2` 必须传至少 1 张图片参考。

```bash
curl http://154.40.44.244:3000/v1/videos \
  -H "Authorization: Bearer YOUR_OPEN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sdquan-2",
    "prompt": "15秒横屏视频。@Image1 是参考画面，保持主体可见，镜头缓慢推进，衣袍和树叶轻微飘动，光线自然，无文字水印。",
    "content": [
      {
        "type": "image_url",
        "image_url": {
          "url": "https://your-cdn.example.com/reference.jpg"
        }
      },
      {
        "type": "text",
        "text": "15秒横屏视频。@Image1 是参考画面，保持主体可见，镜头缓慢推进，衣袍和树叶轻微飘动，光线自然，无文字水印。"
      }
    ],
    "aspect_ratio": "16:9",
    "duration": 15,
    "generate_audio": false,
    "watermark": false
  }'
```

## 7. 多素材示例

```json
{
  "model": "ax2.0-9tu",
  "prompt": "15秒横屏漫剧。@Image1 是女主，@Image2 是男主，@Image3 是夜色街道；参考 @Video1 的缓慢推进运镜，音乐氛围参考 @Audio1。女主发现手机里出现未来短信，表情从疑惑变成震惊。",
  "content": [
    {
      "type": "image_url",
      "image_url": { "url": "https://your-cdn.example.com/role_girl.jpg" }
    },
    {
      "type": "image_url",
      "image_url": { "url": "https://your-cdn.example.com/role_boy.jpg" }
    },
    {
      "type": "image_url",
      "image_url": { "url": "https://your-cdn.example.com/street.jpg" }
    },
    {
      "type": "video_url",
      "video_url": { "url": "https://your-cdn.example.com/camera.mp4" }
    },
    {
      "type": "audio_url",
      "audio_url": { "url": "https://your-cdn.example.com/bgm.mp3" }
    },
    {
      "type": "text",
      "text": "15秒横屏漫剧。@Image1 是女主，@Image2 是男主，@Image3 是夜色街道；参考 @Video1 的缓慢推进运镜，音乐氛围参考 @Audio1。女主发现手机里出现未来短信，表情从疑惑变成震惊。"
    }
  ],
  "aspect_ratio": "16:9",
  "duration": 15,
  "generate_audio": true,
  "watermark": false
}
```

## 8. 本地文件上传

接口建议传公网可访问 URL。只有本地素材时，可以先上传到素材服务，拿到 URL 后放入 `content`。

```bash
curl -X POST http://154.40.44.244:8099/seedance-assets/upload \
  -F "file=@./reference.jpg"
```

响应示例：

```json
{
  "url": "http://154.40.44.244/seedance-assets/local/1783952672-xxxx.jpg",
  "assetUrl": "http://154.40.44.244/seedance-assets/local/1783952672-xxxx.jpg",
  "type": "image"
}
```

## 9. 查询任务

提交成功只表示任务已被接受，最终结果以查询接口返回为准。

```bash
curl http://154.40.44.244:3000/v1/videos/task_xxx \
  -H "Authorization: Bearer YOUR_OPEN_API_KEY"
```

处理中：

```json
{
  "id": "task_xxx",
  "task_id": "video_xxx",
  "status": "in_progress",
  "progress": 70,
  "video_url": null
}
```

完成：

```json
{
  "id": "task_xxx",
  "status": "completed",
  "progress": 100,
  "video_url": "https://axmgc.com/v1/video-proxy/xxx",
  "output": [
    {
      "type": "video",
      "url": "https://axmgc.com/v1/video-proxy/xxx"
    }
  ]
}
```

建议轮询间隔：`10-20` 秒。

## 10. 注意事项

- `prompt` 和 `content` 里的 `text` 都建议传，且内容一致。
- `sdquan-2` 不传图片参考会失败。
- 图片越多，等待时间和失败率可能越高；生产建议只保留关键参考。
- 当前上游返回的视频可能是 `H.265/HEVC` 编码。部分浏览器会出现控件在播放但画面黑的情况，实际视频有画面；如需网页稳定预览，请下载播放或转码为 H.264。
