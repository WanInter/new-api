import { z } from 'zod'
import type { VideoMediaRange, VideoModelCapability } from '../types'

const maxVideoOutputDimension = 32768

export type VideoOutputListField = 'aspect_ratios' | 'resolutions' | 'sizes'

export type VideoSimulationOutput = {
  aspect_ratio?: string
  size?: string
  resolution?: string
}

export type VideoSimulationOutputError =
  | 'invalid_aspect_ratio'
  | 'invalid_size'
  | 'invalid_resolution'
  | 'size_aspect_ratio_conflict'

export type VideoSimulationOutputNormalization =
  | { output: VideoSimulationOutput }
  | { error: VideoSimulationOutputError }

const outputFieldMessages: Record<VideoOutputListField, string> = {
  aspect_ratios: 'Use W:H aspect ratios or adaptive',
  resolutions: 'Use a quality label such as 720p or 4k',
  sizes: 'Use WxH pixel sizes',
}

export function normalizeVideoOutputListValue(
  field: VideoOutputListField,
  value: string
): string | undefined {
  const trimmed = value.trim()
  if (field === 'aspect_ratios') {
    const normalized = trimmed.toLowerCase()
    if (normalized === 'adaptive') return normalized
    const parts = normalized.split(':')
    if (parts.length !== 2) return undefined
    const width = parseVideoOutputDimension(parts[0])
    const height = parseVideoOutputDimension(parts[1])
    if (width === undefined || height === undefined) return undefined
    const divisor = greatestCommonDivisor(width, height)
    return `${width / divisor}:${height / divisor}`
  }

  if (field === 'resolutions') {
    const normalized = trimmed.toLowerCase()
    if (normalized === '4k' || normalized === '2160p') return '4k'
    if (!normalized.endsWith('p')) return undefined
    const height = parseVideoOutputDimension(normalized.slice(0, -1))
    return height === undefined ? undefined : `${height}p`
  }

  const parts = trimmed.split('x')
  if (parts.length !== 2) return undefined
  const width = parseVideoOutputDimension(parts[0])
  const height = parseVideoOutputDimension(parts[1])
  if (width === undefined || height === undefined) return undefined
  return `${width}x${height}`
}

export function normalizeVideoOutputList(
  field: VideoOutputListField,
  values: string[]
): string[] {
  return values.map(
    (value) => normalizeVideoOutputListValue(field, value) || value.trim()
  )
}

export function normalizeVideoSimulationOutput(
  output: VideoSimulationOutput
): VideoSimulationOutputNormalization {
  const aspectRatio = normalizeOptionalVideoOutputValue(
    'aspect_ratios',
    output.aspect_ratio
  )
  if (output.aspect_ratio?.trim() && !aspectRatio) {
    return { error: 'invalid_aspect_ratio' }
  }

  const size = normalizeOptionalVideoOutputValue('sizes', output.size)
  if (output.size?.trim() && !size) {
    return { error: 'invalid_size' }
  }

  const resolution = normalizeOptionalVideoOutputValue(
    'resolutions',
    output.resolution
  )
  if (output.resolution?.trim() && !resolution) {
    return { error: 'invalid_resolution' }
  }

  if (size && aspectRatio) {
    const sizeAspectRatio = aspectRatioFromSize(size)
    if (aspectRatio === 'adaptive' || aspectRatio !== sizeAspectRatio) {
      return { error: 'size_aspect_ratio_conflict' }
    }
  }

  return {
    output: {
      aspect_ratio: aspectRatio,
      size,
      resolution,
    },
  }
}

function normalizeOptionalVideoOutputValue(
  field: VideoOutputListField,
  value?: string
): string | undefined {
  if (!value?.trim()) return undefined
  return normalizeVideoOutputListValue(field, value)
}

function aspectRatioFromSize(size: string): string {
  const [width, height] = size.split('x').map(Number)
  const divisor = greatestCommonDivisor(width, height)
  return `${width / divisor}:${height / divisor}`
}

function parseVideoOutputDimension(value: string): number | undefined {
  if (!/^\d+$/.test(value.trim())) return undefined
  const parsed = Number(value.trim())
  return parsed > 0 && parsed <= maxVideoOutputDimension ? parsed : undefined
}

function greatestCommonDivisor(left: number, right: number): number {
  while (right !== 0) {
    const remainder = left % right
    left = right
    right = remainder
  }
  return left
}

function outputListSchema(field: VideoOutputListField) {
  return z.array(z.string()).superRefine((values, context) => {
    const seen = new Set<string>()
    values.forEach((value, index) => {
      const normalized = normalizeVideoOutputListValue(field, value)
      if (!normalized) {
        context.addIssue({
          code: z.ZodIssueCode.custom,
          path: [index],
          message: outputFieldMessages[field],
        })
        return
      }
      if (seen.has(normalized)) {
        context.addIssue({
          code: z.ZodIssueCode.custom,
          path: [index],
          message: 'Do not repeat output values',
        })
        return
      }
      seen.add(normalized)
    })
  })
}

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
    video_audio_total_min: optionalNonNegativeInteger,
    video_audio_total_max: optionalNonNegativeInteger,
    duration_min: optionalPositiveInteger,
    duration_max: optionalPositiveInteger,
    fixed_duration: optionalPositiveInteger,
    aspect_ratios: outputListSchema('aspect_ratios'),
    resolutions: outputListSchema('resolutions'),
    sizes: outputListSchema('sizes'),
    require_json: booleanOverrideSchema,
    require_text: booleanOverrideSchema,
    content_precedence: booleanOverrideSchema,
  })
  .superRefine((values, context) => {
    validateRange(values.images_min, values.images_max, 'images_max', context)
    validateRange(values.videos_min, values.videos_max, 'videos_max', context)
    validateRange(values.audios_min, values.audios_max, 'audios_max', context)
    validateRange(
      values.video_audio_total_min,
      values.video_audio_total_max,
      'video_audio_total_max',
      context
    )
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
  video_audio_total_min: '',
  video_audio_total_max: '',
  duration_min: '',
  duration_max: '',
  fixed_duration: '',
  aspect_ratios: [],
  resolutions: [],
  sizes: [],
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
    video_audio_total_min: numberToDraft(capability?.video_audio_total?.min),
    video_audio_total_max: numberToDraft(capability?.video_audio_total?.max),
    duration_min: numberToDraft(capability?.duration?.min),
    duration_max: numberToDraft(capability?.duration?.max),
    fixed_duration: numberToDraft(capability?.fixed_duration),
    aspect_ratios: capability?.aspect_ratios || [],
    resolutions: capability?.resolutions || [],
    sizes: capability?.sizes || [],
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
    video_audio_total: rangeFromDraft(
      values.video_audio_total_min,
      values.video_audio_total_max
    ),
    duration: rangeFromDraft(values.duration_min, values.duration_max),
    fixed_duration: draftToNumber(values.fixed_duration),
    aspect_ratios:
      values.aspect_ratios.length > 0
        ? normalizeVideoOutputList('aspect_ratios', values.aspect_ratios)
        : undefined,
    resolutions:
      values.resolutions.length > 0
        ? normalizeVideoOutputList('resolutions', values.resolutions)
        : undefined,
    sizes:
      values.sizes.length > 0
        ? normalizeVideoOutputList('sizes', values.sizes)
        : undefined,
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
    values.aspect_ratios.length > 0 ||
    values.resolutions.length > 0 ||
    values.sizes.length > 0 ||
    Object.entries(values).some(
      ([field, value]) =>
        field !== 'aspect_ratios' &&
        field !== 'resolutions' &&
        field !== 'sizes' &&
        value !== '' &&
        value !== 'inherit'
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
