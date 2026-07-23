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
import {
  DEFAULT_BILLING_QUOTA_PER_UNIT,
  type BillingCapability,
  type BillingCapabilityField,
} from './model-billing-api'

export const CANONICAL_DURATION_PATH = 'billing.duration_seconds'
export const CANONICAL_DEFAULT_RATE_KEY = '__all__'

const numericDraftRegex = /^(?:\d+(?:\.\d*)?|\.\d*)?$/
const numberPattern = '[0-9.eE+-]+'
const supportedFieldTypes = new Set(['number', 'string', 'boolean'])
const omittedSpecificationValue = '__omitted__'

export type CanonicalPricingRates = Record<string, string>
export type CanonicalVideoRates = CanonicalPricingRates
export type CanonicalPricingMode = 'per-second' | 'fixed'

export type CanonicalSpecificationEntry = {
  field: BillingCapabilityField
  value: string
  omitted: boolean
}

export type CanonicalSpecification = {
  entries: CanonicalSpecificationEntry[]
  key: string
  label: string
}

export type CanonicalPricingSchema = {
  fields: BillingCapabilityField[]
  durationField?: BillingCapabilityField
  specificationFields: BillingCapabilityField[]
  durations: number[]
  specifications: CanonicalSpecification[]
  rateKeys: string[]
  priceMode: CanonicalPricingMode
}

function getCanonicalCreditScale(
  capability: BillingCapability | null | undefined
) {
  const quotaPerUnit = Number(capability?.quota_per_unit)
  const effectiveQuotaPerUnit =
    Number.isFinite(quotaPerUnit) && quotaPerUnit > 0
      ? quotaPerUnit
      : DEFAULT_BILLING_QUOTA_PER_UNIT
  return 1_000_000 / effectiveQuotaPerUnit
}

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function formatNumber(value: number) {
  if (!Number.isFinite(value)) return '0'
  return String(Number(value.toFixed(8)))
}

function normalizeEnumValue(
  field: BillingCapabilityField | undefined,
  value: string
): string | null {
  if (!field) return null
  const text = String(value).trim()
  switch (field.type) {
    case 'string':
      return text || null
    case 'number': {
      const number = Number(text)
      return Number.isFinite(number) ? formatNumber(number) : null
    }
    case 'boolean':
      return text === 'true' || text === 'false' ? text : null
    default:
      return null
  }
}

function getEnumValues(field: BillingCapabilityField | undefined) {
  const values = (field?.enum_values || [])
    .map((value) => normalizeEnumValue(field, value))
    .filter((value): value is string => value !== null)
  return [...new Set(values)]
}

function getFieldName(path: string) {
  return path.replace(/^billing\./, '')
}

function buildSpecificationKey(entries: CanonicalSpecificationEntry[]) {
  if (entries.length === 0) return CANONICAL_DEFAULT_RATE_KEY
  return JSON.stringify(
    entries.map((entry) => [
      entry.field.path,
      entry.omitted ? null : entry.value,
    ])
  )
}

function buildSpecificationLabel(entries: CanonicalSpecificationEntry[]) {
  if (entries.length === 0) return ''
  const entryLabel = (entry: CanonicalSpecificationEntry) =>
    entry.omitted ? omittedSpecificationValue : entry.value
  if (entries.length === 1) return entryLabel(entries[0])
  return entries
    .map((entry) => `${getFieldName(entry.field.path)}=${entryLabel(entry)}`)
    .join(', ')
}

export function formatCanonicalSpecificationLabel(
  specification: CanonicalSpecification,
  t: (key: string) => string
) {
  const entryLabel = (entry: CanonicalSpecificationEntry) =>
    entry.omitted ? t('Provider default') : entry.value
  if (specification.entries.length === 0) return ''
  if (specification.entries.length === 1) {
    return entryLabel(specification.entries[0])
  }
  return specification.entries
    .map((entry) => `${getFieldName(entry.field.path)}=${entryLabel(entry)}`)
    .join(', ')
}

function buildSpecifications(fields: BillingCapabilityField[]) {
  let specifications: Array<{ entries: CanonicalSpecificationEntry[] }> = [
    { entries: [] },
  ]
  for (const field of fields) {
    const entries: CanonicalSpecificationEntry[] = getEnumValues(field).map(
      (value) => ({ field, value, omitted: false })
    )
    if (!field.required) {
      entries.unshift({ field, value: '', omitted: true })
    }
    const next: Array<{ entries: CanonicalSpecificationEntry[] }> = []
    for (const specification of specifications) {
      for (const entry of entries) {
        next.push({
          entries: [...specification.entries, entry],
        })
      }
    }
    specifications = next
  }
  return specifications.map((specification) => ({
    ...specification,
    key: buildSpecificationKey(specification.entries),
    label: buildSpecificationLabel(specification.entries),
  }))
}

export function getCanonicalPricingSchema(
  capability: BillingCapability | null | undefined,
  pricingMode: CanonicalPricingMode = 'per-second'
): CanonicalPricingSchema {
  const fields = capability?.fields || []
  const durationField = fields.find(
    (field) => field.path === CANONICAL_DURATION_PATH
  )
  const priceMode =
    pricingMode === 'per-second' && durationField?.required
      ? 'per-second'
      : 'fixed'
  const specificationFields =
    priceMode === 'per-second'
      ? fields.filter((field) => field.path !== CANONICAL_DURATION_PATH)
      : fields
  const durations = getEnumValues(durationField)
    .map((value) => Number(value))
    .filter((value) => Number.isFinite(value) && value > 0)
    .sort((a, b) => a - b)
  const specifications = buildSpecifications(specificationFields)

  return {
    fields,
    durationField,
    specificationFields,
    durations,
    specifications,
    rateKeys: specifications.map((specification) => specification.key),
    priceMode,
  }
}

function isSupportedCanonicalField(field: BillingCapabilityField) {
  if (!/^billing\.[^.]+$/.test(field.path)) return false
  if (!supportedFieldTypes.has(field.type)) return false
  return (
    field.enum_values.length > 0 &&
    getEnumValues(field).length === field.enum_values.length
  )
}

export function getCanonicalPricingBlockReason(
  capability: BillingCapability | null | undefined
) {
  if (!capability) return ''
  if (capability.incompatible_channels.length > 0) {
    return capability.incompatible_channels
      .map((channel) => `${channel.name}: ${channel.reason}`)
      .join('; ')
  }
  if (capability.validation_error) return capability.validation_error
  if (!capability.canonical_available) return 'canonical_unavailable'
  if (!capability.schema_version) return 'canonical_schema_incomplete'

  const schema = getCanonicalPricingSchema(capability)
  const paths = schema.fields.map((field) => field.path)
  if (
    schema.fields.length === 0 ||
    new Set(paths).size !== paths.length ||
    schema.fields.some((field) => !isSupportedCanonicalField(field)) ||
    (schema.durationField &&
      (schema.durationField.type !== 'number' ||
        schema.durations.length === 0 ||
        getEnumValues(schema.durationField).length !==
          schema.durations.length)) ||
    (!schema.durationField && schema.specificationFields.length === 0) ||
    schema.specifications.length === 0
  ) {
    return 'canonical_schema_incomplete'
  }
  return ''
}

export function isCanonicalPricingAvailable(
  capability: BillingCapability | null | undefined
) {
  return Boolean(capability && !getCanonicalPricingBlockReason(capability))
}

export function isCanonicalRateDraft(value: string) {
  return numericDraftRegex.test(value)
}

export function areCanonicalRatesComplete(
  rates: CanonicalPricingRates,
  rateKeys: string[]
) {
  return rateKeys.every((key) => {
    const value = rates[key]
    return (
      value !== undefined &&
      value !== '' &&
      Number.isFinite(Number(value)) &&
      Number(value) >= 0
    )
  })
}

export function hasCanonicalSchemaVersionConflict(
  configuredSchema: string | null | undefined,
  capability: BillingCapability | null | undefined
) {
  if (!capability) return false
  return configuredSchema?.trim() !== capability.schema_version.trim()
}

function enumLiteral(field: BillingCapabilityField, value: string) {
  switch (field.type) {
    case 'number':
      return formatNumber(Number(value))
    case 'boolean':
      return value === 'true' ? 'true' : 'false'
    default:
      return JSON.stringify(value)
  }
}

function specificationCondition(specification: CanonicalSpecification) {
  return specification.entries
    .map(({ field, value, omitted }) =>
      omitted
        ? `param(${JSON.stringify(field.path)}) == nil`
        : `param(${JSON.stringify(field.path)}) == ${enumLiteral(field, value)}`
    )
    .join(' && ')
}

function specificationTierStem(specification: CanonicalSpecification) {
  if (specification.entries.length === 0) return ''
  if (specification.entries.some(({ field }) => !field.required)) {
    return `spec:${specification.key}`
  }
  if (specification.entries.length === 1) {
    return specification.entries[0].value
  }
  return specification.entries
    .map(({ field, value }) => `${getFieldName(field.path)}=${value}`)
    .join('|')
}

function tierLabel(
  specification: CanonicalSpecification,
  duration: number | null
) {
  const stem = specificationTierStem(specification)
  if (duration !== null) return stem ? `${stem}_${duration}s` : `${duration}s`
  return stem || 'default'
}

function buildDurationExpression(
  specification: CanonicalSpecification,
  durations: number[],
  rate: number,
  creditScale: number
) {
  const branches = durations.map((duration) => {
    const rawCost = formatNumber(rate * duration * creditScale)
    return `param(${JSON.stringify(
      CANONICAL_DURATION_PATH
    )}) == ${duration} ? tier(${JSON.stringify(
      tierLabel(specification, duration)
    )}, ${rawCost})`
  })
  return `(${branches.join(' : ')} : tier("unsupported_duration", 0))`
}

export function buildCanonicalPricingExpr(
  capability: BillingCapability,
  rates: CanonicalPricingRates,
  pricingMode: CanonicalPricingMode = 'per-second'
) {
  if (!isCanonicalPricingAvailable(capability)) return ''
  const schema = getCanonicalPricingSchema(capability, pricingMode)
  if (!areCanonicalRatesComplete(rates, schema.rateKeys)) return ''
  const creditScale = getCanonicalCreditScale(capability)

  if (schema.priceMode === 'per-second') {
    const durationExpressions = schema.specifications.map((specification) => {
      const rate = Number(rates[specification.key]) || 0
      return buildDurationExpression(
        specification,
        schema.durations,
        rate,
        creditScale
      )
    })
    if (schema.specificationFields.length === 0) return durationExpressions[0]
    return schema.specifications
      .map(
        (specification, index) =>
          `${specificationCondition(specification)} ? ${durationExpressions[index]}`
      )
      .concat('tier("unsupported_specification", 0)')
      .join(' : ')
  }

  return schema.specifications
    .map((specification) => {
      const rawCost = formatNumber(
        (Number(rates[specification.key]) || 0) * creditScale
      )
      return `${specificationCondition(specification)} ? tier(${JSON.stringify(
        tierLabel(specification, null)
      )}, ${rawCost})`
    })
    .concat('tier("unsupported_specification", 0)')
    .join(' : ')
}

function extractTierRawCost(expr: string, label: string) {
  const match = expr.match(
    new RegExp(
      `tier\\(${escapeRegExp(JSON.stringify(label))},\\s*(${numberPattern})\\)`
    )
  )
  if (!match) return null
  const rawCost = Number(match[1])
  return Number.isFinite(rawCost) ? rawCost : null
}

export function detectCanonicalPricingMode(
  expr: string,
  capability: BillingCapability | null | undefined
): CanonicalPricingMode {
  const requiredDuration = capability?.fields.find(
    (field) => field.path === CANONICAL_DURATION_PATH
  )
  if (!requiredDuration?.required) return 'fixed'
  return /tier\("[^"]*unsupported_duration",\s*0\)/.test(expr)
    ? 'per-second'
    : 'fixed'
}

export function extractCanonicalPricingRates(
  expr: string,
  capability: BillingCapability | null | undefined,
  pricingMode: CanonicalPricingMode = detectCanonicalPricingMode(
    expr,
    capability
  )
): CanonicalPricingRates {
  if (!capability || !expr.includes('param("billing.')) return {}
  const schema = getCanonicalPricingSchema(capability, pricingMode)
  const creditScale = getCanonicalCreditScale(capability)
  const rates: CanonicalPricingRates = {}

  for (const specification of schema.specifications) {
    if (schema.priceMode === 'per-second') {
      const samples = schema.durations
        .map((duration) => {
          const rawCost = extractTierRawCost(
            expr,
            tierLabel(specification, duration)
          )
          return rawCost === null ? null : rawCost / duration / creditScale
        })
        .filter((value): value is number => value !== null)
      if (
        samples.length === schema.durations.length &&
        samples.every((value) => Math.abs(value - samples[0]) < 1e-8)
      ) {
        rates[specification.key] = formatNumber(samples[0])
      }
      continue
    }

    const rawCost = extractTierRawCost(expr, tierLabel(specification, null))
    if (rawCost !== null) {
      rates[specification.key] = formatNumber(rawCost / creditScale)
    }
  }
  return rates
}

// Compatibility exports for callers that still use the original video-specific names.
export const getCanonicalVideoDimensions = getCanonicalPricingSchema
export const getCanonicalVideoBlockReason = getCanonicalPricingBlockReason
export const isCanonicalVideoAvailable = isCanonicalPricingAvailable
export const buildCanonicalVideoExpr = buildCanonicalPricingExpr
export const extractCanonicalVideoRates = extractCanonicalPricingRates
