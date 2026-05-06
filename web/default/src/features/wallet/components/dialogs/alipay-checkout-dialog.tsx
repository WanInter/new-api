import { Loader2 } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

interface AlipayCheckoutDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  amount: string
  tradeNo: string
  qrCode?: string
  checking?: boolean
  onCheck: () => void
}

export function AlipayCheckoutDialog({
  open,
  onOpenChange,
  amount,
  tradeNo,
  qrCode,
  checking = false,
  onCheck,
}: AlipayCheckoutDialogProps) {
  const { t } = useTranslation()

  return (
    <Dialog open={open} onOpenChange={checking ? undefined : onOpenChange}>
      <DialogContent className='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Alipay Checkout')}</DialogTitle>
          <DialogDescription>
            {t(
              'Scan the QR code in Alipay and check the payment status after paying'
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='flex flex-col items-center gap-4 py-2'>
          {qrCode ? (
            <div className='rounded-lg border bg-white p-4'>
              <QRCodeSVG value={qrCode} size={220} />
            </div>
          ) : null}
          <div className='w-full space-y-2 text-sm'>
            <div className='flex items-center justify-between gap-3'>
              <span className='text-muted-foreground'>{t('Trade No')}</span>
              <span className='font-mono text-xs'>{tradeNo}</span>
            </div>
            <div className='flex items-center justify-between gap-3'>
              <span className='text-muted-foreground'>{t('Amount')}</span>
              <span className='font-medium'>¥{amount}</span>
            </div>
          </div>
        </div>

        <DialogFooter className='grid grid-cols-2 gap-2'>
          <Button
            variant='outline'
            disabled={checking}
            onClick={() => onOpenChange(false)}
          >
            {t('Close')}
          </Button>
          <Button onClick={onCheck} disabled={checking}>
            {checking && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
            {t('I have paid, check status')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
