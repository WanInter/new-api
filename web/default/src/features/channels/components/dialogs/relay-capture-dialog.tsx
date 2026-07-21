/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertCircle, FileText, Loader2, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatTimestampToDate } from '@/lib/format'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { Dialog } from '@/components/dialog'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import {
  getChannelRelayCapturePolicy,
  getRelayCapturePart,
  getRelayCaptures,
  updateChannelRelayCapturePolicy,
} from '../../api'
import { channelsQueryKeys } from '../../lib'
import type { RelayCaptureMetadata, RelayCaptureProtocol } from '../../types'
import { useChannels } from '../channels-provider'

const protocolOptions: Array<{
  value: RelayCaptureProtocol
  label: string
  path: string
}> = [
  {
    value: 'openai.chat_completions',
    label: 'OpenAI Chat Completions',
    path: '/v1/chat/completions',
  },
  {
    value: 'anthropic.messages',
    label: 'Anthropic Messages',
    path: '/v1/messages',
  },
  {
    value: 'openai.responses',
    label: 'OpenAI Responses',
    path: '/v1/responses',
  },
]

type CapturePart = 'request' | 'response'

type RelayCapturePolicyDraft = {
  channelId: number
  enabled: boolean
  protocols: RelayCaptureProtocol[]
}

type RelayCaptureDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function RelayCaptureDialog({
  open,
  onOpenChange,
}: RelayCaptureDialogProps) {
  const { t } = useTranslation()
  const { currentRow } = useChannels()
  const queryClient = useQueryClient()
  const [policyDraft, setPolicyDraft] =
    useState<RelayCapturePolicyDraft | null>(null)
  const [isSaving, setIsSaving] = useState(false)
  const [content, setContent] = useState('')
  const [contentPart, setContentPart] = useState<CapturePart | null>(null)
  const [contentCapture, setContentCapture] =
    useState<RelayCaptureMetadata | null>(null)
  const [isLoadingContent, setIsLoadingContent] = useState(false)

  const policyQuery = useQuery({
    queryKey: ['channel-relay-capture-policy', currentRow?.id],
    queryFn: () => getChannelRelayCapturePolicy(currentRow!.id),
    enabled: open && Boolean(currentRow),
  })
  const capturesQuery = useQuery({
    queryKey: ['relay-captures', currentRow?.id],
    queryFn: () =>
      getRelayCaptures({ channel_id: currentRow!.id, page_size: 20 }),
    enabled: open && Boolean(currentRow),
  })
  const {
    open: verificationOpen,
    methods: verificationMethods,
    state: verificationState,
    executeVerification,
    withVerification,
    cancel: cancelVerification,
    setCode: setVerificationCode,
    switchMethod: switchVerificationMethod,
  } = useSecureVerification()

  if (!currentRow) return null

  const serverPolicy = policyQuery.data?.data
  const currentPolicy: RelayCapturePolicyDraft =
    policyDraft?.channelId === currentRow.id
      ? policyDraft
      : {
          channelId: currentRow.id,
          enabled: serverPolicy?.enabled ?? false,
          protocols: serverPolicy?.protocols ?? [],
        }
  const { enabled, protocols } = currentPolicy

  const resetDraft = () => {
    setPolicyDraft(null)
    setContent('')
    setContentPart(null)
    setContentCapture(null)
  }

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) resetDraft()
    onOpenChange(nextOpen)
  }

  const setProtocolEnabled = (
    protocol: RelayCaptureProtocol,
    checked: boolean
  ) => {
    setPolicyDraft((draft) => {
      const current = draft?.channelId === currentRow.id ? draft : currentPolicy
      return {
        ...current,
        protocols: checked
          ? [...new Set([...current.protocols, protocol])]
          : current.protocols.filter((item) => item !== protocol),
      }
    })
  }

  const handleSave = async () => {
    if (enabled && protocols.length === 0) {
      toast.error(t('Select at least one capture protocol'))
      return
    }
    setIsSaving(true)
    try {
      await withVerification(
        async () => {
          const response = await updateChannelRelayCapturePolicy(
            currentRow.id,
            {
              enabled,
              protocols,
            }
          )
          if (!response.success) {
            throw new Error(
              response.message || t('Failed to update capture policy')
            )
          }
          await queryClient.invalidateQueries({
            queryKey: ['channel-relay-capture-policy', currentRow.id],
          })
          await queryClient.invalidateQueries({
            queryKey: channelsQueryKeys.detail(currentRow.id),
          })
          toast.success(t('Capture policy updated'))
          return response
        },
        {
          preferredMethod: 'passkey',
          title: t('Verify to update relay capture policy'),
          description: t(
            'Use Passkey or 2FA to confirm changes to captured client payloads.'
          ),
        }
      )
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to update capture policy')
      )
    } finally {
      setIsSaving(false)
    }
  }

  const handleViewPart = async (
    metadata: RelayCaptureMetadata,
    part: CapturePart
  ) => {
    setIsLoadingContent(true)
    try {
      const body = await withVerification(
        () => getRelayCapturePart(metadata.id, part),
        {
          preferredMethod: 'passkey',
          title: t('Verify to view captured payload'),
          description: t(
            'Use Passkey or 2FA to view this captured client payload.'
          ),
        }
      )
      if (typeof body === 'string') {
        setContent(body)
        setContentPart(part)
        setContentCapture(metadata)
      }
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to load captured payload')
      )
    } finally {
      setIsLoadingContent(false)
    }
  }

  const captures = capturesQuery.data?.data?.items ?? []

  return (
    <>
      <Dialog
        open={open}
        onOpenChange={handleOpenChange}
        title={t('Relay Capture')}
        description={currentRow.name}
        contentClassName='sm:max-w-5xl'
        contentHeight='min(66vh, 44rem)'
        bodyClassName='space-y-4'
        footer={
          <>
            <Button
              variant='outline'
              onClick={() => handleOpenChange(false)}
              disabled={isSaving}
            >
              {t('Close')}
            </Button>
            <Button
              onClick={handleSave}
              disabled={isSaving || policyQuery.isLoading}
            >
              {isSaving && <Loader2 className='size-4 animate-spin' />}
              {t('Save')}
            </Button>
          </>
        }
      >
        <Tabs defaultValue='policy' className='gap-4'>
          <TabsList>
            <TabsTrigger value='policy'>{t('Capture Policy')}</TabsTrigger>
            <TabsTrigger value='captures'>
              {t('Captured Requests')}
              {capturesQuery.data?.data?.total ? (
                <Badge variant='secondary' className='ml-1'>
                  {capturesQuery.data.data.total}
                </Badge>
              ) : null}
            </TabsTrigger>
          </TabsList>

          <TabsContent value='policy' className='space-y-4'>
            <Alert>
              <AlertCircle />
              <AlertDescription>
                {t(
                  'Only non-streaming JSON and plain-text payloads are eligible for capture.'
                )}
              </AlertDescription>
            </Alert>

            {policyQuery.isLoading ? (
              <div className='space-y-3'>
                <Skeleton className='h-10 w-full' />
                <Skeleton className='h-24 w-full' />
              </div>
            ) : (
              <div className='space-y-4'>
                <div className='flex items-center justify-between gap-4 border-b pb-4'>
                  <div className='space-y-1'>
                    <Label htmlFor='relay-capture-enabled'>
                      {t('Enable Relay Capture')}
                    </Label>
                    <p className='text-muted-foreground text-sm'>
                      {t(
                        'Captured payloads are encrypted at rest and require additional verification to read.'
                      )}
                    </p>
                  </div>
                  <Switch
                    id='relay-capture-enabled'
                    checked={enabled}
                    onCheckedChange={(nextEnabled) => {
                      setPolicyDraft((draft) => {
                        const current =
                          draft?.channelId === currentRow.id
                            ? draft
                            : currentPolicy
                        return { ...current, enabled: nextEnabled }
                      })
                    }}
                    disabled={isSaving}
                  />
                </div>

                <fieldset disabled={!enabled || isSaving} className='space-y-3'>
                  <legend className='text-sm font-medium'>
                    {t('Capture Protocols')}
                  </legend>
                  {protocolOptions.map((protocol) => (
                    <div
                      key={protocol.value}
                      className='flex items-center gap-3 rounded-md border px-3 py-2.5'
                    >
                      <Checkbox
                        id={`relay-capture-${protocol.value}`}
                        checked={protocols.includes(protocol.value)}
                        onCheckedChange={(checked) =>
                          setProtocolEnabled(protocol.value, checked === true)
                        }
                      />
                      <Label
                        htmlFor={`relay-capture-${protocol.value}`}
                        className='min-w-0 flex-1 cursor-pointer justify-between gap-4'
                      >
                        <span>{t(protocol.label)}</span>
                        <code className='text-muted-foreground text-xs'>
                          {protocol.path}
                        </code>
                      </Label>
                    </div>
                  ))}
                </fieldset>
              </div>
            )}
          </TabsContent>

          <TabsContent value='captures' className='space-y-4'>
            <div className='flex items-center justify-between gap-3'>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Captured payloads are encrypted at rest and require additional verification to read.'
                )}
              </p>
              <Button
                variant='outline'
                size='sm'
                onClick={() => capturesQuery.refetch()}
                disabled={capturesQuery.isFetching}
              >
                {capturesQuery.isFetching ? (
                  <Loader2 className='size-4 animate-spin' />
                ) : (
                  <RefreshCw className='size-4' />
                )}
                {t('Refresh')}
              </Button>
            </div>

            {capturesQuery.isLoading ? (
              <div className='space-y-2'>
                <Skeleton className='h-10 w-full' />
                <Skeleton className='h-10 w-full' />
              </div>
            ) : capturesQuery.isError ? (
              <Alert variant='destructive'>
                <AlertCircle />
                <AlertDescription>
                  {t('Failed to load captured requests')}
                </AlertDescription>
              </Alert>
            ) : captures.length === 0 ? (
              <div className='text-muted-foreground grid min-h-28 place-items-center border border-dashed text-sm'>
                {t('No captured requests found')}
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Captured At')}</TableHead>
                    <TableHead>{t('Protocol')}</TableHead>
                    <TableHead>{t('Status')}</TableHead>
                    <TableHead>{t('Outcome')}</TableHead>
                    <TableHead className='text-right'>{t('View')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {captures.map((capture) => (
                    <TableRow key={capture.id}>
                      <TableCell className='text-muted-foreground'>
                        <div>{formatTimestampToDate(capture.created_at)}</div>
                        <div className='max-w-48 truncate text-xs'>
                          {capture.model || '-'}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div>{protocolLabel(capture.protocol, t)}</div>
                        <div className='text-muted-foreground text-xs'>
                          {capture.path}
                        </div>
                      </TableCell>
                      <TableCell>{capture.status_code || '-'}</TableCell>
                      <TableCell>
                        <Badge
                          variant={
                            capture.outcome === 'success'
                              ? 'secondary'
                              : 'destructive'
                          }
                        >
                          {captureOutcomeLabel(capture, t)}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <div className='flex justify-end gap-2'>
                          <Button
                            variant='outline'
                            size='sm'
                            onClick={() => handleViewPart(capture, 'request')}
                            disabled={
                              !capture.request.stored || isLoadingContent
                            }
                          >
                            <FileText className='size-4' />
                            {t('Request')}
                          </Button>
                          <Button
                            variant='outline'
                            size='sm'
                            onClick={() => handleViewPart(capture, 'response')}
                            disabled={
                              !capture.response.stored || isLoadingContent
                            }
                          >
                            <FileText className='size-4' />
                            {t('Response')}
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}

            {contentPart && contentCapture && (
              <section className='space-y-2 border-t pt-4'>
                <div className='flex items-center justify-between gap-3'>
                  <Label>
                    {contentPart === 'request' ? t('Request') : t('Response')}:{' '}
                    {contentCapture.id}
                  </Label>
                  <Button
                    variant='ghost'
                    size='sm'
                    onClick={() => {
                      setContent('')
                      setContentPart(null)
                      setContentCapture(null)
                    }}
                  >
                    {t('Close')}
                  </Button>
                </div>
                <Textarea
                  readOnly
                  value={content}
                  className='min-h-64 resize-y font-mono text-xs leading-5'
                  aria-label={
                    contentPart === 'request' ? t('Request') : t('Response')
                  }
                />
              </section>
            )}
          </TabsContent>
        </Tabs>
      </Dialog>

      <SecureVerificationDialog
        open={verificationOpen}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) cancelVerification()
        }}
        methods={verificationMethods}
        state={verificationState}
        onVerify={(method, code) => executeVerification(method, code)}
        onCancel={cancelVerification}
        onCodeChange={setVerificationCode}
        onMethodChange={switchVerificationMethod}
      />
    </>
  )
}

function protocolLabel(
  protocol: RelayCaptureProtocol,
  t: (key: string) => string
) {
  return t(
    protocolOptions.find((item) => item.value === protocol)?.label || protocol
  )
}

function captureOutcomeLabel(
  capture: RelayCaptureMetadata,
  t: (key: string) => string
) {
  switch (capture.skipped_reason) {
    case 'streaming_not_supported':
      return t('Streaming is not supported')
    case 'request_too_large':
      return t('Request is too large')
    case 'response_too_large':
      return t('Response is too large')
    case 'unsupported_request_content_type':
      return t('Unsupported request content type')
    case 'unsupported_response_content_type':
      return t('Unsupported response content type')
    default:
      return capture.outcome === 'success'
        ? t('Success')
        : capture.outcome === 'error'
          ? t('Error')
          : capture.outcome
  }
}
