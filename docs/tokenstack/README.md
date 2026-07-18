# TokenStack Seedance API 文档

本文档整理自 [TokenStack Seedance 官方接入文档](https://new.tokenstack.cc/docs.html#seedance-pick)，内容快照日期为 2026-07-18。

TokenStack 当前展示 4 套 Seedance 接口。它们虽然大多使用 `/v1/videos`，但请求结构、模型名、时长和结果字段并不统一，不能混用请求模板。

## 接口选型

| 需求 | 模型 | 请求结构 | 文档 |
| --- | --- | --- | --- |
| 固定 15 秒、Sora 兼容格式、接入简单 | `seedance-2-0-15s-slow` / `seedance-2-0-15s-high` / `seedance-2-0-15s-fast` | 顶层 `images` / `videos` / `audios` | [Sora 格式 15 秒](./seedance-2.0-15s-sora.md) |
| 5 / 10 / 15 秒、文生/首帧/角色参考 | `seedance-2-0-sale` | `input` + `parameters` | [多模式](./seedance-2.0-multimode.md) |
| 4-15 秒、首尾帧、多素材、全能参考、按秒计费 | `doubao-seedance-2-0-260128` / `doubao-seedance-2-0-fast-260128` | 顶层 JSON + `content` | [Doubao Seedance 2.0](./doubao-seedance-2.0.md) |
| 480p-4K、mini / fast / pro 档位、固定 15 秒 | `seedance-2.0-720p-fast-15s` 等 8 个模型 | 顶层 `reference_*` 字段 | [多分辨率](./seedance-2.0-multiresolution.md) |

## 公共接入信息

- Base URL：`https://www.tokenstack.cc`
- 鉴权：`Authorization: Bearer sk-你的TokenStack密钥`
- 请求格式：`application/json`
- 任务模式：异步提交，保存响应中的任务 ID 后轮询查询接口
- 素材要求：除非具体文档另有说明，图片、视频和音频均需使用公网可访问的 HTTP(S) URL
- 结果保存：生成结果 URL 通常有有效期，任务成功后应及时下载或转存

> 原页面还包含一套明确标记为“旧版、已隐藏”的 multipart 接口，本目录未将其列为当前接口。模型可用性和价格可能调整，以 TokenStack 控制台及官方页面为准。
