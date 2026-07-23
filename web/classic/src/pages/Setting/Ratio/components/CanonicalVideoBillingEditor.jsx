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
import React, { useEffect, useMemo, useState } from 'react';
import { Banner, Button, Card, Input, Select, Tag } from '@douyinfe/semi-ui';
import { API } from '../../../../helpers';
import {
  buildCanonicalVideoExpr,
  extractCanonicalVideoRates,
  getCanonicalVideoBlockReason,
  getCanonicalVideoDimensions,
  isCanonicalRateDraft,
  isCanonicalVideoAvailable,
  normalizeCanonicalBillingCapability,
} from './canonicalVideoBilling';

function SchemaSummary({ capability, t }) {
  const { durationField, resolutionField } =
    getCanonicalVideoDimensions(capability);

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
        {t('Required canonical fields')}
      </div>
      <div className='flex flex-wrap gap-2 mb-3'>
        {[durationField, resolutionField].filter(Boolean).map((field) => (
          <Tag key={field.path} size='small' color='grey'>
            {field.path}
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
            <span className='text-xs text-gray-500'>-</span>
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

export default function CanonicalVideoBillingEditor({
  model,
  onCanonicalChange,
  t,
}) {
  const [capability, setCapability] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [rates, setRates] = useState({});
  const [previewDuration, setPreviewDuration] = useState('');

  const modelName = model?.name || '';
  const canonicalEnabled = Boolean(model?.billingSchema);
  const { durations, resolutions } = useMemo(
    () => getCanonicalVideoDimensions(capability),
    [capability],
  );
  const available = isCanonicalVideoAvailable(capability);
  const blockReason = getCanonicalVideoBlockReason(capability);
  const expression = useMemo(
    () => (capability ? buildCanonicalVideoExpr(capability, rates) : ''),
    [capability, rates],
  );

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
    const nextRates = model?.billingSchema
      ? extractCanonicalVideoRates(model.billingExpr, capability)
      : {};
    setRates(nextRates);
  }, [capability, model?.billingExpr, model?.billingSchema]);

  useEffect(() => {
    if (!durations.length) {
      setPreviewDuration('');
      return;
    }
    setPreviewDuration((current) =>
      durations.some((duration) => String(duration) === current)
        ? current
        : String(durations[durations.length - 1]),
    );
  }, [durations]);

  const enableCanonical = () => {
    if (!capability || !available) return;
    const nextRates = resolutions.reduce((next, resolution) => {
      next[resolution] = rates[resolution] ?? '0';
      return next;
    }, {});
    setRates(nextRates);
    onCanonicalChange({
      billingExpr: buildCanonicalVideoExpr(capability, nextRates),
      billingSchema: capability.schema_version,
    });
  };

  const disableCanonical = () => {
    onCanonicalChange({
      billingExpr: model?.billingExpr || '',
      billingSchema: '',
    });
  };

  const updateRate = (resolution, value) => {
    if (!isCanonicalRateDraft(value)) return;
    const nextRates = { ...rates, [resolution]: value };
    setRates(nextRates);
    if (canonicalEnabled && capability) {
      onCanonicalChange({
        billingExpr: buildCanonicalVideoExpr(capability, nextRates),
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
      <Banner
        type='warning'
        fullMode={false}
        closeIcon={null}
        style={{ marginBottom: 16 }}
        title={t('Canonical video billing')}
        description={`${t('Unable to load billing capability. Generic expressions remain available.')} ${error}`}
      />
    );
  }

  if (!capability) return null;

  if (!available) {
    const genericReason =
      blockReason === 'canonical_unavailable' ||
      blockReason === 'canonical_schema_incomplete'
        ? t(
            'Every routed channel must use the same canonical schema before dynamic video pricing can be enabled.',
          )
        : blockReason;
    return (
      <div style={{ marginBottom: 16 }}>
        <Banner
          type='warning'
          fullMode={false}
          closeIcon={null}
          title={t('Canonical video billing is unavailable for this model.')}
          description={genericReason}
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
          <div className='font-medium mb-1'>{t('Per-second credits')}</div>
          <div className='text-xs text-gray-500 mb-4'>
            {t('Canonical video pricing only uses normalized billing fields.')}
          </div>
          <div className='grid grid-cols-1 sm:grid-cols-2 gap-3 mb-4'>
            {resolutions.map((resolution) => (
              <div key={resolution}>
                <div className='text-sm font-medium mb-1'>{resolution}</div>
                <Input
                  value={rates[resolution] ?? ''}
                  placeholder='0'
                  onChange={(value) => updateRate(resolution, value)}
                  suffix={t('Credits / second')}
                />
              </div>
            ))}
          </div>
          <div className='flex flex-wrap items-end gap-3 border-t pt-3 mb-3'>
            <div>
              <div className='text-xs text-gray-500 mb-1'>
                {t('Preview duration')}
              </div>
              <Select
                value={previewDuration}
                style={{ width: 120 }}
                onChange={setPreviewDuration}
              >
                {durations.map((duration) => (
                  <Select.Option key={duration} value={String(duration)}>
                    {duration}s
                  </Select.Option>
                ))}
              </Select>
            </div>
            <div className='flex flex-wrap gap-x-4 gap-y-1 text-sm pb-1'>
              {resolutions.map((resolution) => (
                <span key={resolution}>
                  {t(
                    '{{resolution}}, {{duration}} seconds = {{credits}} credits',
                    {
                      resolution,
                      duration: previewSeconds || '-',
                      credits:
                        (Number(rates[resolution]) || 0) * previewSeconds,
                    },
                  )}
                </span>
              ))}
            </div>
          </div>
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
