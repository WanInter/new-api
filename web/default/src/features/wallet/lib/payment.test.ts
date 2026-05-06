import { describe, expect, test } from 'bun:test'
import {
  getDefaultPaymentType,
  getMinTopupAmount,
  hasAnyConfigurableTopup,
  isAlipayPayment,
  isWeChatPayment,
} from './payment'

describe('wallet payment helpers', () => {
  test('uses alipay_direct as default payment type when only alipay direct topup is enabled', () => {
    const info = {
      enable_online_topup: false,
      enable_stripe_topup: false,
      pay_methods: [],
      min_topup: 1,
      stripe_min_topup: 1,
      amount_options: [],
      discount: {},
      enable_alipay_topup: true,
      alipay_min_topup: 66,
    } as any

    expect(getDefaultPaymentType(info)).toBe('alipay_direct')
  })

  test('uses wechat_pay as default payment type when only wechat topup is enabled', () => {
    const info = {
      enable_online_topup: false,
      enable_stripe_topup: false,
      pay_methods: [],
      min_topup: 1,
      stripe_min_topup: 1,
      amount_options: [],
      discount: {},
      enable_wechat_topup: true,
      wechat_min_topup: 18,
    } as any

    expect(getDefaultPaymentType(info)).toBe('wechat_pay')
  })

  test('returns alipay minimum topup when only alipay direct topup is enabled', () => {
    const info = {
      enable_online_topup: false,
      enable_stripe_topup: false,
      pay_methods: [],
      min_topup: 1,
      stripe_min_topup: 1,
      amount_options: [],
      discount: {},
      enable_alipay_topup: true,
      alipay_min_topup: 66,
    } as any

    expect(getMinTopupAmount(info)).toBe(66)
  })

  test('returns wechat minimum topup when only wechat topup is enabled', () => {
    const info = {
      enable_online_topup: false,
      enable_stripe_topup: false,
      pay_methods: [],
      min_topup: 1,
      stripe_min_topup: 1,
      amount_options: [],
      discount: {},
      enable_wechat_topup: true,
      wechat_min_topup: 18,
    } as any

    expect(getMinTopupAmount(info)).toBe(18)
  })

  test('recognizes dedicated alipay and wechat payment types', () => {
    expect(isAlipayPayment('alipay_direct')).toBe(true)
    expect(isAlipayPayment('alipay')).toBe(true)
    expect(isWeChatPayment('wechat_pay')).toBe(true)
    expect(isWeChatPayment('wxpay')).toBe(true)
  })

  test('treats alipay direct and wechat topup as configurable gateways', () => {
    expect(
      hasAnyConfigurableTopup({
        enable_online_topup: false,
        enable_stripe_topup: false,
        pay_methods: [],
        min_topup: 1,
        stripe_min_topup: 1,
        amount_options: [],
        discount: {},
        enable_alipay_topup: true,
      } as any)
    ).toBe(true)

    expect(
      hasAnyConfigurableTopup({
        enable_online_topup: false,
        enable_stripe_topup: false,
        pay_methods: [],
        min_topup: 1,
        stripe_min_topup: 1,
        amount_options: [],
        discount: {},
        enable_wechat_topup: true,
      } as any)
    ).toBe(true)
  })
})
