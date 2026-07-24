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
import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  Banner,
  Button,
  Card,
  Input,
  Radio,
  RadioGroup,
  Select,
  Tag,
} from '@douyinfe/semi-ui';
import { API } from '../../../../helpers';
import {
  CANONICAL_DURATION_PATH,
  areCanonicalRatesComplete,
  buildCanonicalPricingExpr,
  detectCanonicalPricingMode,
  extractCanonicalPricingRates,
  formatCanonicalSpecificationLabel,
  getCanonicalPricingBlockReason,
  getCanonicalPricingSchema,
  isCanonicalPricingAvailable,
  isCanonicalRateDraft,
  normalizeCanonicalBillingCapability,
} from './canonicalVideoBilling';

function SchemaSummary({ capability, t }) {
  return (
    <Card bodyStyle={{ padding: 12 }} style={{ marginBottom: 12 }}>
      <div className='flex items-center justify-between gap-3 mb-3'>
        <div className='font-medium'>{t('Canonical billing schema')}</div>
        {capability.schema_version ? (
          <Tag size='small' color='blue'>
            {t('Canonical schema version')}: {capability.schema_version}
          </Tag>
        ) : null}
      </div>
      <div className='text-xs text-gray-500 mb-1'>
        {t('Canonical billing fields')}
      </div>
      <div className='flex flex-wrap gap-2 mb-3'>
        {capability.fields.map((field) => (
          <Tag key={field.path} size='small' color='grey'>
            {field.path} ({field.type})
            {!field.required ? ` [${t('Optional')}]` : ''}
          </Tag>
        ))}
      </div>
      <div className='grid grid-cols-1 md:grid-cols-2 gap-3'>
        <div>
          <div className='text-xs text-gray-500 mb-1'>
            {t('Compatible channels')}
          </div>
          <div className='flex flex-wrap gap-2'>
            {capability.compatible_channels?.length ? (
              capability.compatible_channels.map((channel) => (
                <Tag key={channel.id} size='small' color='green'>
                  {channel.name}
                </Tag>
              ))
            ) : (
              <span className='text-xs text-gray-500'>
                {t('No compatible channels')}
              </span>
            )}
          </div>
        </div>
        <div>
          <div className='text-xs text-gray-500 mb-1'>
            {t('Incompatible channels')}
          </div>
          {capability.incompatible_channels?.length ? (
            capability.incompatible_channels.map((channel) => (
              <div
                key={channel.id}
                className='text-xs text-red-600 break-words mb-1'
              >
                {channel.name}: {channel.reason}
              </div>
            ))
          ) : (
            <span className='text-xs text-gray-500'>{t('None')}</span>
          )}
        </div>
      </div>
      {capability.checked_at > 0 ? (
        <div className='text-xs text-gray-500 mt-3'>
          {t('Last validation')}:{' '}
          {new Date(capability.checked_at * 1000).toLocaleString()}
        </div>
      ) : null}
    </Card>
  );
}

function localizedBlockReason(blockReason, t) {
  if (blockReason === 'canonical_unavailable') {
    return t(
      'Every routed channel must use the same canonical schema before dynamic video pricing can be enabled.',
    );
  }
  if (blockReason === 'canonical_schema_incomplete') {
    return t('The canonical billing schema is not supported by this editor.');
  }
  return blockReason;
}

export default function CanonicalVideoBillingEditor({
  model,
  onCanonicalChange,
  onValidationChange,
  t,
}) {
  const [capability, setCapability] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [rates, setRates] = useState({});
  const [previewDuration, setPreviewDuration] = useState('');
  const [pricingMode, setPricingMode] = useState('per-second');
  const [draftEnabled, setDraftEnabled] = useState(false);
  const lastGeneratedExprRef = useRef({ modelName: '', expression: '' });

  const modelName = model?.name || '';
  const persistedEnabled = Boolean(model?.billingSchema);
  const canonicalEnabled = draftEnabled || persistedEnabled;
  const schema = useMemo(
    () => getCanonicalPricingSchema(capability, pricingMode),
    [capability, pricingMode],
  );
  const available = isCanonicalPricingAvailable(capability);
  const blockReason = getCanonicalPricingBlockReason(capability);
  const expression = useMemo(
    () =>
      capability
        ? buildCanonicalPricingExpr(capability, rates, pricingMode)
        : '',
    [capability, pricingMode, rates],
  );
  const configured = areCanonicalRatesComplete(rates, schema.rateKeys);

  useEffect(() => {
    if (!onValidationChange || !modelName) return;
    let validationError = '';
    if (canonicalEnabled) {
      if (loading) {
        validationError = t('Loading billing capability...');
      } else if (error) {
        validationError = error;
      } else if (!capability || !available) {
        validationError = localizedBlockReason(blockReason, t);
      } else if (!configured) {
        validationError = t(
          'Set a credit price for every supported specification.',
        );
      }
    }
    onValidationChange({
      modelName,
      active: canonicalEnabled,
      valid: validationError === '',
      reason: validationError,
    });
  }, [
    available,
    blockReason,
    canonicalEnabled,
    capability,
    configured,
    error,
    loading,
    modelName,
    onValidationChange,
    t,
  ]);

  useEffect(() => {
    let active = true;
    if (!modelName) {
      setCapability(null);
      setError('');
      setLoading(false);
      return () => {
        active = false;
      };
    }

    setLoading(true);
    setError('');
    API.get('/api/option/billing-capabilities', {
      params: { model: modelName },
      skipErrorHandler: true,
    })
      .then((response) => {
        if (!active) return;
        if (!response?.data?.success || !response?.data?.data) {
          setCapability(null);
          setError(response?.data?.message || t('Request failed'));
          return;
        }
        setCapability(
          normalizeCanonicalBillingCapability(response.data.data, modelName),
        );
      })
      .catch((requestError) => {
        if (!active) return;
        setCapability(null);
        setError(requestError?.message || t('Request failed'));
      })
      .finally(() => {
        if (active) setLoading(false);
      });

    return () => {
      active = false;
    };
  }, [modelName, t]);

  useEffect(() => {
    if (!capability) return;
    if (
      modelName === lastGeneratedExprRef.current.modelName &&
      model?.billingExpr === lastGeneratedExprRef.current.expression
    ) {
      return;
    }
    const detectedMode = model?.billingSchema
      ? detectCanonicalPricingMode(model.billingExpr, capability)
      : capability.fields.some(
            (field) => field.path === CANONICAL_DURATION_PATH && field.required,
          )
        ? 'per-second'
        : 'fixed';
    setDraftEnabled(Boolean(model?.billingSchema));
    setPricingMode(detectedMode);
    const nextRates = model?.billingSchema
      ? extractCanonicalPricingRates(
          model.billingExpr,
          capability,
          detectedMode,
        )
      : {};
    setRates(nextRates);
  }, [capability, model?.billingExpr, model?.billingSchema]);

  useEffect(() => {
    if (!schema.durations.length) {
      setPreviewDuration('');
      return;
    }
    setPreviewDuration((current) =>
      schema.durations.some((duration) => String(duration) === current)
        ? current
        : String(schema.durations[schema.durations.length - 1]),
    );
  }, [schema.durations]);

  const enableCanonical = () => {
    if (!capability || !available) return;
    const nextRates = schema.rateKeys.reduce((next, key) => {
      next[key] = rates[key] ?? '';
      return next;
    }, {});
    setDraftEnabled(true);
    setRates(nextRates);
    if (!areCanonicalRatesComplete(nextRates, schema.rateKeys)) return;
    const nextExpression = buildCanonicalPricingExpr(
      capability,
      nextRates,
      pricingMode,
    );
    lastGeneratedExprRef.current = {
      modelName,
      expression: nextExpression,
    };
    onCanonicalChange({
      billingExpr: nextExpression,
      billingSchema: capability.schema_version,
    });
  };

  const disableCanonical = () => {
    setDraftEnabled(false);
    if (!persistedEnabled) return;
    onCanonicalChange({
      billingExpr: model?.billingExpr || '',
      billingSchema: '',
    });
  };

  const updateRate = (key, value) => {
    if (!isCanonicalRateDraft(value)) return;
    const nextRates = { ...rates, [key]: value };
    setRates(nextRates);
    if (
      canonicalEnabled &&
      capability &&
      areCanonicalRatesComplete(nextRates, schema.rateKeys)
    ) {
      const nextExpression = buildCanonicalPricingExpr(
        capability,
        nextRates,
        pricingMode,
      );
      lastGeneratedExprRef.current = {
        modelName,
        expression: nextExpression,
      };
      onCanonicalChange({
        billingExpr: nextExpression,
        billingSchema: capability.schema_version,
      });
    }
  };

  const changePricingMode = (event) => {
    if (!capability) return;
    const nextMode = event.target.value;
    const nextSchema = getCanonicalPricingSchema(capability, nextMode);
    const nextRates = nextSchema.rateKeys.reduce((next, key) => {
      next[key] = rates[key] ?? '';
      return next;
    }, {});
    setPricingMode(nextMode);
    setRates(nextRates);
    if (
      canonicalEnabled &&
      areCanonicalRatesComplete(nextRates, nextSchema.rateKeys)
    ) {
      const nextExpression = buildCanonicalPricingExpr(
        capability,
        nextRates,
        nextMode,
      );
      lastGeneratedExprRef.current = {
        modelName,
        expression: nextExpression,
      };
      onCanonicalChange({
        billingExpr: nextExpression,
        billingSchema: capability.schema_version,
      });
    }
  };

  if (loading) {
    return (
      <Banner
        type='info'
        fullMode={false}
        closeIcon={null}
        style={{ marginBottom: 16 }}
        title={t('Canonical video billing')}
        description={t('Loading billing capability...')}
      />
    );
  }

  if (error) {
    return (
      <div style={{ marginBottom: 16 }}>
        <Banner
          type='warning'
          fullMode={false}
          closeIcon={null}
          style={{ marginBottom: canonicalEnabled ? 8 : 0 }}
          title={t('Canonical video billing')}
          description={`${t('Unable to load billing capability. Generic expressions remain available.')} ${error}`}
        />
        {canonicalEnabled ? (
          <Button theme='borderless' type='tertiary' onClick={disableCanonical}>
            {t('Use general expression pricing')}
          </Button>
        ) : null}
      </div>
    );
  }

  if (!capability) return null;

  if (!capability.canonical_applicable && !persistedEnabled) return null;

  if (!available) {
    return (
      <div style={{ marginBottom: 16 }}>
        <Banner
          type='warning'
          fullMode={false}
          closeIcon={null}
          title={t('Canonical video billing is unavailable for this model.')}
          description={localizedBlockReason(blockReason, t)}
          style={{ marginBottom: 12 }}
        />
        <SchemaSummary capability={capability} t={t} />
        {canonicalEnabled ? (
          <Button theme='borderless' type='tertiary' onClick={disableCanonical}>
            {t('Use general expression pricing')}
          </Button>
        ) : null}
      </div>
    );
  }

  const previewSeconds = Number(previewDuration);
  const isPerSecond = schema.priceMode === 'per-second';
  return (
    <div style={{ marginBottom: 16 }}>
      <SchemaSummary capability={capability} t={t} />
      <div className='flex flex-wrap gap-2 mb-3'>
        <Button
          type={canonicalEnabled ? 'primary' : 'tertiary'}
          theme={canonicalEnabled ? 'solid' : 'borderless'}
          onClick={enableCanonical}
        >
          {t('Use canonical video pricing')}
        </Button>
        {canonicalEnabled ? (
          <Button theme='borderless' type='tertiary' onClick={disableCanonical}>
            {t('Use general expression pricing')}
          </Button>
        ) : null}
      </div>

      {canonicalEnabled ? (
        <Card bodyStyle={{ padding: 16 }}>
          <div className='font-medium mb-1'>
            {isPerSecond
              ? t('Per-second credits')
              : t('Fixed credits by specification')}
          </div>
          <div className='text-xs text-gray-500 mb-4'>
            {t('Canonical video pricing only uses normalized billing fields.')}
          </div>
          {schema.durationField?.required ? (
            <div className='mb-4'>
              <div className='text-xs text-gray-500 mb-1'>
                {t('Pricing method')}
              </div>
              <RadioGroup
                type='button'
                value={pricingMode}
                onChange={changePricingMode}
              >
                <Radio value='per-second'>{t('Per second')}</Radio>
                <Radio value='fixed'>{t('Per specification')}</Radio>
              </RadioGroup>
            </div>
          ) : null}
          <div className='grid grid-cols-1 sm:grid-cols-2 gap-3 mb-4'>
            {schema.specifications.map((specification) => (
              <div key={specification.key}>
                <div className='text-sm font-medium mb-1'>
                  {formatCanonicalSpecificationLabel(specification, t) ||
                    t('All supported durations')}
                </div>
                <Input
                  value={rates[specification.key] ?? ''}
                  placeholder='0'
                  onChange={(value) => updateRate(specification.key, value)}
                  suffix={
                    isPerSecond ? t('Credits / second') : t('Credits / request')
                  }
                />
              </div>
            ))}
          </div>
          <div className='flex flex-wrap items-end gap-3 border-t pt-3 mb-3'>
            {isPerSecond ? (
              <div>
                <div className='text-xs text-gray-500 mb-1'>
                  {t('Preview duration')}
                </div>
                <Select
                  value={previewDuration}
                  style={{ width: 120 }}
                  onChange={setPreviewDuration}
                >
                  {schema.durations.map((duration) => (
                    <Select.Option key={duration} value={String(duration)}>
                      {duration}s
                    </Select.Option>
                  ))}
                </Select>
              </div>
            ) : null}
            <div className='flex flex-wrap gap-x-4 gap-y-1 text-sm pb-1'>
              {schema.specifications.map((specification) => {
                const rate = Number(rates[specification.key]) || 0;
                const credits = isPerSecond ? rate * previewSeconds : rate;
                const specificationLabel =
                  formatCanonicalSpecificationLabel(specification, t) ||
                  t('All supported durations');
                return (
                  <span key={specification.key}>
                    {isPerSecond
                      ? t(
                          '{{specification}}, {{duration}} seconds = {{credits}} credits',
                          {
                            specification: specificationLabel,
                            duration: previewSeconds || '-',
                            credits: Number.isFinite(credits) ? credits : 0,
                          },
                        )
                      : t('{{specification}} = {{credits}} credits', {
                          specification: specificationLabel,
                          credits: Number.isFinite(credits) ? credits : 0,
                        })}
                  </span>
                );
              })}
            </div>
          </div>
          {!configured ? (
            <div className='text-xs text-red-600 mb-3'>
              {t('Set a credit price for every supported specification.')}
            </div>
          ) : null}
          <div className='text-xs text-gray-500 mb-1'>
            {t('Generated canonical expression')}
          </div>
          <pre
            className='text-xs break-words whitespace-pre-wrap'
            style={{
              margin: 0,
              maxHeight: 180,
              overflow: 'auto',
              padding: 8,
              borderRadius: 4,
              background: 'var(--semi-color-fill-0)',
            }}
          >
            {expression}
          </pre>
        </Card>
      ) : null}
    </div>
  );
}
