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
} from './model-billing-api'

export const CANONICAL_DURATION_PATH = 'billing.duration_seconds'
export const CANONICAL_RESOLUTION_PATH = 'billing.resolution'

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

export type CanonicalVideoRates = Record<string, string>

const numericDraftRegex = /^(?:\d+(?:\.\d*)?|\.\d*)?$/

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function formatNumber(value: number) {
  if (!Number.isFinite(value)) return '0'
  return String(Number(value.toFixed(8)))
}

function getField(
  capability: BillingCapability | null | undefined,
  path: string
) {
  return capability?.fields.find((field) => field.path === path)
}

export function getCanonicalVideoDimensions(
  capability: BillingCapability | null | undefined
) {
  const durationField = getField(capability, CANONICAL_DURATION_PATH)
  const resolutionField = getField(capability, CANONICAL_RESOLUTION_PATH)
  const durations = (durationField?.enum_values || [])
    .map((value) => Number(value))
    .filter((value) => Number.isFinite(value) && value > 0)
    .sort((a, b) => a - b)
  const resolutions = (resolutionField?.enum_values || []).filter(Boolean)

  return { durationField, resolutionField, durations, resolutions }
}

export function getCanonicalVideoBlockReason(
  capability: BillingCapability | null | undefined
) {
  if (!capability) return ''
  if (capability.validation_error) return capability.validation_error
  if (capability.incompatible_channels.length > 0) {
    return capability.incompatible_channels
      .map((channel) => `${channel.name}: ${channel.reason}`)
      .join('; ')
  }
  if (!capability.canonical_available) return 'canonical_unavailable'

  const { durationField, resolutionField, durations, resolutions } =
    getCanonicalVideoDimensions(capability)
  if (
    durationField?.type !== 'number' ||
    !durationField.required ||
    resolutionField?.type !== 'string' ||
    !resolutionField.required ||
    durations.length === 0 ||
    resolutions.length === 0
  ) {
    return 'canonical_schema_incomplete'
  }
  return ''
}

export function isCanonicalVideoAvailable(
  capability: BillingCapability | null | undefined
) {
  return Boolean(capability && !getCanonicalVideoBlockReason(capability))
}

export function isCanonicalRateDraft(value: string) {
  return numericDraftRegex.test(value)
}

export function areCanonicalRatesComplete(
  rates: CanonicalVideoRates,
  resolutions: string[]
) {
  return resolutions.every((resolution) => {
    const value = rates[resolution]
    return value !== undefined && value !== '' && Number.isFinite(Number(value))
  })
}

export function buildCanonicalVideoExpr(
  capability: BillingCapability,
  rates: CanonicalVideoRates
) {
  const { durations, resolutions } = getCanonicalVideoDimensions(capability)
  if (durations.length === 0 || resolutions.length === 0) return ''
  const creditScale = getCanonicalCreditScale(capability)

  const durationExprForResolution = (resolution: string) => {
    const rate = Number(rates[resolution]) || 0
    const branches = durations.map((duration) => {
      const rawCost = formatNumber(rate * duration * creditScale)
      return `${CANONICAL_DURATION_PATH_EXPR} == ${duration} ? tier(${JSON.stringify(
        `${resolution}_${duration}s`
      )}, ${rawCost})`
    })
    return `(${branches.join(' : ')} : tier(${JSON.stringify(
      `${resolution}_unsupported_duration`
    )}, 0))`
  }

  return resolutions
    .map(
      (resolution) =>
        `${CANONICAL_RESOLUTION_PATH_EXPR} == ${JSON.stringify(
          resolution
        )} ? ${durationExprForResolution(resolution)}`
    )
    .concat('tier("unsupported_resolution", 0)')
    .join(' : ')
}

const CANONICAL_DURATION_PATH_EXPR = `param("${CANONICAL_DURATION_PATH}")`
const CANONICAL_RESOLUTION_PATH_EXPR = `param("${CANONICAL_RESOLUTION_PATH}")`

export function extractCanonicalVideoRates(
  expr: string,
  capability: BillingCapability | null | undefined
): CanonicalVideoRates {
  if (!capability || !expr.includes(CANONICAL_DURATION_PATH_EXPR)) return {}
  const { durations, resolutions } = getCanonicalVideoDimensions(capability)
  if (durations.length === 0) return {}
  const creditScale = getCanonicalCreditScale(capability)

  const rates: CanonicalVideoRates = {}
  for (const resolution of resolutions) {
    const samples = durations
      .map((duration) => {
        const label = `${resolution}_${duration}s`
        const match = expr.match(
          new RegExp(
            `tier\\(${escapeRegExp(JSON.stringify(label))},\\s*([0-9.]+)\\)`
          )
        )
        if (!match) return null
        const rawCost = Number(match[1])
        if (!Number.isFinite(rawCost)) return null
        return rawCost / duration / creditScale
      })
      .filter((value): value is number => value !== null)

    if (samples.length === durations.length) {
      const rate = samples[0]
      if (samples.every((value) => Math.abs(value - rate) < 1e-8)) {
        rates[resolution] = formatNumber(rate)
      }
    }
  }
  return rates
}
