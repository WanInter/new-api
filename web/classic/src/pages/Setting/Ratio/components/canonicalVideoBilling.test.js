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
import assert from 'node:assert/strict';
import test from 'node:test';
import {
  buildCanonicalPricingExpr,
  detectCanonicalPricingMode,
  extractCanonicalPricingRates,
  formatCanonicalSpecificationLabel,
  getCanonicalPricingBlockReason,
  getCanonicalPricingSchema,
  hasCanonicalSchemaVersionConflict,
  normalizeCanonicalBillingCapability,
} from './canonicalVideoBilling.js';

const durationField = {
  path: 'billing.duration_seconds',
  type: 'number',
  required: true,
  enum_values: ['4', '5'],
};

const optionalDurationField = {
  ...durationField,
  required: false,
};

const resolutionField = {
  path: 'billing.resolution',
  type: 'string',
  required: true,
  enum_values: ['480p', '720p'],
};

const audioField = {
  path: 'billing.audio_enabled',
  type: 'boolean',
  required: true,
  enum_values: ['false', 'true'],
};

const optionalAudioField = {
  ...audioField,
  required: false,
};

const optionalOffPeakField = {
  path: 'billing.off_peak',
  type: 'boolean',
  required: false,
  enum_values: ['false', 'true'],
};

function capability(fields, overrides = {}) {
  return {
    model: 'video-model',
    canonical_available: true,
    quota_per_unit: 20,
    checked_at: 1,
    schema_version: 'video.v1',
    fields,
    compatible_channels: [{ id: 1, name: 'video-channel' }],
    incompatible_channels: [],
    validation_error: '',
    ...overrides,
  };
}

function assertPricingRoundTrip(fields, mode, values) {
  const currentCapability = capability(fields);
  const schema = getCanonicalPricingSchema(currentCapability, mode);
  const rates = Object.fromEntries(
    schema.rateKeys.map((key, index) => [key, values[index]]),
  );
  const expression = buildCanonicalPricingExpr(currentCapability, rates, mode);

  assert.equal(detectCanonicalPricingMode(expression, currentCapability), mode);
  assert.deepEqual(
    extractCanonicalPricingRates(expression, currentCapability, mode),
    rates,
  );
  for (const match of expression.matchAll(/param\("([^"]+)"\)/g)) {
    assert.match(match[1], /^billing\./);
  }
}

test('normalizes video applicability without breaking older responses', () => {
  assert.equal(
    normalizeCanonicalBillingCapability({ applicable: false })
      .canonical_applicable,
    false,
  );
  assert.equal(
    normalizeCanonicalBillingCapability({}).canonical_applicable,
    true,
  );
});

test('round-trips duration-only per-second pricing', () => {
  assertPricingRoundTrip([durationField], 'per-second', ['3']);
});

test('prices an omitted optional duration as a fixed provider-default tier', () => {
  const currentCapability = capability([
    optionalDurationField,
    resolutionField,
  ]);
  const schema = getCanonicalPricingSchema(currentCapability, 'per-second');
  const rates = Object.fromEntries(
    schema.rateKeys.map((key, index) => [key, String(index)]),
  );
  const expression = buildCanonicalPricingExpr(
    currentCapability,
    rates,
    'per-second',
  );

  assert.equal(schema.priceMode, 'fixed');
  assert.equal(schema.specifications.length, 6);
  assert.equal(
    detectCanonicalPricingMode(expression, currentCapability),
    'fixed',
  );
  assert.match(expression, /param\("billing\.duration_seconds"\) == nil/);
  assert.deepEqual(
    extractCanonicalPricingRates(expression, currentCapability, 'fixed'),
    rates,
  );
});

test('does not generate an expression for implicit zero prices', () => {
  const currentCapability = capability([durationField]);
  const rateKey = getCanonicalPricingSchema(currentCapability).rateKeys[0];

  assert.equal(
    buildCanonicalPricingExpr(currentCapability, { [rateKey]: '' }),
    '',
  );
  assert.match(
    buildCanonicalPricingExpr(currentCapability, { [rateKey]: '0' }),
    /tier\("4s", 0\)/,
  );
});

test('round-trips per-second pricing across an enum specification', () => {
  assertPricingRoundTrip([durationField, resolutionField], 'per-second', [
    '3',
    '4',
  ]);
});

test('uses typed literals for multi-field specifications', () => {
  const currentCapability = capability([
    durationField,
    resolutionField,
    audioField,
  ]);
  const schema = getCanonicalPricingSchema(currentCapability, 'per-second');
  const rates = Object.fromEntries(
    schema.rateKeys.map((key, index) => [key, String(index + 1)]),
  );
  const expression = buildCanonicalPricingExpr(
    currentCapability,
    rates,
    'per-second',
  );

  assert.match(expression, /param\("billing\.audio_enabled"\) == false/);
  assert.deepEqual(
    extractCanonicalPricingRates(expression, currentCapability, 'per-second'),
    rates,
  );
});

test('round-trips optional fields with an explicit provider-default tier', () => {
  const currentCapability = capability([durationField, optionalAudioField]);
  const schema = getCanonicalPricingSchema(currentCapability, 'per-second');
  const rates = Object.fromEntries(
    schema.rateKeys.map((key, index) => [key, String(index + 1)]),
  );
  const expression = buildCanonicalPricingExpr(
    currentCapability,
    rates,
    'per-second',
  );

  assert.equal(getCanonicalPricingBlockReason(currentCapability), '');
  assert.equal(schema.specifications.length, 3);
  assert.deepEqual(
    schema.specifications.map(({ entries }) => entries[0].omitted),
    [true, false, false],
  );
  assert.equal(
    formatCanonicalSpecificationLabel(schema.specifications[0], (key) =>
      key === 'Provider default' ? 'provider-default' : key,
    ),
    'provider-default',
  );
  assert.match(expression, /param\("billing\.audio_enabled"\) == nil/);
  assert.match(expression, /param\("billing\.audio_enabled"\) == false/);
  assert.match(expression, /param\("billing\.audio_enabled"\) == true/);
  assert.deepEqual(
    extractCanonicalPricingRates(expression, currentCapability, 'per-second'),
    rates,
  );
});

test('requires a price for the optional-field omission tier', () => {
  const currentCapability = capability([durationField, optionalAudioField]);
  const schema = getCanonicalPricingSchema(currentCapability, 'per-second');
  const rates = Object.fromEntries(schema.rateKeys.map((key) => [key, '1']));
  delete rates[schema.specifications[0].key];

  assert.equal(
    buildCanonicalPricingExpr(currentCapability, rates, 'per-second'),
    '',
  );
});

test('builds the cartesian product for multiple optional fields', () => {
  const currentCapability = capability([
    optionalAudioField,
    optionalOffPeakField,
  ]);
  const schema = getCanonicalPricingSchema(currentCapability, 'fixed');

  assert.equal(schema.specifications.length, 9);
  assert.equal(new Set(schema.rateKeys).size, 9);
  assertPricingRoundTrip(
    [optionalAudioField, optionalOffPeakField],
    'fixed',
    schema.rateKeys.map((_, index) => String(index + 1)),
  );
});

test('round-trips fixed pricing with duration treated as a specification', () => {
  assertPricingRoundTrip([durationField, resolutionField], 'fixed', [
    '10',
    '11',
    '12',
    '13',
  ]);
});

test('round-trips fixed pricing without a duration field', () => {
  assertPricingRoundTrip([resolutionField], 'fixed', ['8', '12']);
});

test('recognizes and restores the previous resolution pricing expression', () => {
  const currentCapability = capability([durationField, resolutionField]);
  const legacyExpression =
    'param("billing.resolution") == "480p" ? (param("billing.duration_seconds") == 4 ? tier("480p_4s", 600000) : param("billing.duration_seconds") == 5 ? tier("480p_5s", 750000) : tier("480p_unsupported_duration", 0)) : param("billing.resolution") == "720p" ? (param("billing.duration_seconds") == 4 ? tier("720p_4s", 800000) : param("billing.duration_seconds") == 5 ? tier("720p_5s", 1000000) : tier("720p_unsupported_duration", 0)) : tier("unsupported_resolution", 0)';

  assert.equal(
    detectCanonicalPricingMode(legacyExpression, currentCapability),
    'per-second',
  );
  assert.deepEqual(
    Object.values(
      extractCanonicalPricingRates(legacyExpression, currentCapability),
    ),
    ['3', '4'],
  );
});

test('blocks incompatible channel schemas', () => {
  const reason = getCanonicalPricingBlockReason(
    capability([durationField], {
      canonical_available: false,
      incompatible_channels: [
        { id: 2, name: 'other-channel', reason: 'schema mismatch' },
      ],
    }),
  );
  assert.equal(reason, 'other-channel: schema mismatch');
});

test('detects canonical schema version conflicts without auto-upgrading', () => {
  const currentCapability = capability([durationField]);

  assert.equal(
    hasCanonicalSchemaVersionConflict('video.v1', currentCapability),
    false,
  );
  assert.equal(
    hasCanonicalSchemaVersionConflict('video.v0', currentCapability),
    true,
  );
  assert.equal(hasCanonicalSchemaVersionConflict('', currentCapability), true);
});
