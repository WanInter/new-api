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
*/
import { useEffect, useMemo, useState } from 'react'
import { CircleAlert, CircleCheck, LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  areCanonicalRatesComplete,
  buildCanonicalVideoExpr,
  getCanonicalVideoBlockReason,
  getCanonicalVideoDimensions,
  isCanonicalRateDraft,
  isCanonicalVideoAvailable,
  type CanonicalVideoRates,
} from './canonical-video-billing'
import type { BillingCapability } from './model-billing-api'

type CanonicalVideoBillingEditorProps = {
  capability: BillingCapability | null
  capabilityError: string
  isLoading: boolean
  enabled: boolean
  rates: CanonicalVideoRates
  onEnabledChange: (enabled: boolean) => void
  onRatesChange: (rates: CanonicalVideoRates) => void
}

function SchemaSummary({ capability }: { capability: BillingCapability }) {
  const { t } = useTranslation()
  const { durationField, resolutionField } =
    getCanonicalVideoDimensions(capability)

  return (
    <div className='bg-muted/30 space-y-3 rounded-md border p-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='text-sm font-medium'>
          {t('Canonical billing schema')}
        </div>
        {capability.schema_version && (
          <Badge variant='outline' className='font-mono text-xs'>
            {t('Canonical schema version')}: {capability.schema_version}
          </Badge>
        )}
      </div>

      <div className='space-y-1.5'>
        <div className='text-muted-foreground text-xs'>
          {t('Required canonical fields')}
        </div>
        <div className='flex flex-wrap gap-1.5'>
          {[durationField, resolutionField].filter(Boolean).map((field) => (
            <Badge
              key={field!.path}
              variant='secondary'
              className='font-mono text-xs'
            >
              {field!.path}
            </Badge>
          ))}
        </div>
      </div>

      <div className='grid gap-3 md:grid-cols-2'>
        <div className='space-y-1.5'>
          <div className='text-muted-foreground text-xs'>
            {t('Compatible channels')}
          </div>
          {capability.compatible_channels.length > 0 ? (
            <div className='flex flex-wrap gap-1.5'>
              {capability.compatible_channels.map((channel) => (
                <Badge
                  key={channel.id}
                  variant='outline'
                  className='max-w-full truncate'
                >
                  {channel.name}
                </Badge>
              ))}
            </div>
          ) : (
            <div className='text-muted-foreground text-xs'>
              {t('No compatible channels')}
            </div>
          )}
        </div>
        <div className='space-y-1.5'>
          <div className='text-muted-foreground text-xs'>
            {t('Incompatible channels')}
          </div>
          {capability.incompatible_channels.length > 0 ? (
            <div className='space-y-1'>
              {capability.incompatible_channels.map((channel) => (
                <div
                  key={channel.id}
                  className='text-destructive text-xs break-words'
                >
                  {channel.name}: {channel.reason}
                </div>
              ))}
            </div>
          ) : (
            <div className='text-muted-foreground text-xs'>-</div>
          )}
        </div>
      </div>
      {capability.checked_at > 0 && (
        <div className='text-muted-foreground text-xs'>
          {t('Last validation')}: {new Date(capability.checked_at * 1000).toLocaleString()}
        </div>
      )}
    </div>
  )
}

export function CanonicalVideoBillingEditor({
  capability,
  capabilityError,
  isLoading,
  enabled,
  rates,
  onEnabledChange,
  onRatesChange,
}: CanonicalVideoBillingEditorProps) {
  const { t } = useTranslation()
  const { durations, resolutions } = useMemo(
    () => getCanonicalVideoDimensions(capability),
    [capability]
  )
  const [previewDuration, setPreviewDuration] = useState('')
  const available = isCanonicalVideoAvailable(capability)
  const blockReason = getCanonicalVideoBlockReason(capability)

  useEffect(() => {
    if (durations.length === 0) {
      setPreviewDuration('')
      return
    }
    setPreviewDuration((current) =>
      durations.some((duration) => String(duration) === current)
        ? current
        : String(durations[durations.length - 1])
    )
  }, [durations])

  const expression = useMemo(
    () => (capability ? buildCanonicalVideoExpr(capability, rates) : ''),
    [capability, rates]
  )
  const configured = areCanonicalRatesComplete(rates, resolutions)
  const previewDurationNumber = Number(previewDuration)

  const changeRate = (resolution: string, value: string) => {
    if (!isCanonicalRateDraft(value)) return
    onRatesChange({ ...rates, [resolution]: value })
  }

  if (isLoading) {
    return (
      <Alert>
        <LoaderCircle className='animate-spin' />
        <AlertTitle>{t('Canonical video billing')}</AlertTitle>
        <AlertDescription>
          {t('Loading billing capability...')}
        </AlertDescription>
      </Alert>
    )
  }

  if (capabilityError) {
    return (
      <Alert variant='destructive'>
        <CircleAlert />
        <AlertTitle>{t('Canonical video billing')}</AlertTitle>
        <AlertDescription>
          {t(
            'Unable to load billing capability. Generic expressions remain available.'
          )}
          <span className='mt-1 block text-xs'>{capabilityError}</span>
        </AlertDescription>
      </Alert>
    )
  }

  if (!capability) return null

  if (!available) {
    return (
      <div className='space-y-3'>
        <Alert variant='destructive'>
          <CircleAlert />
          <AlertTitle>
            {t('Canonical video billing is unavailable for this model.')}
          </AlertTitle>
          <AlertDescription>
            {blockReason === 'canonical_unavailable'
              ? t(
                  'Every routed channel must use the same canonical schema before dynamic video pricing can be enabled.'
                )
              : blockReason === 'canonical_schema_incomplete'
                ? t(
                    'Every routed channel must use the same canonical schema before dynamic video pricing can be enabled.'
                  )
                : blockReason}
          </AlertDescription>
        </Alert>
        <SchemaSummary capability={capability} />
      </div>
    )
  }

  return (
    <div className='space-y-3'>
      <SchemaSummary capability={capability} />

      <div className='flex flex-wrap gap-2'>
        <Button
          type='button'
          size='sm'
          variant={enabled ? 'default' : 'outline'}
          onClick={() => onEnabledChange(true)}
        >
          <CircleCheck data-icon='inline-start' />
          {t('Use canonical video pricing')}
        </Button>
        {enabled && (
          <Button
            type='button'
            size='sm'
            variant='outline'
            onClick={() => onEnabledChange(false)}
          >
            {t('Use general expression pricing')}
          </Button>
        )}
      </div>

      {enabled && (
        <div className='space-y-4 rounded-md border p-3'>
          <div className='space-y-1'>
            <h4 className='text-sm font-medium'>{t('Per-second credits')}</h4>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Canonical video pricing only uses normalized billing fields.'
              )}
            </p>
          </div>

          <div className='grid gap-3 sm:grid-cols-2'>
            {resolutions.map((resolution) => (
              <Field key={resolution} className='gap-2'>
                <FieldLabel>{resolution}</FieldLabel>
                <Input
                  inputMode='decimal'
                  placeholder='0'
                  value={rates[resolution] || ''}
                  onChange={(event) =>
                    changeRate(resolution, event.target.value)
                  }
                  aria-invalid={
                    rates[resolution] === '' || rates[resolution] === undefined
                  }
                />
                <FieldDescription>{t('Credits / second')}</FieldDescription>
              </Field>
            ))}
          </div>

          <div className='flex flex-wrap items-end gap-3 border-t pt-3'>
            <Field className='gap-2'>
              <FieldLabel>{t('Preview duration')}</FieldLabel>
              <Select
                items={durations.map((duration) => ({
                  value: String(duration),
                  label: `${duration}s`,
                }))}
                value={previewDuration}
                onValueChange={(value) => setPreviewDuration(value ?? '')}
              >
                <SelectTrigger className='w-28' size='sm'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {durations.map((duration) => (
                      <SelectItem key={duration} value={String(duration)}>
                        {duration}s
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            <div className='flex flex-wrap gap-x-4 gap-y-1 pb-1 text-sm'>
              {resolutions.map((resolution) => {
                const credits =
                  (Number(rates[resolution]) || 0) * previewDurationNumber
                return (
                  <span key={resolution}>
                    {t(
                      '{{resolution}}, {{duration}} seconds = {{credits}} credits',
                      {
                        resolution,
                        duration: previewDurationNumber || '-',
                        credits: Number.isFinite(credits) ? credits : 0,
                      }
                    )}
                  </span>
                )
              })}
            </div>
          </div>

          {!configured && (
            <p className='text-destructive text-xs'>
              {t(
                'Set a per-second credit price for every supported resolution.'
              )}
            </p>
          )}

          <div className='space-y-1.5'>
            <div className='text-muted-foreground text-xs'>
              {t('Generated canonical expression')}
            </div>
            <pre className='bg-muted/50 max-h-48 overflow-auto rounded-md border p-2 font-mono text-xs break-words whitespace-pre-wrap'>
              {expression}
            </pre>
          </div>
        </div>
      )}
    </div>
  )
}
