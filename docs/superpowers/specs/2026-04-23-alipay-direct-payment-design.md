# 支付宝官方直连支付设计文档

**日期：** 2026-04-23  
**状态：** 已确认，待实现计划  
**范围：** 为现有项目新增支付宝官方直连支付渠道，覆盖钱包充值与订阅购买；首版同时支持电脑网站支付与扫码预下单，支持异步通知与主动查单；不包含退款、关单与统一支付抽象重构。

---

## 1. 目标

在现有支付体系中新增一个**独立的支付宝官方直连渠道**，并满足以下业务目标：

1. 后台可配置支付宝官方直连参数；
2. 用户可在现有充值页使用支付宝完成钱包充值；
3. 用户可在现有订阅购买流程中使用支付宝完成套餐购买；
4. 同一官方渠道内同时支持：
   - **电脑网站支付**（跳转支付宝收银台）；
   - **扫码支付**（后端预下单，前端展示二维码）；
5. 支付结果以**异步通知为主**，并提供**主动查单**补偿；
6. 复用现有 `TopUp`、`SubscriptionOrder`、订单锁、日志与后台设置体系；
7. 在不影响现有 Stripe / Creem / Waffo / WeChat / EPay 行为的前提下，以最小侵入方式落地。

---

## 2. 非目标

本次设计明确不包含以下内容：

- 退款；
- 关单；
- 统一重构所有支付 Provider 抽象；
- 多币种结算；
- 支付宝 APP 支付、小程序支付、当面付条码支付；
- 支付宝专属固定金额档位；
- 支付宝专属折扣/分组规则；
- 自动兜底切回 EPay 支付宝；
- 静默降级、假成功、模拟成功路径。

---

## 3. 方案选择

### 3.1 采用方案

采用 **方案 A：新增独立“官方支付宝”渠道，在单渠道内支持 page + qr 两种支付模式**。

核心原则：

- 渠道层：新增 `alipay_direct`；
- 模式层：通过 `payment_mode=page|qr` 区分支付形态；
- 订单层：充值与订阅分别复用现有订单表；
- 结果层：异步通知主导，主动查单补偿；
- 展示层：官方启用时优先展示官方支付宝，不再向前端暴露 EPay 的 `alipay`。

### 3.2 不采用的方案

#### 方案 B：拆成两个渠道 `alipay_page` / `alipay_qr`

不采用原因：

- 用户会看到两个支付宝入口，产品语义差；
- 后端账单、配置、统计都会更分裂；
- 与当前微信支付独立渠道风格不一致。

#### 方案 C：先统一抽象所有支付 Provider 再接支付宝

不采用原因：

- 当前目标是新增一个高优先级支付渠道，而不是支付子系统重构；
- 改动面过大，容易影响现有支付稳定性；
- 会拖慢充值与订阅双场景落地。

---

## 4. 总体架构

### 4.1 后端模块划分

新增或修改以下模块：

- `setting/payment_alipay.go`
  - 保存支付宝官方直连运行时配置；
- `pkg/alipay/`
  - 统一封装支付宝 page pay、precreate、trade query、通知验签能力；
- `controller/topup_alipay.go`
  - 处理钱包充值金额预览、下单、查单、回调；
- `controller/subscription_payment_alipay.go`
  - 处理订阅下单、查单、回调；
- `controller/topup.go`
  - 在 `/api/user/topup/info` 中注入支付宝官方直连能力，并在官方启用时隐藏 EPay 的 `alipay`；
- `model/topup.go`
  - 新增 `RechargeAlipay` 或等价方法，完成充值入账；
- `model/subscription.go`
  - 扩展 `SubscriptionOrder` 订单语义，支持支付宝官方直连模式；
- `model/option.go`
  - 将支付宝配置接入 `OptionMap` 初始化与更新；
- `router/api-router.go`
  - 挂载充值、订阅、回调、查单相关路由；
- 前端设置与支付页
  - 后台新增支付宝设置卡片；
  - 充值页与订阅购买页增加支付宝官方直连入口；
  - 增加二维码弹窗、回跳与轮询查单逻辑。

### 4.2 SDK/适配层选择

项目内部通过 `pkg/alipay` 包裹第三方支付宝 Go 客户端，controller 不直接依赖第三方库细节。

实现建议：

- 首选 `github.com/smartwalle/alipay/v3` 作为底层 SDK；
- 只在 `pkg/alipay` 内暴露项目自己的接口与 DTO；
- 后续如需替换为其他库，仅调整 `pkg/alipay`。

说明：

- 这是工程实现建议，不是支付宝官方 SDK 背书；
- 选择依据是其已覆盖本次所需的 `TradePagePay`、`TradePreCreate`、`TradeQuery` 与通知验签能力；
- 支付协议与字段语义仍以支付宝开放平台官方文档为准。

---

## 5. 官方能力映射

本次设计对应的支付宝开放平台能力如下：

| 能力 | 官方接口 | 用途 |
| --- | --- | --- |
| 电脑网站支付 | `alipay.trade.page.pay` | 收银台跳转支付 |
| 扫码预下单 | `alipay.trade.precreate` | 返回二维码内容给前端展示 |
| 主动查单 | `alipay.trade.query` | 支付补偿查询与状态刷新 |
| 异步通知 | 支付宝支付结果异步通知 | 最终状态入账与幂等处理 |

支付结果判断规则：

- **不能只依赖同步回跳**；
- 以**异步通知**为主；
- 异步通知未及时到达时，通过**主动查单**补偿；
- 充值/订阅成功入账只能发生一次。

---

## 6. 配置设计

### 6.1 新增配置项

新增以下 Option 键：

| Key | 含义 |
| --- | --- |
| `AlipayEnabled` | 是否启用支付宝官方直连 |
| `AlipaySandbox` | 是否启用支付宝沙箱 |
| `AlipayAppID` | 支付宝应用 AppID |
| `AlipayPrivateKey` | 应用私钥（RSA2） |
| `AlipayPublicKey` | 支付宝公钥 |
| `AlipayUnitPrice` | 钱包充值单价，单位 CNY 元 |
| `AlipayMinTopUp` | 钱包充值最小数量 |
| `AlipayNotifyURL` | 可选，覆盖默认异步通知地址 |
| `AlipayReturnURL` | 可选，充值收银台回跳地址 |
| `AlipaySubscriptionReturnURL` | 可选，订阅收银台回跳地址 |
| `AlipayOrderDescription` | 订单描述默认值 |

### 6.2 密钥模式

首版采用 **普通公钥模式**：

- `AppID`
- 应用私钥
- 支付宝公钥

不采用证书模式的原因：

- 首版目标是尽快稳定落地；
- 配置项更少，后台设置更直观；
- 适合当前项目最小侵入接入。

### 6.3 敏感字段回显规则

保持与现有后台一致：

- 含 `Key / Secret / Token` 的字段不回显；
- `AlipayPrivateKey` 默认不回显；
- `AlipayPublicKey` 是否回显可按当前后端脱敏规则统一处理；
- 后台设置页支持重新提交敏感值，但刷新后不展示旧值。

---

## 7. 渠道标识与兼容策略

### 7.1 内部渠道类型

为避免与现有 EPay 聚合的历史 `alipay` 冲突，内部约定如下：

- 保留现有 EPay 支付宝：`alipay`
- 新增官方直连支付宝：`alipay_direct`

### 7.2 展示策略

前端展示规则：

- 仅官方直连启用：显示 **支付宝**；
- 仅 EPay 支付宝启用：显示 **支付宝**；
- 两者都启用：
  - 默认只向前端展示 **支付宝（官方）**；
  - EPay 的 `alipay` 不在前端支付方式中显示。

这符合已确认的策略：**官方优先**。

### 7.3 支付模式

官方直连支付宝统一使用一个支付渠道 `alipay_direct`，通过独立字段区分模式：

- `payment_mode = "page"`
- `payment_mode = "qr"`

含义：

- `payment_method` 表示“支付渠道”；
- `payment_mode` 表示“渠道下的具体支付形态”。

### 7.4 订单隔离

所有支付宝官方直连订单都必须满足：

- `payment_method = alipay_direct`
- 查单、回调、入账时按 `trade_no + payment_method` 双条件识别；
- 不允许把 EPay 的 `alipay` 订单与官方直连订单混用。

---

## 8. 前端交互设计

### 8.1 钱包充值入口

用户在现有充值页看到一个主按钮：

- **支付宝**

点击后进入确认弹窗，默认展示：

- 推荐模式：**支付宝收银台支付**；
- 可切换模式：**扫码支付**。

也就是：**默认收银台 + 可切换扫码**。

### 8.2 订阅购买入口

订阅购买弹窗中新增支付宝官方入口：

- 默认模式：收银台支付；
- 可切换模式：扫码支付；
- 后端根据 `plan_id + pay_mode` 创建订阅订单。

### 8.3 支付发起后的交互

#### `pay_mode=page`

- 后端返回 `pay_url`；
- 前端新窗口打开支付宝收银台；
- 用户支付后返回控制台页面；
- 回跳后自动触发一次查单。

#### `pay_mode=qr`

- 后端返回二维码内容（`qr_code` 或等价格式）；
- 前端展示二维码弹窗；
- 弹窗内启动自动轮询查单；
- 保留按钮：**我已完成支付，检查状态**。

### 8.4 主动查单策略

采用：**自动轮询 + 手动补查**。

触发场景：

- 二维码弹窗打开后自动轮询；
- 收银台支付回跳后自动查一次；
- 用户点击“我已完成支付，检查状态”时手动查；
- 前端账单页如需刷新，也可复用相同查单接口。

查单结果表现：

- 成功：关闭弹窗/刷新页面数据/提示成功；
- pending：显示“等待支付中”；
- fail：显示明确错误，不静默忽略。

---

## 9. 后端接口设计

### 9.1 钱包充值接口

建议新增：

- `POST /api/user/alipay/amount`
  - 计算钱包充值金额；
- `POST /api/user/alipay/pay`
  - 创建钱包充值订单；
- `POST /api/user/alipay/query`
  - 查询钱包充值订单状态；
- `POST /api/alipay/notify`
  - 支付宝异步通知。

#### `/api/user/alipay/pay` 请求体

```json
{
  "amount": 10,
  "payment_method": "alipay_direct",
  "pay_mode": "page"
}
```

其中：

- `payment_method` 固定为 `alipay_direct`；
- `pay_mode` 只允许 `page` / `qr`。

### 9.2 订阅支付接口

建议新增：

- `POST /api/subscription/alipay/pay`
- `POST /api/subscription/alipay/query`
- `POST /api/subscription/alipay/notify`

#### `/api/subscription/alipay/pay` 请求体

```json
{
  "plan_id": 1,
  "payment_method": "alipay_direct",
  "pay_mode": "qr"
}
```

### 9.3 查单接口返回语义

查单接口统一返回：

- `success`：本地订单已完成并已入账；
- `pending`：支付宝侧仍未支付；
- `failed`：查询失败、签名异常、金额异常或订单状态不合法。

后端查单流程：

1. 校验订单归属；
2. 校验 `payment_method=alipay_direct`；
3. 调用 `alipay.trade.query`；
4. 若支付宝明确成功，则走与异步通知同样的完成逻辑；
5. 若未支付则返回 pending；
6. 若订单关闭、失败、金额不一致或字段异常，则返回错误。

---

## 10. 数据模型设计

### 10.1 `TopUp` 扩展

建议新增字段：

- `PaymentMode string`
- `ProviderPayload string`

钱包充值订单示例：

```text
payment_method = alipay_direct
payment_mode   = page | qr
trade_no       = ALIPAY-TOPUP-<userId>-<timestamp>-<random>
money          = 支付宝订单金额（CNY 元）
amount         = 标准化后的充值数量
status         = pending / success / failed
```

### 10.2 `SubscriptionOrder` 扩展

`SubscriptionOrder` 已有 `ProviderPayload`，建议新增：

- `PaymentMode string`

订阅订单示例：

```text
payment_method = alipay_direct
payment_mode   = page | qr
trade_no       = ALIPAY-SUB-<userId>-<timestamp>-<random>
money          = plan.PriceAmount
status         = pending / success / failed
```

### 10.3 数据库兼容

本次仅做最小增量：

- `TopUp` 新增 `payment_mode`、`provider_payload`；
- `SubscriptionOrder` 新增 `payment_mode`；
- 老数据允许默认空值；
- 所有迁移必须兼容 SQLite / MySQL / PostgreSQL。

---

## 11. 金额计算与校验规则

### 11.1 钱包充值金额计算

钱包充值金额计算应与当前微信支付逻辑保持一致：

1. 读取用户输入的原始充值数量 `requestAmount`；
2. 若 `QuotaDisplayType = TOKENS`，先用 `QuotaPerUnit` 归一化；
3. 叠加用户分组倍率 `TopupGroupRatio`；
4. 叠加档位折扣 `payment_setting.amount_discount`；
5. 使用 `AlipayUnitPrice` 计算最终 CNY 金额；
6. 最终保留两位小数；
7. 所有内部运算使用 `decimal`，禁止裸 `float64` 做金额分值换算。

### 11.2 钱包充值校验

必须满足：

- `requestAmount >= AlipayMinTopUp`；
- 实付金额 `>= 0.01` 元；
- 本地金额与支付宝返回/通知金额精确一致到分。

### 11.3 订阅金额校验

订阅金额规则：

- 本地订单金额直接取 `plan.PriceAmount`；
- 首版统一按 CNY 处理，不引入多币种折算；
- 支付宝返回金额必须与 `plan.PriceAmount` 精确一致到分。

### 11.4 异步通知/查单金额校验

通知或查单成功后，必须校验：

- `out_trade_no` 对应本地订单存在；
- `payment_method = alipay_direct`；
- `total_amount == local order money`；
- 订单状态允许从 `pending` 迁移到 `success`；
- 验签通过。

任何一项不满足，都必须直接拒绝处理。

---

## 12. 订单完成与幂等设计

### 12.1 钱包充值入账

新增：

- `model.RechargeAlipay(tradeNo string, providerPayload string) error`

要求：

1. 使用事务；
2. 对订单加行级锁；
3. 订单已成功则幂等返回成功；
4. 订单非 `pending` 则返回明确错误；
5. 校验 `payment_method = alipay_direct`；
6. 充值成功后给用户增加 quota；
7. 保存支付宝通知/查单原文到 `provider_payload`；
8. 事务外记录日志。

### 12.2 订阅订单完成

继续复用：

- `model.CompleteSubscriptionOrder(...)`

但需要扩展：

- 保存 `payment_mode`；
- 保存支付宝通知/查单原文；
- 确保 `payment_method = alipay_direct` 订阅链路可正确入账。

### 12.3 可信来源

支付成功完成条件：

- 支付宝异步通知验签通过，**或** 主动查单返回可信成功结果；
- 金额与本地订单一致；
- 本地订单当前状态为 `pending` 或已成功；
- 幂等逻辑保证只入账一次。

---

## 13. 回调与查单处理策略

### 13.1 异步通知

异步通知是主路径：

1. 读取支付宝通知参数；
2. 验签；
3. 校验业务状态；
4. 按 `trade_no` 找本地订单；
5. 校验金额；
6. 上锁；
7. 调用充值或订阅完成逻辑；
8. 返回支付宝要求的成功响应。

### 13.2 主动查单

主动查单是补偿路径，不是主路径替代品。

适用场景：

- 用户已支付但通知未及时到达；
- 页面跳转回来需要即时刷新状态；
- 二维码弹窗需要实时反馈。

查单结果映射：

- 已支付成功：执行本地完成逻辑；
- 未支付：返回 pending；
- 已关闭/失败：返回 failed，并可按业务需要标记本地订单失败；
- 查询异常：直接报错。

---

## 14. 后台设置页设计

新增：

- `web/src/pages/Setting/Payment/SettingsPaymentGatewayAlipay.jsx`

### 14.1 展示内容

1. 开关
   - 启用支付宝官方直连
   - 沙箱模式

2. 基础参数
   - AppID
   - 应用私钥
   - 支付宝公钥

3. 业务参数
   - 单价
   - 最小充值数量
   - 订单描述

4. 回调参数
   - Notify URL
   - Return URL
   - Subscription Return URL

### 14.2 配置生效路径

要接入以下位置：

- `model/option.go`
  - `InitOptionMap`
  - `updateOptionMap`
- `web/src/components/settings/PaymentSetting.jsx`
  - 拉取并分发支付宝设置卡片
- `/api/user/topup/info`
  - 暴露：
    - `enable_alipay_topup`
    - `alipay_min_topup`
    - `pay_methods` 中注入 `alipay_direct`

---

## 15. 错误处理原则

遵循项目既有规则：**不做静默降级，不做假成功，不吞错误。**

### 15.1 明确错误

- 配置未启用：返回“管理员未开启支付宝支付”；
- 配置不完整：返回“支付宝支付配置不完整”；
- 低于最小充值：返回明确错误；
- 下单失败：直接报错，并把本地订单标记为 `failed`；
- 回调验签失败：直接拒绝；
- 查单失败：直接返回错误，不伪装成 pending；
- 金额不一致：直接拒绝；
- 订单状态非法：直接返回错误。

### 15.2 状态迁移建议

- 下单成功：`pending`
- 下单失败：`failed`
- 查单未支付：保持 `pending`
- 查单/通知成功：`success`
- 查单返回关闭/失败：可转 `failed`

---

## 16. 测试设计

参考现有微信支付实现，测试分为四层。

### 16.1 `pkg` 层

建议新增：

- `pkg/alipay/client_test.go`

覆盖：

- `page.pay` 参数构造；
- `precreate` 参数构造；
- `trade.query` 调用参数；
- 通知验签与字段解析；
- 沙箱/正式环境切换。

### 16.2 `controller` 层

建议新增：

- `controller/topup_alipay_test.go`
- `controller/subscription_payment_alipay_test.go`

覆盖：

- 金额预览；
- `page` 模式下单；
- `qr` 模式下单；
- `/api/user/topup/info` 的官方优先逻辑；
- 查单成功 / 未支付 / 金额不一致。

### 16.3 回调测试

建议新增：

- `controller/topup_alipay_notify_test.go`
- `controller/subscription_payment_alipay_notify_test.go`

覆盖：

- 验签成功入账；
- 重复回调幂等；
- 非 `alipay_direct` 订单拒绝；
- 金额不一致拒绝。

### 16.4 `model` 层

建议新增：

- `model/topup_alipay_test.go`

覆盖：

- `RechargeAlipay` 正常入账；
- 幂等；
- 事务一致性；
- 额度只增加一次。

---

## 17. 迁移与上线策略

### 17.1 数据迁移

仅做最小增量字段扩展，不调整现有支付渠道结构。

### 17.2 上线顺序

建议按以下顺序推进实现与验证：

1. 后台设置页与配置项；
2. 钱包充值下单与回调；
3. 钱包充值主动查单；
4. 订阅下单与回调；
5. 订阅主动查单；
6. 前端回跳与二维码轮询联动；
7. 账单展示与管理端兼容收尾。

### 17.3 与 EPay 共存策略

- 官方支付宝启用后，前端不显示 EPay 的 `alipay`；
- EPay 相关后端代码保留，不做破坏性删除；
- 老订单、老账单继续按历史 `payment_method` 正常展示。

---

## 18. 验收标准

以下条件同时满足才算通过：

1. 后台可保存并更新支付宝官方直连配置；
2. `/api/user/topup/info` 能正确暴露 `alipay_direct`；
3. 官方启用时，前端不再显示 EPay 的 `alipay`；
4. 钱包充值支持 `page` 与 `qr` 两种模式；
5. 订阅购买支持 `page` 与 `qr` 两种模式；
6. 异步通知成功后订单仅入账一次；
7. 主动查单可补偿回调前的状态刷新；
8. 金额不一致、验签失败、订单状态错误不会静默成功；
9. 自动化测试覆盖金额、下单、回调、查单、幂等等核心路径。

---

## 19. 参考资料

### 官方文档

- 支付宝电脑网站支付产品介绍：
  - https://developer.alibaba.com/docs/doc.htm?articleId=105898&docType=1&treeId=270
- `alipay.trade.page.pay`：
  - https://developer.alibaba.com/docs/doc.htm?articleId=105901&docType=1&source=search&treeId=237
- `alipay.trade.precreate`：
  - https://developer.alibaba.com/docs/doc.htm?articleId=109388&docType=1&treeId=568
- `alipay.trade.query`：
  - https://developer.alibaba.com/docs/api.htm?apiId=757&docType=4
- 支付结果异步通知：
  - https://developer.alibaba.com/docs/doc.htm?articleId=105902&docType=1&treeId=193

### 工程参考

- 现有微信支付设计文档：
  - `docs/superpowers/specs/2026-04-16-wechat-pay-native-topup-design.md`
- 建议的 Go SDK（工程实现建议，非官方背书）：
  - `github.com/smartwalle/alipay/v3`

