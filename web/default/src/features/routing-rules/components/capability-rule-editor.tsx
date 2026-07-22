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
import { useEffect, useMemo, useState } from 'react'
import { Controller, type UseFormReturn, useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { Save, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  deleteVideoRoutingCapability,
  upsertVideoRoutingCapability,
} from '../api'
import {
  capabilityRuleFormSchema,
  capabilityToFormValues,
  emptyCapabilityRuleFormValues,
  formValuesToCapability,
  videoResolutionOptions,
  type CapabilityRuleFormValues,
} from '../lib/capability-form'
import type {
  VideoMediaRange,
  VideoResolution,
  VideoRoutingCandidate,
} from '../types'

type CapabilityRuleEditorProps = {
  candidate: VideoRoutingCandidate | null
  onClose: () => void
  onSaved: () => void | Promise<void>
}

type RangePrefix = 'images' | 'videos' | 'audios' | 'duration'

export function CapabilityRuleEditor(props: CapabilityRuleEditorProps) {
  const { t } = useTranslation()
  const [resetOpen, setResetOpen] = useState(false)
  const form = useForm<CapabilityRuleFormValues>({
    resolver: zodResolver(capabilityRuleFormSchema),
    defaultValues: emptyCapabilityRuleFormValues,
  })

  useEffect(() => {
    form.reset(
      capabilityToFormValues(props.candidate?.editable_rule?.capability)
    )
  }, [form, props.candidate])

  const saveMutation = useMutation({
    mutationFn: upsertVideoRoutingCapability,
    onSuccess: async () => {
      toast.success(t('Routing override saved'))
      await props.onSaved()
      props.onClose()
    },
    onError: () => {
      toast.error(t('Failed to save routing override'))
    },
  })
  const deleteMutation = useMutation({
    mutationFn: ({ id, revision }: { id: number; revision: number }) =>
      deleteVideoRoutingCapability(id, revision),
    onSuccess: async () => {
      toast.success(t('Routing override reset'))
      setResetOpen(false)
      await props.onSaved()
      props.onClose()
    },
    onError: () => {
      toast.error(t('Failed to reset routing override'))
    },
  })

  const booleanItems = useMemo(
    () => [
      { label: t('Inherit'), value: 'inherit' },
      { label: t('Required'), value: 'true' },
      { label: t('Not required'), value: 'false' },
    ],
    [t]
  )

  const handleSubmit = form.handleSubmit((values) => {
    const candidate = props.candidate
    if (!candidate) return
    saveMutation.mutate({
      channel_id: candidate.channel_id,
      upstream_model: candidate.mapping.model,
      capability: formValuesToCapability(values),
      revision: candidate.editable_rule?.revision || 0,
    })
  })

  const handleReset = () => {
    const rule = props.candidate?.editable_rule
    if (!rule) return
    deleteMutation.mutate({ id: rule.id, revision: rule.revision })
  }

  const busy = saveMutation.isPending || deleteMutation.isPending
  const candidate = props.candidate

  return (
    <>
      <Sheet
        open={Boolean(candidate)}
        onOpenChange={(open) => !open && !busy && props.onClose()}
      >
        <SheetContent side='right' className='sm:max-w-2xl'>
          <SheetHeader>
            <div className='flex flex-wrap items-center gap-2'>
              <SheetTitle>{t('Edit routing override')}</SheetTitle>
              <Badge variant={candidate?.editable_rule ? 'default' : 'outline'}>
                {candidate?.editable_rule
                  ? t('Database override')
                  : t('Inherited')}
              </Badge>
            </div>
            <SheetDescription>
              {candidate
                ? `#${candidate.channel_id} · ${candidate.channel_name} · ${candidate.mapping.model}`
                : ''}
            </SheetDescription>
          </SheetHeader>

          {candidate && (
            <form
              className='flex min-h-0 flex-1 flex-col'
              onSubmit={handleSubmit}
            >
              <div className='flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto px-4 py-2'>
                <RangeFieldSet
                  form={form}
                  prefix='images'
                  label={t('Images')}
                  effective={candidate.capability?.images}
                />
                <RangeFieldSet
                  form={form}
                  prefix='videos'
                  label={t('Videos')}
                  effective={candidate.capability?.videos}
                />
                <RangeFieldSet
                  form={form}
                  prefix='audios'
                  label={t('Audios')}
                  effective={candidate.capability?.audios}
                />

                <Separator />

                <ResolutionOverrideField
                  form={form}
                  effective={candidate.capability?.resolutions}
                />

                <Separator />

                <FieldSet>
                  <FieldLegend variant='label'>{t('Duration')}</FieldLegend>
                  <FieldGroup className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
                    <NumberOverrideField
                      form={form}
                      name='duration_min'
                      label={t('Minimum')}
                      min={1}
                      placeholder={candidate.capability?.duration?.min}
                    />
                    <NumberOverrideField
                      form={form}
                      name='duration_max'
                      label={t('Maximum')}
                      min={1}
                      placeholder={candidate.capability?.duration?.max}
                    />
                    <NumberOverrideField
                      form={form}
                      name='fixed_duration'
                      label={t('Fixed')}
                      min={1}
                      placeholder={candidate.capability?.fixed_duration}
                    />
                  </FieldGroup>
                </FieldSet>

                <Separator />

                <FieldSet>
                  <FieldLegend variant='label'>
                    {t('Request semantics')}
                  </FieldLegend>
                  <FieldGroup>
                    <BooleanOverrideField
                      form={form}
                      name='require_json'
                      label={t('JSON request')}
                      items={booleanItems}
                    />
                    <BooleanOverrideField
                      form={form}
                      name='require_text'
                      label={t('Text content')}
                      items={booleanItems}
                    />
                    <BooleanOverrideField
                      form={form}
                      name='content_precedence'
                      label={t('Explicit content precedence')}
                      items={booleanItems}
                    />
                  </FieldGroup>
                </FieldSet>
              </div>

              <SheetFooter className='border-t'>
                <div className='flex w-full flex-wrap justify-between gap-2'>
                  <div>
                    {candidate.editable_rule && (
                      <Button
                        type='button'
                        variant='destructive'
                        onClick={() => setResetOpen(true)}
                        disabled={busy}
                      >
                        <Trash2 data-icon='inline-start' />
                        {t('Reset override')}
                      </Button>
                    )}
                  </div>
                  <div className='flex gap-2'>
                    <Button
                      type='button'
                      variant='outline'
                      onClick={props.onClose}
                      disabled={busy}
                    >
                      {t('Cancel')}
                    </Button>
                    <Button type='submit' disabled={busy}>
                      <Save data-icon='inline-start' />
                      {t('Save override')}
                    </Button>
                  </div>
                </div>
              </SheetFooter>
            </form>
          )}
        </SheetContent>
      </Sheet>

      <AlertDialog open={resetOpen} onOpenChange={setResetOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Reset routing override?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('This channel will return to inherited routing capabilities.')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              onClick={handleReset}
              disabled={deleteMutation.isPending}
            >
              {t('Reset override')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

function ResolutionOverrideField(props: {
  form: UseFormReturn<CapabilityRuleFormValues>
  effective?: VideoResolution[]
}) {
  const { t } = useTranslation()
  return (
    <Controller
      control={props.form.control}
      name='resolutions'
      render={({ field }) => (
        <FieldSet>
          <FieldLegend variant='label'>
            {t('Supported resolutions')}
          </FieldLegend>
          <FieldGroup className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
            {videoResolutionOptions.map((resolution) => {
              const checked = field.value.includes(resolution)
              return (
                <Field key={resolution} orientation='horizontal'>
                  <Checkbox
                    id={`routing-resolution-${resolution}`}
                    checked={checked}
                    onCheckedChange={(nextChecked) => {
                      const next = nextChecked
                        ? [...field.value, resolution]
                        : field.value.filter((item) => item !== resolution)
                      field.onChange(next)
                    }}
                  />
                  <FieldLabel htmlFor={`routing-resolution-${resolution}`}>
                    {resolution}
                  </FieldLabel>
                </Field>
              )
            })}
          </FieldGroup>
          {props.effective?.length ? (
            <p className='text-muted-foreground text-xs'>
              {t('Effective')}: {props.effective.join(', ')}
            </p>
          ) : null}
        </FieldSet>
      )}
    />
  )
}

function RangeFieldSet(props: {
  form: UseFormReturn<CapabilityRuleFormValues>
  prefix: RangePrefix
  label: string
  effective?: VideoMediaRange
}) {
  const { t } = useTranslation()
  return (
    <FieldSet>
      <FieldLegend variant='label'>{props.label}</FieldLegend>
      <FieldGroup className='grid grid-cols-2 gap-3'>
        <NumberOverrideField
          form={props.form}
          name={`${props.prefix}_min`}
          label={t('Minimum')}
          min={0}
          placeholder={props.effective?.min}
        />
        <NumberOverrideField
          form={props.form}
          name={`${props.prefix}_max`}
          label={t('Maximum')}
          min={0}
          placeholder={props.effective?.max}
        />
      </FieldGroup>
    </FieldSet>
  )
}

function NumberOverrideField(props: {
  form: UseFormReturn<CapabilityRuleFormValues>
  name:
    | 'images_min'
    | 'images_max'
    | 'videos_min'
    | 'videos_max'
    | 'audios_min'
    | 'audios_max'
    | 'duration_min'
    | 'duration_max'
    | 'fixed_duration'
  label: string
  min: number
  placeholder?: number
}) {
  const { t } = useTranslation()
  const error = props.form.formState.errors[props.name]
  return (
    <Field data-invalid={Boolean(error)}>
      <FieldLabel htmlFor={`routing-${props.name}`}>{props.label}</FieldLabel>
      <Input
        id={`routing-${props.name}`}
        type='number'
        min={props.min}
        step={1}
        placeholder={props.placeholder?.toString() || '—'}
        aria-invalid={Boolean(error)}
        {...props.form.register(props.name)}
      />
      <FieldError>{error?.message ? t(error.message) : null}</FieldError>
    </Field>
  )
}

function BooleanOverrideField(props: {
  form: UseFormReturn<CapabilityRuleFormValues>
  name: 'require_json' | 'require_text' | 'content_precedence'
  label: string
  items: Array<{ label: string; value: string }>
}) {
  return (
    <Controller
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <Field orientation='horizontal'>
          <FieldLabel htmlFor={`routing-${props.name}`}>
            {props.label}
          </FieldLabel>
          <Select
            items={props.items}
            value={field.value}
            onValueChange={field.onChange}
          >
            <SelectTrigger id={`routing-${props.name}`} className='w-40'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {props.items.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
      )}
    />
  )
}
