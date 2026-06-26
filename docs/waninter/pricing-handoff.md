# Waninter NewAPI 定价同步与配置交接文档

> 给后续 Agent 使用。目标是：上游 `pricing.json` 更新后，能安全拉取、对比、更新本站 NewAPI 价格配置，并保证「展示价格」和「实际扣费」一致。

## 1. 核心原则

1. **只处理上游启用模型**
   - 以 `docs/pricing.latest.json` 中 `is_enabled = true` 的模型为准。
   - 上游未启用、已下线、或本站不准备提供的模型，不要新增价格。

2. **先本地备份，再更新线上**
   - 当前本站线上价格快照保存在：
     - `docs/waninter/newapi-pricing-config-current.json`
   - 每次改价前先刷新这个快照，改完后再刷新一次。
   - 不要保留很多过程 JSON；保留一份当前全量、准确的配置即可。

3. **不要把密钥写入仓库**
   - 上游价格接口 Token、NewAPI 管理 Token、渠道 Key 都不能写入文档、脚本或提交记录。
   - 命令示例统一使用环境变量占位。

4. **业务换算关系**
   - 本站充值规则：`1 CNY = 20 积分`。
   - NewAPI 当前 `QuotaPerUnit = 20`，可以把 quota 显示/扣费理解为本站积分单位。
   - 不要再引入 `1 CNY = 500000 quota` 这类旧假设。

## 2. 重要文件

本文件已合并并取代旧的定价校准计划、校准报告和同步流程文档。

| 文件 | 作用 |
| --- | --- |
| `docs/pricing.latest.json` | 最近一次从 Creative Studio 拉取的上游价格快照 |
| `docs/waninter/newapi-pricing-config-current.json` | 当前本站 NewAPI 线上价格相关配置全量快照 |
| `pkg/billingexpr/expr.md` | 表达式计费系统说明；改表达式计费前必须阅读 |
| `relay/helper/price.go` | NewAPI 实际价格计算入口 |

## 3. 拉取上游 pricing.json

上游价格来自 Creative Studio 管理概览接口：

```bash
export CREATIVE_STUDIO_ADMIN_TOKEN='***'

curl -fsS https://creative-studio.waninter.com/v1/admin/overview \
  --oauth2-bearer "$CREATIVE_STUDIO_ADMIN_TOKEN" \
  -o docs/pricing.latest.json
```

注意：

- 不要把真实 token 写进文档或 Git。
- 拉取后先检查 JSON 是否有效：

```bash
python3 -m json.tool docs/pricing.latest.json >/dev/null
```

## 4. 刷新本站当前价格配置快照

使用 NewAPI 管理 API 获取当前线上配置。推荐走 `newapi` skill 的脚本，不要手写带密钥的 curl。

```bash
SKILL=/Users/mageia/.agents/skills/newapi
API_SCRIPT="$SKILL/scripts/api.js"

if command -v bun >/dev/null; then RUNTIME=bun; else RUNTIME=node; fi

$RUNTIME "$API_SCRIPT" GET /api/option/ > /tmp/newapi-options.json
```

然后筛选价格相关项，写入：

```text
docs/waninter/newapi-pricing-config-current.json
```

建议保留的配置项包括：

- `QuotaPerUnit`
- `ModelPrice`
- `ModelRatio`
- `CompletionRatio`
- `GroupRatio`
- `GroupGroupRatio`
- `billing_setting.billing_expr`
- `billing_setting.billing_mode`
- 其他包含 `price` / `ratio` / `billing` / `quota` 的配置项

## 5. NewAPI 价格配置入口

### 5.1 固定价格：`ModelPrice`

适合每次固定扣费的模型。

当前线上示例：

```json
{
  "veo-omni-flash": 1,
  "grok-image-video": 20,
  "grok-video-1.5": 20
}
```

含义：

```text
实际扣费积分 = ModelPrice * QuotaPerUnit * 分组倍率
```

当前 `QuotaPerUnit = 20`，所以：

- `ModelPrice = 1` => `20 积分`
- `ModelPrice = 20` => `400 积分`

### 5.2 表达式价格：`billing_setting.billing_mode` + `billing_setting.billing_expr`

适合图片/视频阶梯计费，例如按：

- 图片尺寸 `size`
- 图片质量 `quality`
- 视频时长 `duration`
- 视频分辨率 `resolution`
- 生成数量 `sample_count` / `n`

配置方式：

```json
"billing_setting.billing_mode": {
  "model-name": "tiered_expr"
}
```

```json
"billing_setting.billing_expr": {
  "model-name": "param(\"quality\") == \"high\" ? tier(\"high\", 200000) : tier(\"default\", 100000)"
}
```

实际换算由 `relay/helper/price.go` 中 `modelPriceHelperTiered` 执行：

```text
rawCost = 表达式结果
quotaBeforeGroup = rawCost / 1_000_000 * QuotaPerUnit
实际扣费 = quotaBeforeGroup * 分组倍率
```

当前 `QuotaPerUnit = 20`，因此表达式里常见的系数换算是：

```text
1 积分 = 50000 expression rawCost
```

例子：

```text
tier("1K", 100000) => 100000 / 1_000_000 * 20 = 2 积分
tier("720p", 4500000) => 4500000 / 1_000_000 * 20 = 90 积分
```

> 注意：这里的 `50000` 只是表达式内部 rawCost 到积分的换算系数，不是充值比例。充值比例仍然是 `1 CNY = 20 积分`。

## 6. 从上游 pricing_config 生成表达式

### 6.1 固定价格

上游如果是固定 `credits`：

```json
{
  "pricing_config": {
    "image": { "credits": 2 }
  }
}
```

可以：

- 固定价模型：`ModelPrice = credits / QuotaPerUnit`
- 或表达式模型：`tier("default", credits * 50000)`

例如 `2 积分/次`：

```text
ModelPrice = 2 / 20 = 0.1
```

或：

```text
tier("default", 100000)
```

### 6.2 图片阶梯价格

上游常见结构：

```json
{
  "tier_1k": { "low": 2, "medium": 2, "high": 4 },
  "tier_2k": { "low": 4, "medium": 4, "high": 6 },
  "tier_4k": { "low": 6, "medium": 8, "high": 10 }
}
```

表达式通常按 `size` 判断 1K/2K/4K，再按 `quality` 判断 low/medium/high。

示例片段：

```text
param("size") == "2880x2880" || param("size") == "3840x2160" ?
  (param("quality") == "high" ? tier("4k_high", 500000) : tier("4k_low", 300000)) :
  (param("quality") == "high" ? tier("1k_high", 200000) : tier("1k_low", 100000))
```

换算：

```text
credits * 50000 = expression rawCost
```

### 6.3 视频按时长价格

上游常见结构：

```json
{
  "video": {
    "mode": "duration",
    "credits_per_second": 18,
    "min_seconds": 5,
    "duration_param": "duration",
    "resolution_param": "resolution",
    "resolution_multipliers": {
      "720p": 1,
      "1080p": 1.5,
      "4k": 4
    }
  }
}
```

表达式结构：

```text
param("resolution") == "4k" ?
  tier("4k", duration * credits_per_second * 4 * 50000) :
param("resolution") == "1080p" ?
  tier("1080p", duration * credits_per_second * 1.5 * 50000) :
  tier("720p", duration * credits_per_second * 1 * 50000)
```

注意：

- 如果上游有 `min_seconds`，表达式要做最小时长保护。
- 如果前端只允许固定几个 duration，可用三元表达式枚举。
- 不要把视频阶梯价压成固定价。

## 7. 当前关键模型价格基线

以下以 `docs/waninter/newapi-pricing-config-current.json` 为准。

### 7.1 固定价模型

| 模型 | 配置位置 | 当前含义 |
| --- | --- | --- |
| `veo-omni-flash` | `ModelPrice` | `1 * 20 = 20 积分/次` |
| `grok-image-video` | `ModelPrice` | `20 * 20 = 400 积分/次` |
| `grok-video-1.5` | `ModelPrice` | `20 * 20 = 400 积分/次` |
| `seedance-2-0-15s-high` | `ModelPrice` | `11.25 * 20 = 225 积分/次` |

### 7.2 表达式计费模型

当前使用 `tiered_expr` 的模型包括：

- `gpt-image-2`
- `gpt-image-2-4k`
- `gpt-image-2-nio`
- `gpt-image-2-nio-4k`
- `gpt-image-2-vibe`
- `gpt-image-2-vibe-4k`
- `nano-banana`
- `nano-banana-2`
- `seedance2.0-720p`
- `Seedance2.0-jimeng`
- `Seedance2.0-fast`

### 7.3 Seedance / Xinghe 重点

#### `seedance2.0-720p`

当前按上游 Seedance duration 规则：

```text
积分 = max(duration, 5) * 18 * resolution_multiplier
```

倍率：

- `720p`: `1`
- `1080p`: 当前表达式使用 `2.1`
- `4k`: `4`

示例：

```text
5s 720p = 90 积分
```

#### `Seedance2.0-jimeng`

当前价格与 `seedance2.0-720p` 一致。

#### `Seedance2.0-fast`

当前价格已按上游 `xinghe-fast` 对齐：

```text
积分 = duration * 4 * resolution_multiplier
```

支持时长：`4 / 5 / 8 / 10 / 15`，默认按 `10`。

倍率：

- `720p`: `1`
- `1080p`: `1.5`
- `4k`: `4`

示例：

```text
4s 720p = 16 积分
5s 720p = 20 积分
10s 720p = 40 积分
10s 1080p = 60 积分
10s 4k = 160 积分
```

## 8. 分组倍率

当前有 VIP 分组示例：

```json
"GroupGroupRatio": {
  "vip_veo": {
    "vip_veo": 0.8
  }
}
```

含义：

- 用户组：`vip_veo`
- 令牌/使用组：`vip_veo`
- 最终价格乘 `0.8`

普通组仍按自己的 `GroupRatio` 计算。

## 9. 更新线上配置的方法

使用管理 API 更新 `/api/option/`。

请求格式：

```json
{
  "key": "billing_setting.billing_expr",
  "value": "{...JSON string...}"
}
```

注意：

- `value` 必须是字符串。
- 对 map 类配置，需要先把整个 map JSON 序列化成字符串再提交。
- 更新表达式时通常要同时确认：
  - `billing_setting.billing_mode[model] = "tiered_expr"`
  - `billing_setting.billing_expr[model] = "..."`

示例伪代码：

```python
exprs = get_option("billing_setting.billing_expr")
modes = get_option("billing_setting.billing_mode")

exprs[model] = new_expr
modes[model] = "tiered_expr"

update_option("billing_setting.billing_expr", json.dumps(exprs, ensure_ascii=False))
update_option("billing_setting.billing_mode", json.dumps(modes, ensure_ascii=False))
```

## 10. 校验流程

更新后至少做以下检查：

1. **重新拉取线上 option**
   - 确认 `billing_expr` 和 `billing_mode` 已生效。

2. **刷新本地快照**
   - 更新 `docs/waninter/newapi-pricing-config-current.json`。

3. **模型广场展示**
   - 如果改了 tier 名称、表达式、前端展示逻辑，检查模型广场是否显示完整。
   - 此前出现过 classic/default 两套主题展示不一致问题，改展示时要同步两套主题。

4. **实际调用扣费**
   - 用测试 token 调一次小任务。
   - 看任务日志中的消耗是否等于预期积分。

5. **缓存**
   - 如果前端仍旧展示旧价格，先清理服务端/浏览器缓存。
   - 已部署新代码但 UI 不变时，检查当前主题是 `classic` 还是 `default`。

## 11. 常见坑

### 11.1 `param is not defined`

说明表达式执行环境没有走 `param("key")` 方式，或前端/后端错误使用了裸变量。

正确：

```text
param("duration") == "5" ? ...
```

错误：

```text
duration == "5" ? ...
```

### 11.2 表达式价格被重复乘 duration/resolution

任务计费中 `EstimateBilling` 会提供 `OtherRatios`。对于 `tiered_expr`，表达式已经算了完整价格，不能再把 `OtherRatios` 乘一次。

当前代码在 `relay/relay_task.go` 已处理：

```text
info.TieredBillingSnapshot != nil 时不再叠加 OtherRatios
```

### 11.3 只配置了价格但没有配置 billing_mode

如果模型使用表达式，必须同时配置：

```text
billing_setting.billing_mode[model] = "tiered_expr"
billing_setting.billing_expr[model] = "..."
```

### 11.4 上游未启用模型不要配置

原则上只配置 `docs/pricing.latest.json` 中 `is_enabled = true` 的模型。例外情况必须明确说明：例如本站临时仍在提供兼容模型。

### 11.5 固定价和表达式价不要混用冲突

同一模型如果已经走 `tiered_expr`，不要再依赖 `ModelPrice` 表示实际阶梯价格。否则后续维护者容易误判。

## 12. 建议的 Agent 接手步骤

1. 阅读本文件。
2. 如果涉及表达式计费，阅读 `pkg/billingexpr/expr.md`。
3. 拉取最新上游：更新 `docs/pricing.latest.json`。
4. 拉取当前 NewAPI 配置：更新 `docs/waninter/newapi-pricing-config-current.json`。
5. 只筛 `is_enabled = true` 模型。
6. 对比：
   - 是否新增模型；
   - 是否价格变化；
   - 是否 fixed ↔ tiered 变化；
   - 是否 duration/resolution 倍率变化。
7. 先修改本地快照/记录计划。
8. 再通过 `/api/option/` 更新线上配置。
9. 更新后重新拉取快照，覆盖 `docs/waninter/newapi-pricing-config-current.json`。
10. 用一次真实调用验证扣费。
11. 清理过程文件，只保留准确的当前快照和必要文档。

## 13. 提交前检查

```bash
git status --short
python3 -m json.tool docs/pricing.latest.json >/dev/null
python3 -m json.tool docs/waninter/newapi-pricing-config-current.json >/dev/null
```

如果改了计费代码：

```bash
go test ./relay/helper ./relay ./constant
```

如果改了前端展示：

```bash
cd web/default && bun run build
```

## 14. Grok 视频结果自动转存配置

`grok-image-video` / `grok-video-1.5` 上游可能返回 `https://vidgen.x.ai/...mp4`，部分用户或服务器无法直接访问。NewAPI 支持通过环境变量把命中的远程结果 URL 自动下载并转存到 OSS。

关键配置：

```env
TASK_RESULT_REHOST_ENABLED=true
TASK_RESULT_REHOST_DOMAINS=vidgen.x.ai
TASK_RESULT_REHOST_BACKEND=aliyun_oss
TASK_RESULT_REHOST_ENDPOINT=https://oss-cn-shanghai.aliyuncs.com
TASK_RESULT_REHOST_UPLOAD_ENDPOINT=https://oss-cn-shanghai.aliyuncs.com
TASK_RESULT_REHOST_BUCKET=your-bucket
TASK_RESULT_REHOST_REGION=cn-shanghai
TASK_RESULT_REHOST_PUBLIC_BASE_URL=https://your-bucket.oss-cn-shanghai.aliyuncs.com
TASK_RESULT_REHOST_PREFIX=generated/newapi/videos
TASK_RESULT_REHOST_ACCESS_KEY_ID=***
TASK_RESULT_REHOST_ACCESS_KEY_SECRET=***
TASK_RESULT_REHOST_MAX_MB=512
TASK_RESULT_REHOST_TIMEOUT_SECONDS=180

# 可选：转存下载源文件时专用代理。配置后命中 TASK_RESULT_REHOST_DOMAINS 的下载会优先走该代理；未配置则回退渠道 proxy。
TASK_RESULT_REHOST_PROXY=http://host:port
```

注意：

- 自动转存只在任务轮询到成功时触发。已经完成的历史任务不会自动补转存。
- 如果服务器无法访问 `vidgen.x.ai`，必须配置 `TASK_RESULT_REHOST_PROXY` 或渠道 proxy。
- 成功后会替换任务 `PrivateData.ResultURL`，并把任务 Data 中相同的源 URL 一并替换为 OSS URL，因此 `/v1/videos/{task_id}` 返回应不再出现 `vidgen.x.ai`。

## 15. 已清理/不再跟随的上游 Provider

- `www.vjimeng.vip`：该 provider 已下线/禁用，相关 `sd2-*` 模型不再保留在本地上游价格快照中，也不配置 NewAPI 价格。线上渠道和价格配置当前无残留。
