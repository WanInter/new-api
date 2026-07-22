import { describe, expect, test } from 'bun:test'
import {
  capabilityRuleFormSchema,
  capabilityToFormValues,
  formValuesToCapability,
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

  test('rejects reversed ranges and mixed duration modes', () => {
    const result = capabilityRuleFormSchema.safeParse({
      images_min: '4',
      images_max: '3',
      videos_min: '',
      videos_max: '',
      audios_min: '',
      audios_max: '',
      duration_min: '5',
      duration_max: '15',
      fixed_duration: '10',
      resolutions: [],
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
      duration_min: '',
      duration_max: '',
      fixed_duration: '',
      resolutions: [],
      require_json: 'inherit',
      require_text: 'inherit',
      content_precedence: 'inherit',
    })

    expect(result.success).toBe(false)
  })

  test('serializes selected resolution capabilities', () => {
    const values = capabilityToFormValues({ resolutions: ['720p', '4k'] })

    expect(JSON.stringify(values.resolutions)).toBe(
      JSON.stringify(['720p', '4k'])
    )
    expect(JSON.stringify(formValuesToCapability(values))).toBe(
      JSON.stringify({ resolutions: ['720p', '4k'] })
    )
  })
})
