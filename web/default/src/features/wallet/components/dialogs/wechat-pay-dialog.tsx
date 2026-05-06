import { QRCodeSVG } from 'qrcode.react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

interface WeChatPayDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  codeUrl: string
  amount: string
  tradeNo: string
  onOpenBilling?: () => void
}

export function WeChatPayDialog({
  open,
  onOpenChange,
  codeUrl,
  amount,
  tradeNo,
  onOpenBilling,
}: WeChatPayDialogProps) {
  const { t } = useTranslation()

  const handleCopy = async () => {
    if (!codeUrl) {
      return
    }
    await copyToClipboard(codeUrl)
    toast.success(t('Copied to clipboard'))
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('WeChat QR Payment')}</DialogTitle>
          <DialogDescription>
            {t('Scan the QR code with WeChat to complete the payment')}
          </DialogDescription>
        </DialogHeader>

        <div className='flex flex-col items-center gap-4 py-2'>
          <div className='rounded-lg border bg-white p-4'>
            <QRCodeSVG value={codeUrl || ''} size={220} />
          </div>
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

        <DialogFooter className='grid grid-cols-1 gap-2 sm:grid-cols-3'>
          <Button variant='outline' onClick={handleCopy}>
            {t('Copy Payment Link')}
          </Button>
          <Button
            variant='outline'
            onClick={() => {
              onOpenBilling?.()
              onOpenChange(false)
            }}
          >
            {t('Order History')}
          </Button>
          <Button onClick={() => onOpenChange(false)}>{t('Close')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
