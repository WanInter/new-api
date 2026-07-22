import { z } from 'zod'
import type {
  VideoMediaRange,
  VideoModelCapability,
  VideoResolution,
} from '../types'

export const videoResolutionOptions: VideoResolution[] = [
  '480p',
  '720p',
  '1080p',
  '4k',
]

const optionalNonNegativeInteger = z
  .string()
  .refine((value) => value === '' || /^\d+$/.test(value), {
    message: 'Enter a non-negative integer',
  })

const optionalPositiveInteger = z
  .string()
  .refine((value) => value === '' || /^[1-9]\d*$/.test(value), {
    message: 'Enter a positive integer',
  })

export const booleanOverrideSchema = z.enum(['inherit', 'true', 'false'])

export const capabilityRuleFormSchema = z
  .object({
    images_min: optionalNonNegativeInteger,
    images_max: optionalNonNegativeInteger,
    videos_min: optionalNonNegativeInteger,
    videos_max: optionalNonNegativeInteger,
    audios_min: optionalNonNegativeInteger,
    audios_max: optionalNonNegativeInteger,
    duration_min: optionalPositiveInteger,
    duration_max: optionalPositiveInteger,
    fixed_duration: optionalPositiveInteger,
    resolutions: z.array(z.enum(videoResolutionOptions)),
    require_json: booleanOverrideSchema,
    require_text: booleanOverrideSchema,
    content_precedence: booleanOverrideSchema,
  })
  .superRefine((values, context) => {
    validateRange(values.images_min, values.images_max, 'images_max', context)
    validateRange(values.videos_min, values.videos_max, 'videos_max', context)
    validateRange(values.audios_min, values.audios_max, 'audios_max', context)
    validateRange(
      values.duration_min,
      values.duration_max,
      'duration_max',
      context
    )
    if (
      values.fixed_duration !== '' &&
      (values.duration_min !== '' || values.duration_max !== '')
    ) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['fixed_duration'],
        message: 'Fixed duration cannot be combined with a duration range',
      })
    }
    if (!hasAnyOverride(values)) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['images_min'],
        message: 'Add at least one override',
      })
    }
  })

export type CapabilityRuleFormValues = z.infer<typeof capabilityRuleFormSchema>

export const emptyCapabilityRuleFormValues: CapabilityRuleFormValues = {
  images_min: '',
  images_max: '',
  videos_min: '',
  videos_max: '',
  audios_min: '',
  audios_max: '',
  duration_min: '',
  duration_max: '',
  fixed_duration: '',
  resolutions: [],
  require_json: 'inherit',
  require_text: 'inherit',
  content_precedence: 'inherit',
}

export function capabilityToFormValues(
  capability?: VideoModelCapability
): CapabilityRuleFormValues {
  return {
    images_min: numberToDraft(capability?.images?.min),
    images_max: numberToDraft(capability?.images?.max),
    videos_min: numberToDraft(capability?.videos?.min),
    videos_max: numberToDraft(capability?.videos?.max),
    audios_min: numberToDraft(capability?.audios?.min),
    audios_max: numberToDraft(capability?.audios?.max),
    duration_min: numberToDraft(capability?.duration?.min),
    duration_max: numberToDraft(capability?.duration?.max),
    fixed_duration: numberToDraft(capability?.fixed_duration),
    resolutions: capability?.resolutions || [],
    require_json: booleanToOverride(capability?.require_json),
    require_text: booleanToOverride(capability?.require_text),
    content_precedence: booleanToOverride(capability?.content_precedence),
  }
}

export function formValuesToCapability(
  values: CapabilityRuleFormValues
): VideoModelCapability {
  return {
    images: rangeFromDraft(values.images_min, values.images_max),
    videos: rangeFromDraft(values.videos_min, values.videos_max),
    audios: rangeFromDraft(values.audios_min, values.audios_max),
    duration: rangeFromDraft(values.duration_min, values.duration_max),
    fixed_duration: draftToNumber(values.fixed_duration),
    resolutions: values.resolutions.length > 0 ? values.resolutions : undefined,
    require_json: overrideToBoolean(values.require_json),
    require_text: overrideToBoolean(values.require_text),
    content_precedence: overrideToBoolean(values.content_precedence),
  }
}

function validateRange(
  minDraft: string,
  maxDraft: string,
  path: keyof CapabilityRuleFormValues,
  context: z.RefinementCtx
) {
  if (minDraft === '' || maxDraft === '') return
  if (Number(minDraft) <= Number(maxDraft)) return
  context.addIssue({
    code: z.ZodIssueCode.custom,
    path: [path],
    message: 'Maximum must be greater than or equal to minimum',
  })
}

function hasAnyOverride(values: CapabilityRuleFormValues) {
  return (
    values.resolutions.length > 0 ||
    Object.entries(values).some(
      ([field, value]) =>
        field !== 'resolutions' && value !== '' && value !== 'inherit'
    )
  )
}

function rangeFromDraft(minDraft: string, maxDraft: string) {
  const min = draftToNumber(minDraft)
  const max = draftToNumber(maxDraft)
  if (min === undefined && max === undefined) return undefined
  const range: VideoMediaRange = {}
  if (min !== undefined) range.min = min
  if (max !== undefined) range.max = max
  return range
}

function draftToNumber(value: string) {
  return value === '' ? undefined : Number(value)
}

function numberToDraft(value?: number) {
  return value === undefined ? '' : String(value)
}

function booleanToOverride(
  value?: boolean
): CapabilityRuleFormValues['require_json'] {
  if (value === undefined) return 'inherit'
  return value ? 'true' : 'false'
}

function overrideToBoolean(value: CapabilityRuleFormValues['require_json']) {
  if (value === 'inherit') return undefined
  return value === 'true'
}
