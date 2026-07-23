import { describe, expect, test } from 'bun:test'
import {
  capabilityRuleFormSchema,
  capabilityToFormValues,
  formValuesToCapability,
  normalizeVideoDurationList,
  normalizeVideoDurationListValue,
  normalizeVideoOutputListValue,
  normalizeVideoSimulationOutput,
} from './capability-form'

describe('routing capability override form', () => {
  test('preserves explicit zero and false values', () => {
    const values = capabilityToFormValues({
      images: { max: 0 },
      require_json: false,
    })

    expect(values.images_max).toBe('0')
    expect(values.require_json).toBe('false')
    expect(JSON.stringify(formValuesToCapability(values))).toBe(
      JSON.stringify({ images: { max: 0 }, require_json: false })
    )
  })

  test('round-trips the combined video and audio total range', () => {
    const values = capabilityToFormValues({
      video_audio_total: { min: 0, max: 3 },
    })

    expect(values.video_audio_total_min).toBe('0')
    expect(values.video_audio_total_max).toBe('3')
    expect(JSON.stringify(formValuesToCapability(values))).toBe(
      JSON.stringify({ video_audio_total: { min: 0, max: 3 } })
    )
  })

  test('rejects a reversed combined video and audio total range', () => {
    const result = capabilityRuleFormSchema.safeParse({
      ...capabilityToFormValues(),
      video_audio_total_min: '4',
      video_audio_total_max: '3',
    })

    expect(result.success).toBe(false)
    if (!result.success) {
      expect(
        result.error.issues.some(
          (issue) => issue.path[0] === 'video_audio_total_max'
        )
      ).toBe(true)
    }
  })

  test('rejects reversed ranges and mixed duration modes', () => {
    const result = capabilityRuleFormSchema.safeParse({
      images_min: '4',
      images_max: '3',
      videos_min: '',
      videos_max: '',
      audios_min: '',
      audios_max: '',
      video_audio_total_min: '',
      video_audio_total_max: '',
      duration_min: '5',
      duration_max: '15',
      fixed_duration: '10',
      durations: [],
      aspect_ratios: [],
      resolutions: [],
      sizes: [],
      require_json: 'inherit',
      require_text: 'inherit',
      content_precedence: 'inherit',
    })

    expect(result.success).toBe(false)
    if (!result.success) {
      const messages = result.error.issues.map((issue) => issue.message)
      expect(
        messages.includes('Maximum must be greater than or equal to minimum')
      ).toBe(true)
      expect(
        messages.includes(
          'Fixed duration cannot be combined with a duration range'
        )
      ).toBe(true)
    }
  })

  test('requires at least one explicit override', () => {
    const result = capabilityRuleFormSchema.safeParse({
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
      durations: [],
      aspect_ratios: [],
      resolutions: [],
      sizes: [],
      require_json: 'inherit',
      require_text: 'inherit',
      content_precedence: 'inherit',
    })

    expect(result.success).toBe(false)
  })

  test('round-trips supported discrete durations as numbers', () => {
    const values = capabilityToFormValues({ durations: [6, 10, 15] })

    expect(JSON.stringify(values.durations)).toBe(
      JSON.stringify(['6', '10', '15'])
    )
    expect(JSON.stringify(formValuesToCapability(values))).toBe(
      JSON.stringify({ durations: [6, 10, 15] })
    )
    expect(JSON.stringify(normalizeVideoDurationList(['06', ' 10 ']))).toBe(
      JSON.stringify(['6', '10'])
    )
    expect(normalizeVideoDurationListValue('0')).toBe(undefined)
  })

  test('rejects invalid, duplicate, and mixed duration constraints', () => {
    const invalidList = capabilityRuleFormSchema.safeParse({
      ...capabilityToFormValues(),
      durations: ['6', '06', '0', '1.5'],
    })
    expect(invalidList.success).toBe(false)

    const mixedModes = capabilityRuleFormSchema.safeParse({
      ...capabilityToFormValues(),
      duration_max: '20',
      durations: ['6', '10', '15'],
    })
    expect(mixedModes.success).toBe(false)
    if (!mixedModes.success) {
      expect(
        mixedModes.error.issues.some((issue) => issue.path[0] === 'durations')
      ).toBe(true)
    }
  })

  test('serializes selected resolution capabilities', () => {
    const values = capabilityToFormValues({
      resolutions: ['720p', '768p', '4k'],
    })

    expect(JSON.stringify(values.resolutions)).toBe(
      JSON.stringify(['720p', '768p', '4k'])
    )
    expect(JSON.stringify(formValuesToCapability(values))).toBe(
      JSON.stringify({ resolutions: ['720p', '768p', '4k'] })
    )
  })

  test('normalizes public video output capability values', () => {
    expect(normalizeVideoOutputListValue('aspect_ratios', ' 32:18 ')).toBe(
      '16:9'
    )
    expect(normalizeVideoOutputListValue('sizes', '1280x0720')).toBe('1280x720')
    expect(normalizeVideoOutputListValue('resolutions', '2160P')).toBe('4k')
    expect(normalizeVideoOutputListValue('resolutions', '768p')).toBe('768p')
  })

  test('rejects invalid and duplicate normalized output capability values', () => {
    const result = capabilityRuleFormSchema.safeParse({
      ...capabilityToFormValues(),
      aspect_ratios: ['16:9', '32:18'],
      resolutions: ['invalid'],
      sizes: ['1280X720'],
    })

    expect(result.success).toBe(false)
    if (!result.success) {
      const paths = result.error.issues.map((issue) => issue.path.join('.'))
      expect(paths.includes('aspect_ratios.1')).toBe(true)
      expect(paths.includes('resolutions.0')).toBe(true)
      expect(paths.includes('sizes.0')).toBe(true)
    }
  })

  test('normalizes and validates video simulator output fields together', () => {
    const cases = [
      {
        input: {
          aspect_ratio: '32:18',
          size: '1280x0720',
          resolution: '2160P',
        },
        expected: {
          output: {
            aspect_ratio: '16:9',
            size: '1280x720',
            resolution: '4k',
          },
        },
      },
      {
        input: { aspect_ratio: 'wide' },
        expected: { error: 'invalid_aspect_ratio' },
      },
      {
        input: { size: '1280X720' },
        expected: { error: 'invalid_size' },
      },
      {
        input: { resolution: 'hd' },
        expected: { error: 'invalid_resolution' },
      },
      {
        input: { aspect_ratio: '9:16', size: '1280x720' },
        expected: { error: 'size_aspect_ratio_conflict' },
      },
      {
        input: { aspect_ratio: 'adaptive', size: '1280x720' },
        expected: { error: 'size_aspect_ratio_conflict' },
      },
      {
        input: { size: '1280x720', resolution: '720p' },
        expected: {
          output: {
            aspect_ratio: undefined,
            size: '1280x720',
            resolution: '720p',
          },
        },
      },
    ]

    for (const testCase of cases) {
      expect(
        JSON.stringify(normalizeVideoSimulationOutput(testCase.input))
      ).toBe(JSON.stringify(testCase.expected))
    }
  })
})
