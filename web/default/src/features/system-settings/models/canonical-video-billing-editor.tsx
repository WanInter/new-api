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
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import {
  areCanonicalRatesComplete,
  buildCanonicalPricingExpr,
  formatCanonicalSpecificationLabel,
  getCanonicalPricingBlockReason,
  getCanonicalPricingSchema,
  hasCanonicalSchemaVersionConflict,
  isCanonicalPricingAvailable,
  isCanonicalRateDraft,
  type CanonicalPricingMode,
  type CanonicalPricingRates,
} from './canonical-video-billing'
import type { BillingCapability } from './model-billing-api'

type CanonicalVideoBillingEditorProps = {
  capability: BillingCapability | null
  capabilityError: string
  isLoading: boolean
  enabled: boolean
  configuredSchemaVersion: string
  pricingMode: CanonicalPricingMode
  rates: CanonicalPricingRates
  onEnabledChange: (enabled: boolean) => void
  onSchemaVersionAccept: () => void
  onPricingModeChange: (mode: CanonicalPricingMode) => void
  onRatesChange: (rates: CanonicalPricingRates) => void
}

function SchemaSummary({ capability }: { capability: BillingCapability }) {
  const { t } = useTranslation()

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
          {t('Canonical billing fields')}
        </div>
        <div className='flex flex-wrap gap-1.5'>
          {capability.fields.map((field) => (
            <Badge
              key={field.path}
              variant='secondary'
              className='font-mono text-xs'
            >
              {field.path} ({field.type})
              {!field.required ? ` [${t('Optional')}]` : ''}
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
            <div className='text-muted-foreground text-xs'>{t('None')}</div>
          )}
        </div>
      </div>
      {capability.checked_at > 0 && (
        <div className='text-muted-foreground text-xs'>
          {t('Last validation')}:{' '}
          {new Date(capability.checked_at * 1000).toLocaleString()}
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
  configuredSchemaVersion,
  pricingMode,
  rates,
  onEnabledChange,
  onSchemaVersionAccept,
  onPricingModeChange,
  onRatesChange,
}: CanonicalVideoBillingEditorProps) {
  const { t } = useTranslation()
  const schema = useMemo(
    () => getCanonicalPricingSchema(capability, pricingMode),
    [capability, pricingMode]
  )
  const [previewDuration, setPreviewDuration] = useState('')
  const available = isCanonicalPricingAvailable(capability)
  const blockReason = getCanonicalPricingBlockReason(capability)

  useEffect(() => {
    if (schema.durations.length === 0) {
      setPreviewDuration('')
      return
    }
    setPreviewDuration((current) =>
      schema.durations.some((duration) => String(duration) === current)
        ? current
        : String(schema.durations[schema.durations.length - 1])
    )
  }, [schema.durations])

  const expression = useMemo(
    () =>
      capability
        ? buildCanonicalPricingExpr(capability, rates, pricingMode)
        : '',
    [capability, pricingMode, rates]
  )
  const configured = areCanonicalRatesComplete(rates, schema.rateKeys)
  const schemaVersionConflict = hasCanonicalSchemaVersionConflict(
    configuredSchemaVersion,
    capability
  )
  const previewDurationNumber = Number(previewDuration)

  const changeRate = (key: string, value: string) => {
    if (!isCanonicalRateDraft(value)) return
    onRatesChange({ ...rates, [key]: value })
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
      <div className='flex flex-col gap-2'>
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
        {enabled && (
          <Button
            type='button'
            size='sm'
            variant='outline'
            className='self-start'
            onClick={() => onEnabledChange(false)}
          >
            {t('Use general expression pricing')}
          </Button>
        )}
      </div>
    )
  }

  if (!capability) return null

  if (!capability.canonical_applicable && !enabled) return null

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
                    'The canonical billing schema is not supported by this editor.'
                  )
                : blockReason}
          </AlertDescription>
        </Alert>
        <SchemaSummary capability={capability} />
        {enabled && (
          <Button
            type='button'
            size='sm'
            variant='outline'
            className='self-start'
            onClick={() => onEnabledChange(false)}
          >
            {t('Use general expression pricing')}
          </Button>
        )}
      </div>
    )
  }

  const isPerSecond = schema.priceMode === 'per-second'
  return (
    <div className='space-y-3'>
      <SchemaSummary capability={capability} />

      {enabled && schemaVersionConflict && (
        <Alert variant='destructive'>
          <CircleAlert />
          <AlertTitle>{t('Canonical billing schema')}</AlertTitle>
          <AlertDescription>
            {t(
              'The canonical schema for {{model}} changed from {{configured}} to {{current}}.',
              {
                model: capability.model,
                configured: configuredSchemaVersion || '-',
                current: capability.schema_version || '-',
              }
            )}
            <Button
              type='button'
              size='sm'
              variant='outline'
              className='mt-2'
              onClick={onSchemaVersionAccept}
            >
              {t('Use current canonical schema')}
            </Button>
          </AlertDescription>
        </Alert>
      )}

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
            <h4 className='text-sm font-medium'>
              {isPerSecond
                ? t('Per-second credits')
                : t('Fixed credits by specification')}
            </h4>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Canonical video pricing only uses normalized billing fields.'
              )}
            </p>
          </div>

          {schema.durationField?.required && (
            <Field className='gap-2'>
              <FieldLabel>{t('Pricing method')}</FieldLabel>
              <ToggleGroup
                value={[pricingMode]}
                onValueChange={(values) => {
                  const next = values.find((value) => value !== pricingMode)
                  if (next === 'per-second' || next === 'fixed') {
                    onPricingModeChange(next)
                  }
                }}
                variant='outline'
                size='sm'
                aria-label={t('Pricing method')}
              >
                <ToggleGroupItem value='per-second'>
                  {t('Per second')}
                </ToggleGroupItem>
                <ToggleGroupItem value='fixed'>
                  {t('Per specification')}
                </ToggleGroupItem>
              </ToggleGroup>
            </Field>
          )}

          <div className='grid gap-3 sm:grid-cols-2'>
            {schema.specifications.map((specification) => (
              <Field key={specification.key} className='gap-2'>
                <FieldLabel>
                  {formatCanonicalSpecificationLabel(specification, t) ||
                    t('All supported durations')}
                </FieldLabel>
                <Input
                  inputMode='decimal'
                  placeholder='0'
                  value={rates[specification.key] || ''}
                  onChange={(event) =>
                    changeRate(specification.key, event.target.value)
                  }
                  aria-invalid={
                    rates[specification.key] === '' ||
                    rates[specification.key] === undefined
                  }
                />
                <FieldDescription>
                  {isPerSecond ? t('Credits / second') : t('Credits / request')}
                </FieldDescription>
              </Field>
            ))}
          </div>

          <div className='flex flex-wrap items-end gap-3 border-t pt-3'>
            {isPerSecond && (
              <Field className='gap-2'>
                <FieldLabel>{t('Preview duration')}</FieldLabel>
                <Select
                  items={schema.durations.map((duration) => ({
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
                      {schema.durations.map((duration) => (
                        <SelectItem key={duration} value={String(duration)}>
                          {duration}s
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
            )}
            <div className='flex flex-wrap gap-x-4 gap-y-1 pb-1 text-sm'>
              {schema.specifications.map((specification) => {
                const rate = Number(rates[specification.key]) || 0
                const credits = isPerSecond
                  ? rate * previewDurationNumber
                  : rate
                const specificationLabel =
                  formatCanonicalSpecificationLabel(specification, t) ||
                  t('All supported durations')
                return (
                  <span key={specification.key}>
                    {isPerSecond
                      ? t(
                          '{{specification}}, {{duration}} seconds = {{credits}} credits',
                          {
                            specification: specificationLabel,
                            duration: previewDurationNumber || '-',
                            credits: Number.isFinite(credits) ? credits : 0,
                          }
                        )
                      : t('{{specification}} = {{credits}} credits', {
                          specification: specificationLabel,
                          credits: Number.isFinite(credits) ? credits : 0,
                        })}
                  </span>
                )
              })}
            </div>
          </div>

          {!configured && (
            <p className='text-destructive text-xs'>
              {t('Set a credit price for every supported specification.')}
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
