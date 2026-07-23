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
import { api } from '@/lib/api'

export type BillingCapabilityField = {
  path: string
  type: string
  required: boolean
  enum_values: string[]
}

export type BillingCapabilityChannel = {
  id: number
  name: string
  type: number
  upstream_model: string
  schema_version?: string
}

export type BillingIncompatibleChannel = BillingCapabilityChannel & {
  reason: string
}

export type BillingCapability = {
  model: string
  canonical_available: boolean
  quota_per_unit: number
  checked_at: number
  schema_version: string
  fields: BillingCapabilityField[]
  compatible_channels: BillingCapabilityChannel[]
  incompatible_channels: BillingIncompatibleChannel[]
  validation_error: string
}

type BillingCapabilityWire = {
  model?: unknown
  canonical_available?: unknown
  compatible?: unknown
  quota_per_unit?: unknown
  checked_at?: unknown
  schema_version?: unknown
  fields?: unknown
  compatible_channels?: unknown
  incompatible_channels?: unknown
  validation_error?: unknown
  reason?: unknown
}

export type BillingCapabilityResponse = {
  success: boolean
  message: string
  data?: BillingCapability
}

export type BillingModelsPayload = {
  billing_mode: Record<string, string>
  billing_expr: Record<string, string>
  billing_schema: Record<string, string>
}

export type BillingModelsResponse = {
  success: boolean
  message: string
  data?: BillingModelsPayload
}

const requestConfig = {
  skipBusinessError: true,
  skipErrorHandler: true,
}

// Older backend versions did not expose quota_per_unit. Keep their established
// 50,000 raw-unit credit scale until every server has been upgraded.
export const DEFAULT_BILLING_QUOTA_PER_UNIT = 20

function toRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  return value as Record<string, unknown>
}

function toString(value: unknown): string {
  if (typeof value !== 'string') return ''
  return value.trim()
}

function toNumber(value: unknown, fallback = 0): number {
  const parsed = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

function toBoolean(value: unknown): boolean {
  return value === true || value === 'true' || value === 1 || value === '1'
}

function normalizeField(value: unknown): BillingCapabilityField | null {
  const field = toRecord(value)
  if (!field) return null

  return {
    path: toString(field.path),
    type: toString(field.type).toLowerCase(),
    required: toBoolean(field.required),
    enum_values: Array.isArray(field.enum_values)
      ? field.enum_values.map(toString).filter(Boolean)
      : [],
  }
}

function normalizeChannel(
  value: unknown,
  incompatible: boolean
): BillingCapabilityChannel | BillingIncompatibleChannel | null {
  const channel = toRecord(value)
  if (!channel) return null

  const normalized = {
    id: toNumber(channel.channel_id ?? channel.id),
    name: toString(channel.channel_name ?? channel.name),
    type: toNumber(channel.channel_type ?? channel.type),
    upstream_model: toString(channel.upstream_model),
    schema_version: toString(channel.schema_version),
  }
  if (!incompatible) return normalized

  return {
    ...normalized,
    reason: toString(channel.incompatibility ?? channel.reason),
  }
}

function normalizeChannels(
  value: unknown,
  incompatible: boolean
): BillingCapabilityChannel[] | BillingIncompatibleChannel[] {
  if (!Array.isArray(value)) return []
  return value
    .map((channel) => normalizeChannel(channel, incompatible))
    .filter(
      (
        channel
      ): channel is BillingCapabilityChannel | BillingIncompatibleChannel =>
        channel !== null
    )
}

function normalizeQuotaPerUnit(value: unknown): number {
  const quotaPerUnit = toNumber(value, DEFAULT_BILLING_QUOTA_PER_UNIT)
  return quotaPerUnit > 0 ? quotaPerUnit : DEFAULT_BILLING_QUOTA_PER_UNIT
}

function normalizeBillingCapability(
  value: BillingCapabilityWire,
  model: string
): BillingCapability {
  return {
    model: toString(value.model) || model,
    canonical_available: toBoolean(
      value.canonical_available ?? value.compatible
    ),
    quota_per_unit: normalizeQuotaPerUnit(value.quota_per_unit),
    checked_at: Math.max(0, toNumber(value.checked_at)),
    schema_version: toString(value.schema_version),
    fields: Array.isArray(value.fields)
      ? value.fields
          .map(normalizeField)
          .filter((field): field is BillingCapabilityField => field !== null)
      : [],
    compatible_channels: normalizeChannels(
      value.compatible_channels,
      false
    ) as BillingCapabilityChannel[],
    incompatible_channels: normalizeChannels(
      value.incompatible_channels,
      true
    ) as BillingIncompatibleChannel[],
    validation_error: toString(value.validation_error ?? value.reason),
  }
}

export async function getBillingCapability(
  model: string
): Promise<BillingCapabilityResponse> {
  const res = await api.get<BillingCapabilityResponse>(
    '/api/option/billing-capabilities',
    {
      ...requestConfig,
      params: { model },
    }
  )
  if (!res.data.data) return res.data
  const capability = res.data.data as BillingCapabilityWire
  return {
    ...res.data,
    data: normalizeBillingCapability(capability, model),
  }
}

export async function saveBillingModels(payload: BillingModelsPayload) {
  const res = await api.put<BillingModelsResponse>(
    '/api/option/billing-models',
    payload,
    requestConfig
  )
  return res.data
}
