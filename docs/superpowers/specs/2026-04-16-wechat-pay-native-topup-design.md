# 微信支付 Native 钱包充值设计文档

**日期：** 2026-04-16  
**状态：** 已确认，待实现计划  
**范围：** 仅支持官方微信支付 API v3 Native 扫码支付，业务范围仅覆盖钱包充值，不包含订阅、退款、关单、查单与轮询。

---

## 1. 目标

在现有支付体系中新增一个独立的微信支付渠道，使用 **微信支付官方 API v3** 的 **Native 扫码支付** 模式完成钱包充值闭环：

1. 后台可配置微信支付商户参数；
2. 用户可在现有充值页选择“微信支付”；
3. 后端根据现有动态充值规则计算人民币金额并创建 Native 订单；
4. 前端展示二维码供用户扫码支付；
5. 微信支付异步回调成功后，系统安全验签、解密、校验金额并完成充值入账；
6. 复用现有 `TopUp` 订单模型、订单锁与充值记录体系，保持与现有支付渠道一致的管理体验。

---

## 2. 非目标

本次设计明确不包含以下内容：

- 订阅支付；
- 微信 H5 / JSAPI / APP 支付；
- 退款；
- 关单；
- 主动查单；
- 支付成功后的前端轮询；
- 分账；
- 微信支付专属固定金额档位；
- 微信支付专属折扣/分组规则。

---

## 3. 方案选择

### 3.1 采用方案

采用 **方案 A：独立微信支付渠道，最小侵入接入**。

即：

- 新增独立微信支付配置；
- 新增独立 controller / route / callback；
- 底层新增 `pkg/wechatpay` 适配层；
- 业务上继续复用现有 `TopUp`、订单锁、充值入账逻辑。

### 3.2 不采用的方案

- **统一抽象所有支付 Provider 后再接微信支付**：当前需求范围有限，先重构支付抽象会扩大改动面；
- **只做临时后端能力、不接后台配置与前端设置**：会与现有项目的支付接入方式不一致，后续返工成本更高。

---

## 4. 总体架构

### 4.1 后端模块划分

新增或修改以下模块：

- `setting/payment_wechat.go`
  - 保存微信支付独立配置项；
- `controller/topup_wechat.go`
  - 处理金额预览、Native 下单、回调通知；
- `pkg/wechatpay/`
  - 对官方 Go SDK 做薄封装；
  - 负责 Native 下单、回调验签、资源解密；
- `model/topup.go`
  - 新增 `RechargeWeChatPay` 或等价方法，完成订单成功入账；
- `model/option.go`
  - 将微信支付配置接入 `OptionMap` 初始化与更新；
- `router/api-router.go`
  - 挂载微信支付相关用户接口与回调路由；
- `controller/topup.go`
  - 将微信支付开关与方式注入 `/api/user/topup/info` 返回值；
- 前端设置与充值页
  - 后台新增微信支付设置卡片；
  - 充值页接入“微信支付”方式，并新增二维码弹窗体验。

### 4.2 SDK 选型

底层优先使用 **微信支付官方 Go SDK**：

- 仓库：`github.com/wechatpay-apiv3/wechatpay-go`

项目内通过 `pkg/wechatpay` 包裹一层适配，避免 controller 直接持有 SDK 细节。

理由：

1. 官方 SDK 更贴近官方协议变化；
2. 可减少手写签名/验签/解密出错概率；
3. 项目内部仍保留可测试、可替换的边界。

---

## 5. 配置设计

### 5.1 新增配置项

新增以下 Option 键：

| Key | 含义 |
| --- | --- |
| `WeChatPayEnabled` | 是否启用微信支付 |
| `WeChatPayMchID` | 微信支付商户号 |
| `WeChatPayAppID` | 绑定商户号的 AppID |
| `WeChatPayAPIv3Key` | APIv3 密钥，用于解密回调资源 |
| `WeChatPayPrivateKey` | 商户 API 私钥 PEM，用于请求签名 |
| `WeChatPayMerchantSerialNo` | 商户 API 证书序列号 |
| `WeChatPayPublicKeyID` | 微信支付公钥 ID，用于校验回调头中的 `Wechatpay-Serial` |
| `WeChatPayPublicKey` | 微信支付公钥 PEM，用于验签回调 |
| `WeChatPayUnitPrice` | 微信支付充值单价，单位 CNY 元 |
| `WeChatPayMinTopUp` | 微信支付最小充值数量 |
| `WeChatPayNotifyUrl` | 可选，覆盖默认回调地址 |
| `WeChatPayOrderDescription` | 可选，订单描述，默认“账户充值” |

### 5.2 公钥模式

首版直接使用 **微信支付公钥模式**，不采用平台证书模式。

原因：

- 官方文档当前推荐公钥模式；
- 配置与运维复杂度更低；
- 当前项目更适合将证书轮换复杂度降到最低。

### 5.3 敏感字段回显规则

保持与现有后台一致：

- `GetOptions` 会过滤包含 `Key / Secret / Token` 的字段；
- 因此 `WeChatPayAPIv3Key`、`WeChatPayPrivateKey`、`WeChatPayPublicKey` 默认不回显；
- 后台设置页允许重新提交，但刷新后不显示原值。

---

## 6. 数据模型与订单语义

### 6.1 复用 `TopUp`

不新增微信支付专用订单表，继续复用：

- `model.TopUp`

微信支付订单写入规则：

- `PaymentMethod = "wechat_pay"`
- `TradeNo = "WXPAY-<uid>-<ts>-<rand>"`
- `Status = pending`
- `Money = 实际支付人民币金额（元）`
- `Amount = 归一化后的充值基数`

### 6.2 统一订单语义

微信支付的订单语义对齐 Epay / Waffo：

- `Amount` 表示用于最终入账的充值基数；
- `Money` 用于账单展示与金额校验；
- 入账时按 `Amount * QuotaPerUnit` 增加额度。

这样避免引入 Stripe 当前那种 `Amount` / `Money` 语义不一致的问题。

---

## 7. 金额计算规则

### 7.1 总体原则

完全复用现有动态充值逻辑，只把最终金额结算为微信支付要求的 **CNY 分**。

复用项：

- `QuotaDisplayType`
- `TopupGroupRatio`
- `payment_setting.amount_discount`
- `QuotaPerUnit`

### 7.2 计算公式

设：

- `requestAmount` 为用户输入/选择的原始充值数量；
- `originalAmount = requestAmount`；
- `normalizedAmount` 为展示模式归一化后的金额；
- `groupRatio` 为用户分组充值倍率；
- `discount` 为充值档位折扣；
- `unitPrice = WeChatPayUnitPrice`。

计算规则：

```text
if QuotaDisplayType == TOKENS:
  normalizedAmount = requestAmount / QuotaPerUnit
else:
  normalizedAmount = requestAmount

groupRatio = TopupGroupRatio[user.group]，缺失时按 1
discount = AmountDiscount[originalAmount]，缺失时按 1

payCNY = normalizedAmount * unitPrice * groupRatio * discount
payFen = round_half_up(payCNY * 100)
```

### 7.3 约束

- 若 `requestAmount < WeChatPayMinTopUp`，拒绝；
- 若 `payCNY < 0.01` 或 `payFen < 1`，拒绝；
- 所有金额计算内部使用 `decimal`，禁止直接用 `float64` 参与分值换算。

### 7.4 回调金额校验

回调验签与解密成功后，必须校验：

- `amount.total == localOrderMoneyFen`
- `amount.currency == "CNY"`
- `appid == WeChatPayAppID`
- `mchid == WeChatPayMchID`

只有全部一致才允许入账。

---

## 8. 后端接口设计

### 8.1 用户侧接口

#### `POST /api/user/wechat/amount`

作用：计算微信支付应付金额。

请求体：

```json
{
  "amount": 10
}
```

响应成功示例：

```json
{
  "message": "success",
  "data": "68.00"
}
```

#### `POST /api/user/wechat/pay`

作用：创建本地订单并调用微信 Native 下单接口。

请求体：

```json
{
  "amount": 10,
  "payment_method": "wechat_pay"
}
```

响应成功示例：

```json
{
  "message": "success",
  "data": {
    "trade_no": "WXPAY-1-1710000000000-ABC123",
    "code_url": "weixin://wxpay/bizpayurl?...",
    "amount_yuan": "68.00"
  }
}
```

### 8.2 回调接口

#### `POST /api/wechat/notify`

作用：接收微信支付成功通知，验签、解密并入账。

处理规则：

1. 读取原始请求体；
2. 校验请求头签名；
3. 使用 APIv3 Key 解密 `resource`；
4. 校验回调状态、AppID、商户号、金额、币种；
5. 订单加锁；
6. 事务内完成订单成功与用户加额；
7. 返回 2xx 成功响应。

---

## 9. 下单与回调流程

### 9.1 下单流程

1. 用户进入现有充值页；
2. 前端调用 `/api/user/topup/info` 获取可用支付方式；
3. 用户选择“微信支付”；
4. 前端调用 `/api/user/wechat/amount` 预览实付金额；
5. 用户确认后调用 `/api/user/wechat/pay`；
6. 后端校验配置、金额与用户分组；
7. 后端创建本地 `TopUp` 订单；
8. 后端调微信 Native 下单接口，拿到 `code_url`；
9. 前端展示二维码，等待用户扫码支付。

### 9.2 回调流程

1. 微信服务器调用 `/api/wechat/notify`；
2. 后端验证回调头、签名与公钥 ID；
3. 后端解密 `resource`；
4. 仅处理：
   - `event_type == TRANSACTION.SUCCESS`
   - `trade_type == NATIVE`
   - `trade_state == SUCCESS`
5. 查找本地订单；
6. 校验金额与商户身份；
7. `LockOrder(tradeNo)`；
8. 调 `RechargeWeChatPay` 入账；
9. 返回成功响应。

### 9.3 幂等保证

采用现有两层机制：

- 进程内：`LockOrder/UnlockOrder`
- 数据库事务：`FOR UPDATE`

回调重复投递时，只允许首次成功入账，其余请求返回成功但不重复加额。

---

## 10. 错误处理策略

### 10.1 下单阶段

- 配置未启用：返回“管理员未开启微信支付”；
- 配置不完整：返回“微信支付配置不完整”；
- 金额过低：返回“充值金额过低”；
- 低于最小充值额：返回“充值数量不能小于 X”；
- Native 下单失败：
  - 本地订单标记为 `failed`；
  - 返回“拉起支付失败”。

### 10.2 回调阶段

以下情况返回非 2xx，并保持错误暴露：

- 验签失败；
- 公钥 ID 不匹配；
- 解密失败；
- 订单不存在；
- 金额不一致；
- `appid / mchid` 不一致；
- 数据库事务失败；
- 用户加额失败。

### 10.3 不做静默兜底

若金额或身份校验失败，不自动把订单改为 `failed`，因为这类错误通常意味着配置错误、环境混用或代码缺陷，应显式暴露并等待修复。

---

## 11. 前端设计

### 11.1 后台设置页

新增：

- `web/src/pages/Setting/Payment/SettingsPaymentGatewayWeChat.jsx`

并接入：

- `web/src/components/settings/PaymentSetting.jsx`

页面内容：

- 启用开关；
- 商户号、AppID；
- APIv3 Key；
- 商户私钥；
- 商户证书序列号；
- 微信支付公钥 ID；
- 微信支付公钥；
- 单价、最小充值；
- 回调地址覆盖项；
- 订单描述。

页面交互：

- 顶部 Banner 显示默认回调地址：`${ServerAddress}/api/wechat/notify`；
- PEM 文本使用 TextArea；
- 敏感项刷新后不回显；
- 留空回调地址时使用默认值。

### 11.2 用户充值页

不新增页面，复用：

- `web/src/components/topup/index.jsx`

后端在 `/api/user/topup/info` 中新增返回：

- `enable_wechat_topup`
- `wechat_min_topup`
- 并向 `pay_methods` 注入：

```json
{
  "name": "微信支付",
  "type": "wechat_pay",
  "color": "rgba(var(--semi-green-5), 1)",
  "min_topup": 1
}
```

### 11.3 二维码支付弹窗

前端使用现有 `qrcode.react` 依赖，直接在当前页打开二维码 Modal，不跳转新页、不弹新窗口。

弹窗展示：

- 二维码；
- 应付金额；
- 订单号；
- “请使用微信扫码支付”；
- “支付成功后到账可能有短暂延迟”。

按钮：

- 复制支付链接；
- 查看充值账单；
- 关闭。

### 11.4 v1 不做主动查单

本次不做支付成功轮询，也不做主动查单。用户支付后依赖微信异步通知完成入账，可通过账单记录手动确认结果。

---

## 12. 测试设计

### 12.1 单元测试

至少覆盖：

1. 金额计算：
   - `USD / CNY / TOKENS` 三种展示模式；
   - `TopupGroupRatio`；
   - `AmountDiscount`；
   - 四舍五入到分；
   - 小于 1 分拒绝。
2. 配置校验：
   - 缺商户号；
   - 缺 AppID；
   - 缺 APIv3 Key；
   - 缺私钥；
   - 缺商户序列号；
   - 缺微信支付公钥 ID / 公钥。
3. 回调处理：
   - 重复回调幂等；
   - 订单不存在；
   - 金额不一致；
   - 状态非成功；
   - 正常成功入账。

### 12.2 控制器测试

至少覆盖：

- `POST /api/user/wechat/amount`
- `POST /api/user/wechat/pay`
- `POST /api/wechat/notify`

验证点：

- 参数错误；
- 未启用；
- 配置不完整；
- 最小充值限制；
- 下单成功返回 `code_url`；
- 回调成功后订单状态更新与用户额度增加。

### 12.3 测试隔离

`pkg/wechatpay` 暴露内部接口，例如：

- `CreateNativeOrder(...)`
- `VerifyAndDecryptNotify(...)`

业务层测试使用 fake/mock 替代真实 SDK，避免测试依赖真实密钥材料与外部网络。

---

## 13. 文件影响清单

### 新增文件

- `setting/payment_wechat.go`
- `controller/topup_wechat.go`
- `pkg/wechatpay/client.go`
- `pkg/wechatpay/notify.go`
- `web/src/pages/Setting/Payment/SettingsPaymentGatewayWeChat.jsx`
- 相关 Go / 前端测试文件

### 修改文件

- `router/api-router.go`
- `model/option.go`
- `model/topup.go`
- `controller/topup.go`
- `web/src/components/settings/PaymentSetting.jsx`
- `web/src/components/topup/index.jsx`
- `web/src/i18n/locales/*.json`

---

## 14. 风险与约束

1. 微信支付金额以 **CNY 分** 为准，必须严格做整数分校验；
2. 公钥模式要求正确配置微信支付公钥 ID 与公钥内容，配置错误会直接导致回调失败；
3. 本次不做主动查单，因此所有到账状态依赖异步通知；
4. 首版不引入更大范围的支付抽象重构，避免影响现有渠道稳定性。

---

## 15. 验收标准

实现完成后，以下条件同时满足才算通过：

1. 后台可保存并更新微信支付配置；
2. `/api/user/topup/info` 能正确暴露微信支付启用状态与最小充值额；
3. 用户可在充值页看到“微信支付”；
4. 用户发起充值后能得到 `code_url` 并看到二维码；
5. 微信支付成功回调后，订单仅入账一次；
6. 账单记录中可看到 `wechat_pay` 充值订单；
7. 金额不一致、验签失败等异常场景不会静默成功；
8. 自动化测试覆盖核心金额计算、下单、回调与幂等路径。

---

## 16. 参考资料

- Native 下单：https://pay.wechatpay.cn/doc/v3/merchant/4012791877
- APIv3 概述：https://pay.wechatpay.cn/doc/v3/merchant/4012081606
- 开发必要参数说明：https://pay.wechatpay.cn/doc/v3/merchant/4013070756
- 微信支付公钥验签说明：https://pay.wechatpay.cn/doc/v3/merchant/4013053249
- 官方 Go SDK：https://github.com/wechatpay-apiv3/wechatpay-go
