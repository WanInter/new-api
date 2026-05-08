import { describe, expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const paymentFieldNames = [
  'AlipayEnabled',
  'AlipaySandbox',
  'AlipayAppID',
  'AlipayPrivateKey',
  'AlipayPublicKey',
  'AlipayUnitPrice',
  'AlipayMinTopUp',
  'AlipayPayMode',
  'AlipayNotifyURL',
  'AlipayReturnURL',
  'AlipaySubscriptionReturnURL',
  'AlipayOrderDescription',
  'WeChatPayEnabled',
  'WeChatPayMchID',
  'WeChatPayAppID',
  'WeChatPayAPIv3Key',
  'WeChatPayPrivateKey',
  'WeChatPayMerchantSerialNo',
  'WeChatPayPublicKeyID',
  'WeChatPayPublicKey',
  'WeChatPayUnitPrice',
  'WeChatPayMinTopUp',
  'WeChatPayNotifyUrl',
  'WeChatPayOrderDescription',
]

describe('billing payment settings coverage', () => {
  test('keeps alipay and wechat fields in defaultBillingSettings', () => {
    const source = readFileSync(resolve(import.meta.dir, './index.tsx'), 'utf8')

    for (const fieldName of paymentFieldNames) {
      expect(source).toContain(`${fieldName}:`)
    }
  })

  test('passes alipay and wechat defaults into PaymentSettingsSection', () => {
    const source = readFileSync(
      resolve(import.meta.dir, './section-registry.tsx'),
      'utf8'
    )

    for (const fieldName of paymentFieldNames) {
      expect(source).toContain(`${fieldName}: settings.${fieldName}`)
    }
  })
})
