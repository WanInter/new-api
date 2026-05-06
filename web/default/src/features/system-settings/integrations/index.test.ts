import { describe, expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

describe('default integration settings coverage', () => {
  test('includes alipay and wechat non-sensitive fields in defaultIntegrationSettings', () => {
    const source = readFileSync(
      resolve(import.meta.dir, './index.tsx'),
      'utf8'
    )

    expect(source).toContain('AlipayEnabled:')
    expect(source).toContain('AlipaySandbox:')
    expect(source).toContain('AlipayAppID:')
    expect(source).toContain('AlipayUnitPrice:')
    expect(source).toContain('AlipayMinTopUp:')
    expect(source).toContain('AlipayPayMode:')
    expect(source).toContain('AlipayNotifyURL:')
    expect(source).toContain('AlipayReturnURL:')
    expect(source).toContain('AlipaySubscriptionReturnURL:')
    expect(source).toContain('AlipayOrderDescription:')

    expect(source).toContain('WeChatPayEnabled:')
    expect(source).toContain('WeChatPayMchID:')
    expect(source).toContain('WeChatPayAppID:')
    expect(source).toContain('WeChatPayMerchantSerialNo:')
    expect(source).toContain('WeChatPayPublicKeyID:')
    expect(source).toContain('WeChatPayUnitPrice:')
    expect(source).toContain('WeChatPayMinTopUp:')
    expect(source).toContain('WeChatPayNotifyUrl:')
    expect(source).toContain('WeChatPayOrderDescription:')
  })
})
