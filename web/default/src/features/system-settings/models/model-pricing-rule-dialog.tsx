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
import { useEffect, useMemo } from 'react'
import * as z from 'zod'
import { type Resolver, useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { Dialog } from '@/components/dialog'
import type {
  ModelPricingRule,
  ModelPricingRulePayload,
  ModelPricingRuleSubjectType,
} from './model-pricing-rules-types'

const FORM_ID = 'model-pricing-rule-form'

type ModelPricingRuleFormValues = {
  subject_type: ModelPricingRuleSubjectType
  subject_value: string
  model: string
  using_group: string
  ratio: number
  enabled: boolean
}

type ModelPricingRuleDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  rule: ModelPricingRule | null
  isSaving: boolean
  onSave: (payload: ModelPricingRulePayload) => Promise<boolean>
}

function buildDefaultValues(
  rule: ModelPricingRule | null
): ModelPricingRuleFormValues {
  if (rule) {
    return {
      subject_type: rule.subject_type,
      subject_value: rule.subject_value,
      model: rule.model,
      using_group: rule.using_group,
      ratio: rule.ratio,
      enabled: rule.enabled,
    }
  }
  return {
    subject_type: 'user',
    subject_value: '',
    model: '',
    using_group: '',
    ratio: 1,
    enabled: true,
  }
}

export function ModelPricingRuleDialog(props: ModelPricingRuleDialogProps) {
  const { t } = useTranslation()
  const schema = useMemo(
    () =>
      z.object({
        subject_type: z.enum(['user', 'user_group']),
        subject_value: z.string().trim().min(1, t('Value is required')),
        model: z.string().trim().min(1, t('Value is required')),
        using_group: z.string(),
        ratio: z.coerce
          .number()
          .finite(t('Ratio must be a non-negative number'))
          .min(0, t('Ratio must be a non-negative number')),
        enabled: z.boolean(),
      }),
    [t]
  )
  const form = useForm<ModelPricingRuleFormValues>({
    resolver: zodResolver(
      schema
    ) as unknown as Resolver<ModelPricingRuleFormValues>,
    defaultValues: buildDefaultValues(props.rule),
  })

  useEffect(() => {
    if (props.open) form.reset(buildDefaultValues(props.rule))
  }, [form, props.open, props.rule])

  const subjectType = form.watch('subject_type')
  const enabled = form.watch('enabled')

  const onSubmit = async (values: ModelPricingRuleFormValues) => {
    const saved = await props.onSave({
      subject_type: values.subject_type,
      subject_value: values.subject_value.trim(),
      model: values.model.trim(),
      using_group: values.using_group.trim(),
      ratio: Number(values.ratio),
      enabled: values.enabled,
    })
    if (saved) props.onOpenChange(false)
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={props.rule ? t('Edit Rule') : t('Create Rule')}
      contentClassName='sm:max-w-xl'
      contentHeight='auto'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
            disabled={props.isSaving}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' form={FORM_ID} disabled={props.isSaving}>
            {props.isSaving ? t('Saving...') : t('Save')}
          </Button>
        </>
      }
    >
      <form
        id={FORM_ID}
        className='grid gap-4 sm:grid-cols-2'
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <div className='grid gap-2'>
          <Label htmlFor='model-pricing-rule-subject-type'>
            {t('Subject Type')}
          </Label>
          <NativeSelect
            id='model-pricing-rule-subject-type'
            className='w-full'
            {...form.register('subject_type')}
          >
            <option value='user'>{t('User')}</option>
            <option value='user_group'>{t('User Group')}</option>
          </NativeSelect>
        </div>
        <div className='grid gap-2'>
          <Label htmlFor='model-pricing-rule-subject-value'>
            {subjectType === 'user' ? t('User ID') : t('User Group')}
          </Label>
          <Input
            id='model-pricing-rule-subject-value'
            aria-invalid={!!form.formState.errors.subject_value}
            {...form.register('subject_value')}
          />
          {form.formState.errors.subject_value?.message ? (
            <p className='text-destructive text-xs'>
              {form.formState.errors.subject_value.message}
            </p>
          ) : null}
        </div>
        <div className='grid gap-2'>
          <Label htmlFor='model-pricing-rule-model'>{t('Model')}</Label>
          <Input
            id='model-pricing-rule-model'
            aria-invalid={!!form.formState.errors.model}
            {...form.register('model')}
          />
          {form.formState.errors.model?.message ? (
            <p className='text-destructive text-xs'>
              {form.formState.errors.model.message}
            </p>
          ) : null}
        </div>
        <div className='grid gap-2'>
          <Label htmlFor='model-pricing-rule-routing-group'>
            {t('Routing Group')}
          </Label>
          <Input
            id='model-pricing-rule-routing-group'
            {...form.register('using_group')}
          />
        </div>
        <div className='grid gap-2'>
          <Label htmlFor='model-pricing-rule-ratio'>{t('Ratio')}</Label>
          <Input
            id='model-pricing-rule-ratio'
            type='number'
            min={0}
            step='0.000001'
            aria-invalid={!!form.formState.errors.ratio}
            {...form.register('ratio', { valueAsNumber: true })}
          />
          {form.formState.errors.ratio?.message ? (
            <p className='text-destructive text-xs'>
              {form.formState.errors.ratio.message}
            </p>
          ) : null}
        </div>
        <div className='flex items-center gap-3 pt-7'>
          <Switch
            id='model-pricing-rule-enabled'
            checked={enabled}
            onCheckedChange={(checked) =>
              form.setValue('enabled', checked, { shouldDirty: true })
            }
          />
          <Label htmlFor='model-pricing-rule-enabled'>{t('Enabled')}</Label>
        </div>
      </form>
    </Dialog>
  )
}
