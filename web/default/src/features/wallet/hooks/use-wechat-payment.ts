import { useCallback, useState } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import { isApiSuccess, requestWeChatPayment } from '../api'

export interface WeChatPaymentData {
  codeUrl: string
  tradeNo: string
  amount: string
}

function getErrorMessage(message: string | undefined, data: unknown): string {
  if (typeof data === 'string' && data.trim()) {
    return data
  }

  return message || i18next.t('Payment request failed')
}

export function useWeChatPayment() {
  const [processing, setProcessing] = useState(false)

  const processWeChatPayment = useCallback(async (topupAmount: number) => {
    setProcessing(true)

    try {
      const response = await requestWeChatPayment({
        amount: Math.floor(topupAmount),
        payment_method: 'wechat_pay',
      })

      if (
        isApiSuccess(response) &&
        response.data &&
        typeof response.data === 'object'
      ) {
        const codeUrl =
          'code_url' in response.data &&
          typeof response.data.code_url === 'string'
            ? response.data.code_url
            : ''
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

        if (codeUrl && tradeNo) {
          return {
            codeUrl,
            tradeNo,
            amount,
          } satisfies WeChatPaymentData
        }
      }

      toast.error(getErrorMessage(response.message, response.data))
      return null
    } catch (_error) {
      toast.error(i18next.t('Payment request failed'))
      return null
    } finally {
      setProcessing(false)
    }
  }, [])

  return { processing, processWeChatPayment }
}
