/*
Copyright (C) 2025 QuantumNous

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

export const CANONICAL_DURATION_PATH = 'billing.duration_seconds';
export const CANONICAL_RESOLUTION_PATH = 'billing.resolution';
const DEFAULT_QUOTA_PER_UNIT = 20;
const LEGACY_CREDIT_SCALE = 1000000 / DEFAULT_QUOTA_PER_UNIT;

const DURATION_EXPR = `param("${CANONICAL_DURATION_PATH}")`;
const RESOLUTION_EXPR = `param("${CANONICAL_RESOLUTION_PATH}")`;
const NUMERIC_DRAFT_REGEX = /^(?:\d+(?:\.\d*)?|\.\d*)?$/;

function escapeRegExp(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function formatNumber(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return '0';
  return String(Number(number.toFixed(8)));
}

function toRecord(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  return value;
}

function toString(value) {
  return typeof value === 'string' ? value.trim() : '';
}

function toNumber(value, fallback = 0) {
  const number = typeof value === 'number' ? value : Number(value);
  return Number.isFinite(number) ? number : fallback;
}

function toBoolean(value) {
  return value === true || value === 'true' || value === 1 || value === '1';
}

function normalizeChannel(value, incompatible) {
  const channel = toRecord(value);
  if (!channel) return null;
  const normalized = {
    id: toNumber(channel.channel_id ?? channel.id),
    name: toString(channel.channel_name ?? channel.name),
    type: toNumber(channel.channel_type ?? channel.type),
    upstream_model: toString(channel.upstream_model),
    schema_version: toString(channel.schema_version),
  };
  if (!incompatible) return normalized;
  return {
    ...normalized,
    reason: toString(channel.incompatibility ?? channel.reason),
  };
}

function normalizeChannels(value, incompatible) {
  if (!Array.isArray(value)) return [];
  return value
    .map((channel) => normalizeChannel(channel, incompatible))
    .filter(Boolean);
}

function normalizeField(value) {
  const field = toRecord(value);
  if (!field) return null;
  return {
    path: toString(field.path),
    type: toString(field.type),
    required: toBoolean(field.required),
    enum_values: Array.isArray(field.enum_values)
      ? field.enum_values.map(toString).filter(Boolean)
      : [],
  };
}

export function normalizeCanonicalBillingCapability(value, fallbackModel = '') {
  const capability = toRecord(value) || {};
  const quotaPerUnit = toNumber(
    capability.quota_per_unit,
    DEFAULT_QUOTA_PER_UNIT,
  );
  return {
    model: toString(capability.model) || fallbackModel,
    canonical_available: toBoolean(
      capability.canonical_available ?? capability.compatible,
    ),
    quota_per_unit: quotaPerUnit > 0 ? quotaPerUnit : DEFAULT_QUOTA_PER_UNIT,
    checked_at: Math.max(0, toNumber(capability.checked_at)),
    schema_version: toString(capability.schema_version),
    fields: Array.isArray(capability.fields)
      ? capability.fields.map(normalizeField).filter(Boolean)
      : [],
    compatible_channels: normalizeChannels(
      capability.compatible_channels,
      false,
    ),
    incompatible_channels: normalizeChannels(
      capability.incompatible_channels,
      true,
    ),
    validation_error: toString(
      capability.validation_error ?? capability.reason,
    ),
  };
}

function getCanonicalCreditScale(capability) {
  const quotaPerUnit = toNumber(capability?.quota_per_unit, 0);
  return quotaPerUnit > 0 ? 1000000 / quotaPerUnit : LEGACY_CREDIT_SCALE;
}

export function getCanonicalVideoDimensions(capability) {
  const fields = capability?.fields || [];
  const durationField = fields.find(
    (field) => field.path === CANONICAL_DURATION_PATH,
  );
  const resolutionField = fields.find(
    (field) => field.path === CANONICAL_RESOLUTION_PATH,
  );
  const durations = (durationField?.enum_values || [])
    .map((value) => Number(value))
    .filter((value) => Number.isFinite(value) && value > 0)
    .sort((a, b) => a - b);
  const resolutions = (resolutionField?.enum_values || []).filter(Boolean);
  return { durationField, resolutionField, durations, resolutions };
}

export function getCanonicalVideoBlockReason(capability) {
  if (!capability) return '';
  if (capability.validation_error) return capability.validation_error;
  if ((capability.incompatible_channels || []).length > 0) {
    return capability.incompatible_channels
      .map((channel) => `${channel.name}: ${channel.reason}`)
      .join('; ');
  }
  if (!capability.canonical_available) return 'canonical_unavailable';

  const { durationField, resolutionField, durations, resolutions } =
    getCanonicalVideoDimensions(capability);
  if (
    durationField?.type !== 'number' ||
    !durationField.required ||
    resolutionField?.type !== 'string' ||
    !resolutionField.required ||
    durations.length === 0 ||
    resolutions.length === 0
  ) {
    return 'canonical_schema_incomplete';
  }
  return '';
}

export function isCanonicalVideoAvailable(capability) {
  return Boolean(capability && !getCanonicalVideoBlockReason(capability));
}

export function isCanonicalRateDraft(value) {
  return NUMERIC_DRAFT_REGEX.test(value);
}

export function buildCanonicalVideoExpr(capability, rates) {
  const { durations, resolutions } = getCanonicalVideoDimensions(capability);
  if (durations.length === 0 || resolutions.length === 0) return '';
  const creditScale = getCanonicalCreditScale(capability);

  const durationExpr = (resolution) => {
    const rate = Number(rates?.[resolution]) || 0;
    const branches = durations.map((duration) => {
      const rawCost = formatNumber(rate * duration * creditScale);
      return `${DURATION_EXPR} == ${duration} ? tier(${JSON.stringify(
        `${resolution}_${duration}s`,
      )}, ${rawCost})`;
    });
    return `(${branches.join(' : ')} : tier(${JSON.stringify(
      `${resolution}_unsupported_duration`,
    )}, 0))`;
  };

  return resolutions
    .map(
      (resolution) =>
        `${RESOLUTION_EXPR} == ${JSON.stringify(resolution)} ? ${durationExpr(resolution)}`,
    )
    .concat('tier("unsupported_resolution", 0)')
    .join(' : ');
}

export function extractCanonicalVideoRates(expr, capability) {
  if (!capability || !String(expr || '').includes(DURATION_EXPR)) return {};
  const { durations, resolutions } = getCanonicalVideoDimensions(capability);
  if (durations.length === 0) return {};
  const creditScale = getCanonicalCreditScale(capability);

  return resolutions.reduce((rates, resolution) => {
    const values = durations
      .map((duration) => {
        const label = `${resolution}_${duration}s`;
        const match = String(expr).match(
          new RegExp(
            `tier\\(${escapeRegExp(JSON.stringify(label))},\\s*([0-9.]+)\\)`,
          ),
        );
        if (!match) return null;
        const rawCost = Number(match[1]);
        return Number.isFinite(rawCost)
          ? rawCost / duration / creditScale
          : null;
      })
      .filter((value) => value !== null);
    if (
      values.length === durations.length &&
      values.every((value) => Math.abs(value - values[0]) < 1e-8)
    ) {
      rates[resolution] = formatNumber(values[0]);
    }
    return rates;
  }, {});
}
