import { useCallback, useState } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import { isApiSuccess, queryAlipayPayment, requestAlipayPayment } from '../api'

export interface AlipayPaymentData {
  tradeNo: string
  amount: string
  payMode: string
  payUrl?: string
  qrCode?: string
}

function getErrorMessage(message: string | undefined, data: unknown): string {
  if (typeof data === 'string' && data.trim()) {
    return data
  }

  return message || i18next.t('Payment request failed')
}

export function useAlipayPayment() {
  const [creating, setCreating] = useState(false)
  const [checking, setChecking] = useState(false)

  const createAlipayPayment = useCallback(
    async (topupAmount: number, payMode: string) => {
      setCreating(true)

      try {
        const response = await requestAlipayPayment({
          amount: Math.floor(topupAmount),
          payment_method: 'alipay_direct',
          pay_mode: payMode,
        })

        if (
          isApiSuccess(response) &&
          response.data &&
          typeof response.data === 'object'
        ) {
          const tradeNo =
            'trade_no' in response.data &&
            typeof response.data.trade_no === 'string'
              ? response.data.trade_no
              : ''
          const amount =
            'amount_yuan' in response.data &&
            (typeof response.data.amount_yuan === 'string' ||
              typeof response.data.amount_yuan === 'number')
              ? String(response.data.amount_yuan)
              : ''
          const payUrl =
            'pay_url' in response.data &&
            typeof response.data.pay_url === 'string'
              ? response.data.pay_url
              : undefined
          const qrCode =
            'qr_code' in response.data &&
            typeof response.data.qr_code === 'string'
              ? response.data.qr_code
              : undefined

          if (tradeNo) {
            return {
              tradeNo,
              amount,
              payMode,
              payUrl,
              qrCode,
            } satisfies AlipayPaymentData
          }
        }

        toast.error(getErrorMessage(response.message, response.data))
        return null
      } catch (_error) {
        toast.error(i18next.t('Payment request failed'))
        return null
      } finally {
        setCreating(false)
      }
    },
    []
  )

  const checkAlipayPayment = useCallback(async (tradeNo: string) => {
    setChecking(true)

    try {
      const response = await queryAlipayPayment(tradeNo)

      if (
        isApiSuccess(response) &&
        response.data &&
        typeof response.data === 'object'
      ) {
        const status =
          'status' in response.data && typeof response.data.status === 'string'
            ? response.data.status
            : ''
        if (status) {
          return status
        }
      }

      toast.error(getErrorMessage(response.message, response.data))
      return null
    } catch (_error) {
      toast.error(i18next.t('Payment request failed'))
      return null
    } finally {
      setChecking(false)
    }
  }, [])

  return {
    creating,
    checking,
    createAlipayPayment,
    checkAlipayPayment,
  }
}
