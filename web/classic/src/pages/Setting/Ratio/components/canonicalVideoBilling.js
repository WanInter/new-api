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
export const CANONICAL_DEFAULT_RATE_KEY = '__all__';

const DEFAULT_QUOTA_PER_UNIT = 20;
const LEGACY_CREDIT_SCALE = 1000000 / DEFAULT_QUOTA_PER_UNIT;
const OMITTED_SPECIFICATION_VALUE = '__omitted__';
const NUMERIC_DRAFT_REGEX = /^(?:\d+(?:\.\d*)?|\.\d*)?$/;
const NUMBER_PATTERN = '[0-9.eE+-]+';
const SUPPORTED_FIELD_TYPES = new Set(['number', 'string', 'boolean']);

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
    type: toString(field.type).toLowerCase(),
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
    canonical_applicable:
      capability.canonical_applicable === undefined &&
      capability.applicable === undefined
        ? true
        : toBoolean(
            capability.canonical_applicable ?? capability.applicable,
          ),
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

function normalizeEnumValue(field, value) {
  const text = String(value).trim();
  switch (field.type) {
    case 'string':
      return text ? text : null;
    case 'number': {
      const number = Number(text);
      return Number.isFinite(number) ? formatNumber(number) : null;
    }
    case 'boolean':
      if (text === 'true' || text === 'false') return text;
      return null;
    default:
      return null;
  }
}

function getEnumValues(field) {
  const values = (field?.enum_values || [])
    .map((value) => normalizeEnumValue(field, value))
    .filter((value) => value !== null);
  return [...new Set(values)];
}

function getFieldName(path) {
  return String(path || '').replace(/^billing\./, '');
}

function buildSpecificationKey(entries) {
  if (entries.length === 0) return CANONICAL_DEFAULT_RATE_KEY;
  return JSON.stringify(
    entries.map((entry) => [
      entry.field.path,
      entry.omitted ? null : entry.value,
    ]),
  );
}

function buildSpecificationLabel(entries) {
  if (entries.length === 0) return '';
  const entryLabel = (entry) =>
    entry.omitted ? OMITTED_SPECIFICATION_VALUE : entry.value;
  if (entries.length === 1) return entryLabel(entries[0]);
  return entries
    .map((entry) => `${getFieldName(entry.field.path)}=${entryLabel(entry)}`)
    .join(', ');
}

export function formatCanonicalSpecificationLabel(specification, t) {
  const entries = specification?.entries || [];
  const translate = typeof t === 'function' ? t : (value) => value;
  const entryLabel = (entry) =>
    entry.omitted ? translate('Provider default') : entry.value;
  if (entries.length === 0) return '';
  if (entries.length === 1) return entryLabel(entries[0]);
  return entries
    .map((entry) => `${getFieldName(entry.field.path)}=${entryLabel(entry)}`)
    .join(', ');
}

function buildSpecifications(fields) {
  let specifications = [{ entries: [] }];
  for (const field of fields) {
    const entries = getEnumValues(field).map((value) => ({
      field,
      value,
      omitted: false,
    }));
    if (!field.required) {
      entries.unshift({ field, value: '', omitted: true });
    }
    const next = [];
    for (const specification of specifications) {
      for (const entry of entries) {
        next.push({
          entries: [...specification.entries, entry],
        });
      }
    }
    specifications = next;
  }
  return specifications.map((specification) => ({
    ...specification,
    key: buildSpecificationKey(specification.entries),
    label: buildSpecificationLabel(specification.entries),
  }));
}

export function getCanonicalPricingSchema(
  capability,
  pricingMode = 'per-second',
) {
  const fields = capability?.fields || [];
  const durationField = fields.find(
    (field) => field.path === CANONICAL_DURATION_PATH,
  );
  const priceMode =
    pricingMode === 'per-second' && durationField?.required
      ? 'per-second'
      : 'fixed';
  const specificationFields =
    priceMode === 'per-second'
      ? fields.filter((field) => field.path !== CANONICAL_DURATION_PATH)
      : fields;
  const durations = getEnumValues(durationField)
    .map((value) => Number(value))
    .filter((value) => Number.isFinite(value) && value > 0)
    .sort((a, b) => a - b);
  const specifications = buildSpecifications(specificationFields);

  return {
    fields,
    durationField,
    specificationFields,
    durations,
    specifications,
    rateKeys: specifications.map((specification) => specification.key),
    priceMode,
  };
}

function isSupportedCanonicalField(field) {
  if (!field || !/^billing\.[^.]+$/.test(field.path)) return false;
  if (!SUPPORTED_FIELD_TYPES.has(field.type)) return false;
  return (
    field.enum_values.length > 0 &&
    getEnumValues(field).length === field.enum_values.length
  );
}

export function getCanonicalPricingBlockReason(capability) {
  if (!capability) return '';
  if ((capability.incompatible_channels || []).length > 0) {
    return capability.incompatible_channels
      .map((channel) => `${channel.name}: ${channel.reason}`)
      .join('; ');
  }
  if (capability.validation_error) return capability.validation_error;
  if (!capability.canonical_available) return 'canonical_unavailable';
  if (!capability.schema_version) return 'canonical_schema_incomplete';

  const schema = getCanonicalPricingSchema(capability);
  const paths = schema.fields.map((field) => field.path);
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
    return 'canonical_schema_incomplete';
  }
  return '';
}

export function isCanonicalPricingAvailable(capability) {
  return Boolean(capability && !getCanonicalPricingBlockReason(capability));
}

export function isCanonicalRateDraft(value) {
  return NUMERIC_DRAFT_REGEX.test(value);
}

export function areCanonicalRatesComplete(rates, rateKeys) {
  return rateKeys.every((key) => {
    const value = rates?.[key];
    return (
      value !== undefined &&
      value !== '' &&
      Number.isFinite(Number(value)) &&
      Number(value) >= 0
    );
  });
}

export function hasCanonicalSchemaVersionConflict(
  configuredSchema,
  capability,
) {
  if (!capability) return false;
  return (
    String(configuredSchema || '').trim() !==
    String(capability.schema_version || '').trim()
  );
}

function enumLiteral(field, value) {
  switch (field.type) {
    case 'number':
      return formatNumber(value);
    case 'boolean':
      return value === 'true' ? 'true' : 'false';
    default:
      return JSON.stringify(value);
  }
}

function specificationCondition(specification) {
  return specification.entries
    .map(({ field, value, omitted }) =>
      omitted
        ? `param(${JSON.stringify(field.path)}) == nil`
        : `param(${JSON.stringify(field.path)}) == ${enumLiteral(field, value)}`,
    )
    .join(' && ');
}

function specificationTierStem(specification) {
  if (specification.entries.length === 0) return '';
  if (specification.entries.some(({ field }) => !field.required)) {
    return `spec:${specification.key}`;
  }
  if (specification.entries.length === 1) {
    return specification.entries[0].value;
  }
  return specification.entries
    .map(({ field, value }) => `${getFieldName(field.path)}=${value}`)
    .join('|');
}

function tierLabel(specification, duration) {
  const stem = specificationTierStem(specification);
  if (duration !== null) return stem ? `${stem}_${duration}s` : `${duration}s`;
  return stem || 'default';
}

function buildDurationExpression(specification, durations, rate, creditScale) {
  const branches = durations.map((duration) => {
    const rawCost = formatNumber(rate * duration * creditScale);
    return `param(${JSON.stringify(
      CANONICAL_DURATION_PATH,
    )}) == ${duration} ? tier(${JSON.stringify(
      tierLabel(specification, duration),
    )}, ${rawCost})`;
  });
  return `(${branches.join(' : ')} : tier("unsupported_duration", 0))`;
}

export function buildCanonicalPricingExpr(
  capability,
  rates,
  pricingMode = 'per-second',
) {
  if (!isCanonicalPricingAvailable(capability)) return '';
  const schema = getCanonicalPricingSchema(capability, pricingMode);
  if (!areCanonicalRatesComplete(rates, schema.rateKeys)) return '';
  const creditScale = getCanonicalCreditScale(capability);

  if (schema.priceMode === 'per-second') {
    const durationExpressions = schema.specifications.map((specification) => {
      const rate = Number(rates?.[specification.key]) || 0;
      return buildDurationExpression(
        specification,
        schema.durations,
        rate,
        creditScale,
      );
    });
    if (schema.specificationFields.length === 0) return durationExpressions[0];
    return schema.specifications
      .map(
        (specification, index) =>
          `${specificationCondition(specification)} ? ${durationExpressions[index]}`,
      )
      .concat('tier("unsupported_specification", 0)')
      .join(' : ');
  }

  return schema.specifications
    .map((specification) => {
      const rawCost = formatNumber(
        (Number(rates?.[specification.key]) || 0) * creditScale,
      );
      return `${specificationCondition(specification)} ? tier(${JSON.stringify(
        tierLabel(specification, null),
      )}, ${rawCost})`;
    })
    .concat('tier("unsupported_specification", 0)')
    .join(' : ');
}

function extractTierRawCost(expr, label) {
  const match = String(expr || '').match(
    new RegExp(
      `tier\\(${escapeRegExp(JSON.stringify(label))},\\s*(${NUMBER_PATTERN})\\)`,
    ),
  );
  if (!match) return null;
  const rawCost = Number(match[1]);
  return Number.isFinite(rawCost) ? rawCost : null;
}

export function detectCanonicalPricingMode(expr, capability) {
  const requiredDuration = capability?.fields?.find(
    (field) => field.path === CANONICAL_DURATION_PATH,
  );
  if (!requiredDuration?.required) return 'fixed';
  return /tier\("[^"]*unsupported_duration",\s*0\)/.test(String(expr || ''))
    ? 'per-second'
    : 'fixed';
}

export function extractCanonicalPricingRates(
  expr,
  capability,
  pricingMode = detectCanonicalPricingMode(expr, capability),
) {
  if (!capability || !String(expr || '').includes('param("billing.')) return {};
  const schema = getCanonicalPricingSchema(capability, pricingMode);
  const creditScale = getCanonicalCreditScale(capability);
  const rates = {};

  for (const specification of schema.specifications) {
    if (schema.priceMode === 'per-second') {
      const samples = schema.durations
        .map((duration) => {
          const rawCost = extractTierRawCost(
            expr,
            tierLabel(specification, duration),
          );
          return rawCost === null ? null : rawCost / duration / creditScale;
        })
        .filter((value) => value !== null);
      if (
        samples.length === schema.durations.length &&
        samples.every((value) => Math.abs(value - samples[0]) < 1e-8)
      ) {
        rates[specification.key] = formatNumber(samples[0]);
      }
      continue;
    }

    const rawCost = extractTierRawCost(expr, tierLabel(specification, null));
    if (rawCost !== null) {
      rates[specification.key] = formatNumber(rawCost / creditScale);
    }
  }
  return rates;
}

// Compatibility exports for callers that still use the original video-specific names.
export const getCanonicalVideoDimensions = getCanonicalPricingSchema;
export const getCanonicalVideoBlockReason = getCanonicalPricingBlockReason;
export const isCanonicalVideoAvailable = isCanonicalPricingAvailable;
export const buildCanonicalVideoExpr = buildCanonicalPricingExpr;
export const extractCanonicalVideoRates = extractCanonicalPricingRates;
