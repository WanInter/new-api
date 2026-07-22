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

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Banner,
  Button,
  Descriptions,
  Divider,
  Input,
  InputNumber,
  Modal,
  Radio,
  RadioGroup,
  Select,
  SideSheet,
  Space,
  Spin,
  Switch,
  Table,
  Tabs,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  ArrowLeft,
  CheckCircle2,
  Eye,
  FlaskConical,
  Pencil,
  RefreshCw,
  Route,
  Save,
  Trash2,
  XCircle,
} from 'lucide-react';
import { API, isRoot, showError, showSuccess } from '../../helpers';
import { CHANNEL_OPTIONS } from '../../constants';
import ImageRoutingPanel from './ImageRoutingPanel';

const { Title, Text } = Typography;
const EMPTY_CAPABILITY_FORM = {
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
  aspect_ratios: [],
  resolutions: [],
  sizes: [],
  require_json: 'inherit',
  require_text: 'inherit',
  content_precedence: 'inherit',
};

const MAX_VIDEO_OUTPUT_DIMENSION = 32768;

const parseVideoOutputDimension = (value) => {
  const trimmed = String(value).trim();
  if (!/^\d+$/.test(trimmed)) return undefined;
  const parsed = Number(trimmed);
  return parsed > 0 && parsed <= MAX_VIDEO_OUTPUT_DIMENSION
    ? parsed
    : undefined;
};

const greatestCommonDivisor = (left, right) => {
  while (right !== 0) {
    [left, right] = [right, left % right];
  }
  return left;
};

const normalizeVideoOutputListValue = (field, value) => {
  const trimmed = String(value).trim();
  if (field === 'aspect_ratios') {
    const normalized = trimmed.toLowerCase();
    if (normalized === 'adaptive') return normalized;
    const parts = normalized.split(':');
    if (parts.length !== 2) return undefined;
    const width = parseVideoOutputDimension(parts[0]);
    const height = parseVideoOutputDimension(parts[1]);
    if (width === undefined || height === undefined) return undefined;
    const divisor = greatestCommonDivisor(width, height);
    return `${width / divisor}:${height / divisor}`;
  }
  if (field === 'resolutions') {
    const normalized = trimmed.toLowerCase();
    if (normalized === '4k' || normalized === '2160p') return '4k';
    if (!normalized.endsWith('p')) return undefined;
    const height = parseVideoOutputDimension(normalized.slice(0, -1));
    return height === undefined ? undefined : `${height}p`;
  }
  const parts = trimmed.split('x');
  if (parts.length !== 2) return undefined;
  const width = parseVideoOutputDimension(parts[0]);
  const height = parseVideoOutputDimension(parts[1]);
  if (width === undefined || height === undefined) return undefined;
  return `${width}x${height}`;
};

const normalizeVideoOutputList = (field, values) =>
  values.map(
    (value) => normalizeVideoOutputListValue(field, value) || value.trim(),
  );

const normalizeVideoSimulationOutput = ({
  aspect_ratio = '',
  size = '',
  resolution = '',
}) => {
  const normalizedAspectRatio = aspect_ratio.trim()
    ? normalizeVideoOutputListValue('aspect_ratios', aspect_ratio)
    : '';
  if (aspect_ratio.trim() && !normalizedAspectRatio) {
    return { error: 'invalid_aspect_ratio' };
  }

  const normalizedSize = size.trim()
    ? normalizeVideoOutputListValue('sizes', size)
    : '';
  if (size.trim() && !normalizedSize) {
    return { error: 'invalid_size' };
  }

  const normalizedResolution = resolution.trim()
    ? normalizeVideoOutputListValue('resolutions', resolution)
    : '';
  if (resolution.trim() && !normalizedResolution) {
    return { error: 'invalid_resolution' };
  }

  if (normalizedAspectRatio && normalizedSize) {
    const [width, height] = normalizedSize.split('x').map(Number);
    const divisor = greatestCommonDivisor(width, height);
    const sizeAspectRatio = `${width / divisor}:${height / divisor}`;
    if (
      normalizedAspectRatio === 'adaptive' ||
      normalizedAspectRatio !== sizeAspectRatio
    ) {
      return { error: 'size_aspect_ratio_conflict' };
    }
  }

  return {
    output: {
      aspect_ratio: normalizedAspectRatio,
      size: normalizedSize,
      resolution: normalizedResolution,
    },
  };
};

const validateVideoOutputList = (field, values) => {
  const seen = new Set();
  for (const value of values) {
    const normalized = normalizeVideoOutputListValue(field, value);
    if (!normalized || seen.has(normalized)) return false;
    seen.add(normalized);
  }
  return true;
};

const channelTypeNames = Object.fromEntries(
  CHANNEL_OPTIONS.map((option) => [option.value, option.label]),
);

const formatRange = (range) => {
  if (!range || (range.min === undefined && range.max === undefined))
    return '—';
  if (range.min !== undefined && range.max !== undefined) {
    return range.min === range.max
      ? `${range.min}`
      : `${range.min}–${range.max}`;
  }
  if (range.min !== undefined) return `≥ ${range.min}`;
  return `≤ ${range.max}`;
};

const formatDurationCapability = (capability) => {
  if (capability?.fixed_duration !== undefined) {
    return `${capability.fixed_duration}s`;
  }
  const range = formatRange(capability?.duration);
  return range === '—' ? range : `${range}s`;
};

const formatResolutionCapability = (capability) =>
  capability?.resolutions?.length ? capability.resolutions.join(', ') : '—';

const formatAspectRatioCapability = (capability) =>
  capability?.aspect_ratios?.length
    ? capability.aspect_ratios.join(', ')
    : '—';

const formatSizeCapability = (capability) =>
  capability?.sizes?.length ? capability.sizes.join(', ') : '—';

const numberToDraft = (value) =>
  value === undefined || value === null ? '' : String(value);

const booleanToDraft = (value) => {
  if (value === undefined || value === null) return 'inherit';
  return value ? 'true' : 'false';
};

const capabilityToForm = (capability) => ({
  images_min: numberToDraft(capability?.images?.min),
  images_max: numberToDraft(capability?.images?.max),
  videos_min: numberToDraft(capability?.videos?.min),
  videos_max: numberToDraft(capability?.videos?.max),
  audios_min: numberToDraft(capability?.audios?.min),
  audios_max: numberToDraft(capability?.audios?.max),
  video_audio_total_min: numberToDraft(capability?.video_audio_total?.min),
  video_audio_total_max: numberToDraft(capability?.video_audio_total?.max),
  duration_min: numberToDraft(capability?.duration?.min),
  duration_max: numberToDraft(capability?.duration?.max),
  fixed_duration: numberToDraft(capability?.fixed_duration),
  aspect_ratios: capability?.aspect_ratios || [],
  resolutions: capability?.resolutions || [],
  sizes: capability?.sizes || [],
  require_json: booleanToDraft(capability?.require_json),
  require_text: booleanToDraft(capability?.require_text),
  content_precedence: booleanToDraft(capability?.content_precedence),
});

const draftToNumber = (value) => (value === '' ? undefined : Number(value));

const draftToBoolean = (value) => {
  if (value === 'inherit') return undefined;
  return value === 'true';
};

const rangeFromDraft = (minDraft, maxDraft) => {
  const min = draftToNumber(minDraft);
  const max = draftToNumber(maxDraft);
  if (min === undefined && max === undefined) return undefined;
  const range = {};
  if (min !== undefined) range.min = min;
  if (max !== undefined) range.max = max;
  return range;
};

const formToCapability = (form) => ({
  images: rangeFromDraft(form.images_min, form.images_max),
  videos: rangeFromDraft(form.videos_min, form.videos_max),
  audios: rangeFromDraft(form.audios_min, form.audios_max),
  video_audio_total: rangeFromDraft(
    form.video_audio_total_min,
    form.video_audio_total_max,
  ),
  duration: rangeFromDraft(form.duration_min, form.duration_max),
  fixed_duration: draftToNumber(form.fixed_duration),
  aspect_ratios:
    form.aspect_ratios.length > 0 ? form.aspect_ratios : undefined,
  resolutions: form.resolutions.length > 0 ? form.resolutions : undefined,
  sizes: form.sizes.length > 0 ? form.sizes : undefined,
  require_json: draftToBoolean(form.require_json),
  require_text: draftToBoolean(form.require_text),
  content_precedence: draftToBoolean(form.content_precedence),
});

const validateCapabilityForm = (form, t) => {
  const nonNegativeFields = [
    'images_min',
    'images_max',
    'videos_min',
    'videos_max',
    'audios_min',
    'audios_max',
    'video_audio_total_min',
    'video_audio_total_max',
  ];
  const positiveFields = ['duration_min', 'duration_max', 'fixed_duration'];
  if (
    nonNegativeFields.some(
      (field) => form[field] !== '' && !/^\d+$/.test(form[field]),
    )
  ) {
    return t('请输入非负整数');
  }
  if (
    positiveFields.some(
      (field) => form[field] !== '' && !/^[1-9]\d*$/.test(form[field]),
    )
  ) {
    return t('请输入正整数');
  }
  const ranges = [
    ['images_min', 'images_max'],
    ['videos_min', 'videos_max'],
    ['audios_min', 'audios_max'],
    ['video_audio_total_min', 'video_audio_total_max'],
    ['duration_min', 'duration_max'],
  ];
  if (
    ranges.some(
      ([min, max]) =>
        form[min] !== '' &&
        form[max] !== '' &&
        Number(form[min]) > Number(form[max]),
    )
  ) {
    return t('最大值必须大于或等于最小值');
  }
  if (
    form.fixed_duration !== '' &&
    (form.duration_min !== '' || form.duration_max !== '')
  ) {
    return t('固定时长不能与范围时长同时设置');
  }
  const hasOverride = Object.entries(form).some(([field, value]) =>
    field === 'aspect_ratios' || field === 'resolutions' || field === 'sizes'
      ? value.length > 0
      : value !== '' && value !== 'inherit',
  );
  if (!hasOverride) {
    return t('至少添加一项覆盖');
  }
  return null;
};

const strictSourceLabel = (source, t) => {
  if (source === 'database') return t('数据库策略');
  if (source === 'built_in') return t('内置策略');
  return t('默认策略');
};

const violationText = (violation, t) => {
  const options = { actual: violation.actual, expected: violation.expected };
  const messages = {
    images_below_min: t('至少需要 {{expected}} 张图片', options),
    images_above_max: t('最多支持 {{expected}} 张图片', options),
    videos_above_max: t('最多支持 {{expected}} 个视频', options),
    audios_above_max: t('最多支持 {{expected}} 个音频', options),
    video_audio_total_below_min: t(
      '视频与音频合计至少需要 {{expected}} 个',
      options,
    ),
    video_audio_total_above_max: t(
      '视频与音频合计最多支持 {{expected}} 个',
      options,
    ),
    duration_mismatch: t('仅支持 {{expected}} 秒时长', options),
    duration_below_min: t('时长至少为 {{expected}} 秒', options),
    duration_above_max: t('时长最多为 {{expected}} 秒', options),
    resolution_not_supported: t(
      '清晰度 {{resolution}} 不受支持，支持：{{supported_resolutions}}',
      {
        resolution: violation.resolution,
        supported_resolutions: violation.supported_resolutions?.join(', '),
      },
    ),
    aspect_ratio_not_supported: t(
      '画面比例 {{aspect_ratio}} 不受支持，支持：{{supported_aspect_ratios}}',
      {
        aspect_ratio: violation.aspect_ratio,
        supported_aspect_ratios: violation.supported_aspect_ratios?.join(', '),
      },
    ),
    size_not_supported: t(
      '像素尺寸 {{size}} 不受支持，支持：{{supported_sizes}}',
      {
        size: violation.size,
        supported_sizes: violation.supported_sizes?.join(', '),
      },
    ),
    content_type_mismatch: t('仅支持 application/json 请求'),
    missing_capability: t('未配置能力档案'),
    invalid_content: t('显式 content 包含无效内容项'),
    text_below_min: t('显式 content 必须包含非空文本项'),
  };
  return messages[violation.code] || violation.code;
};

const configurationErrorText = (error, t) => {
  if (error === 'request_path_not_supported') {
    return t('渠道不支持视频请求路径');
  }
  return error;
};

const CandidateStatus = ({ candidate, t }) => {
  if (candidate.configuration_error) {
    return <Tag color='red'>{t('配置错误')}</Tag>;
  }
  if (candidate.eligible === true) {
    return (
      <span className='inline-flex items-center gap-1 text-green-600'>
        <CheckCircle2 size={15} />
        {candidate.selected_priority ? t('命中优先级') : t('可用')}
      </span>
    );
  }
  if (candidate.eligible === false || candidate.violations?.length) {
    return (
      <span className='inline-flex items-center gap-1 text-gray-500'>
        <XCircle size={15} /> {t('已排除')}
      </span>
    );
  }
  return <Tag color='green'>{t('已配置')}</Tag>;
};

const CandidateDetails = ({ candidate, onClose, t }) => (
  <SideSheet
    placement='right'
    title={candidate?.channel_name || t('分流详情')}
    visible={Boolean(candidate)}
    width='min(560px, 100vw)'
    onCancel={onClose}
  >
    {candidate && (
      <div className='space-y-6'>
        <Descriptions
          data={[
            { key: t('渠道 ID'), value: candidate.channel_id },
            { key: t('分组'), value: candidate.group },
            {
              key: t('渠道类型'),
              value:
                channelTypeNames[candidate.channel_type] ||
                candidate.channel_type,
            },
            { key: t('优先级'), value: candidate.priority },
            { key: t('权重'), value: candidate.weight },
          ]}
        />

        <section>
          <Title heading={6}>{t('模型映射链')}</Title>
          <div className='mt-3 flex flex-wrap items-center gap-2'>
            {(candidate.mapping?.chain || []).map((modelName, index) => (
              <React.Fragment key={`${modelName}-${index}`}>
                {index > 0 && <Text type='tertiary'>→</Text>}
                <Tag>{modelName}</Tag>
              </React.Fragment>
            ))}
          </div>
        </section>

        <section>
          <Title heading={6}>{t('生效能力')}</Title>
          <Descriptions
            className='mt-3'
            data={[
              {
                key: t('图片'),
                value: formatRange(candidate.capability?.images),
              },
              {
                key: t('视频'),
                value: formatRange(candidate.capability?.videos),
              },
              {
                key: t('音频'),
                value: formatRange(candidate.capability?.audios),
              },
              {
                key: t('视频与音频合计'),
                value: formatRange(candidate.capability?.video_audio_total),
              },
              {
                key: t('时长'),
                value: formatDurationCapability(candidate.capability),
              },
              {
                key: t('画面比例'),
                value: formatAspectRatioCapability(candidate.capability),
              },
              {
                key: t('像素尺寸'),
                value: formatSizeCapability(candidate.capability),
              },
              {
                key: t('清晰度'),
                value: candidate.capability?.resolutions?.length
                  ? candidate.capability.resolutions.join(', ')
                  : t('不限'),
              },
              {
                key: 'Content-Type',
                value: candidate.capability?.require_json
                  ? 'application/json'
                  : t('不限'),
              },
            ]}
          />
        </section>

        <section>
          <Title heading={6}>{t('配置来源')}</Title>
          <Space wrap className='mt-3'>
            {(candidate.sources || []).map((source) => (
              <Tag key={source}>{source}</Tag>
            ))}
            {!candidate.sources?.length && <Text type='tertiary'>—</Text>}
          </Space>
        </section>

        {(candidate.configuration_error ||
          candidate.violations?.length > 0) && (
          <section>
            <Title heading={6}>{t('排除原因')}</Title>
            <div className='mt-3 space-y-2'>
              {candidate.configuration_error && (
                <Text type='danger'>
                  {configurationErrorText(candidate.configuration_error, t)}
                </Text>
              )}
              {(candidate.violations || []).map((violation, index) => (
                <div key={`${violation.code}-${index}`}>
                  <Text type='tertiary'>{violationText(violation, t)}</Text>
                </div>
              ))}
            </div>
          </section>
        )}
      </div>
    )}
  </SideSheet>
);

const RangeOverrideFields = ({
  label,
  prefix,
  form,
  onChange,
  effective,
  t,
}) => (
  <section>
    <Text strong>{label}</Text>
    <div className='mt-2 grid grid-cols-2 gap-3'>
      <div>
        <Text type='tertiary' size='small'>
          {t('最小值')}
        </Text>
        <Input
          className='mt-1'
          type='number'
          min={0}
          value={form[`${prefix}_min`]}
          placeholder={numberToDraft(effective?.min)}
          onChange={(value) => onChange(`${prefix}_min`, value)}
        />
      </div>
      <div>
        <Text type='tertiary' size='small'>
          {t('最大值')}
        </Text>
        <Input
          className='mt-1'
          type='number'
          min={0}
          value={form[`${prefix}_max`]}
          placeholder={numberToDraft(effective?.max)}
          onChange={(value) => onChange(`${prefix}_max`, value)}
        />
      </div>
    </div>
  </section>
);

const BooleanOverrideField = ({ label, value, onChange, t }) => (
  <div>
    <Text strong>{label}</Text>
    <RadioGroup
      className='mt-2'
      direction='horizontal'
      type='button'
      value={value}
      onChange={(event) => onChange(event.target.value)}
    >
      <Radio value='inherit'>{t('继承')}</Radio>
      <Radio value='true'>{t('必填')}</Radio>
      <Radio value='false'>{t('非必填')}</Radio>
    </RadioGroup>
  </div>
);

const CapabilityRuleEditor = ({ candidate, onClose, onSaved, t }) => {
  const [form, setForm] = useState(EMPTY_CAPABILITY_FORM);
  const [outputErrors, setOutputErrors] = useState({});
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setForm(capabilityToForm(candidate?.editable_rule?.capability));
    setOutputErrors({});
  }, [candidate]);

  const updateField = (field, value) => {
    setForm((current) => ({ ...current, [field]: value ?? '' }));
  };

  const normalizeOutputField = (field, values) => {
    const valid = validateVideoOutputList(field, values);
    setOutputErrors((current) => ({
      ...current,
      [field]: valid ? '' : t('格式错误'),
    }));
    if (valid) {
      updateField(field, normalizeVideoOutputList(field, values));
    }
  };

  const saveOverride = async () => {
    if (!candidate) return;
    const outputFields = ['aspect_ratios', 'resolutions', 'sizes'];
    const invalidOutputField = outputFields.find(
      (field) => !validateVideoOutputList(field, form[field]),
    );
    if (invalidOutputField) {
      setOutputErrors((current) => ({
        ...current,
        [invalidOutputField]: t('格式错误'),
      }));
      return;
    }
    const normalizedForm = {
      ...form,
      aspect_ratios: normalizeVideoOutputList(
        'aspect_ratios',
        form.aspect_ratios,
      ),
      resolutions: normalizeVideoOutputList('resolutions', form.resolutions),
      sizes: normalizeVideoOutputList('sizes', form.sizes),
    };
    setForm(normalizedForm);
    const validationError = validateCapabilityForm(normalizedForm, t);
    if (validationError) {
      showError(validationError);
      return;
    }
    setSaving(true);
    try {
      const response = await API.put('/api/channel/routing_rules/capability', {
        channel_id: candidate.channel_id,
        upstream_model: candidate.mapping?.model,
        capability: formToCapability(normalizedForm),
        revision: candidate.editable_rule?.revision || 0,
      });
      if (!response.data.success) throw new Error(response.data.message);
      showSuccess(t('分流覆盖已保存'));
      await onSaved();
      onClose();
    } catch (error) {
      showError(
        error.response?.data?.message || error.message || t('保存分流覆盖失败'),
      );
    } finally {
      setSaving(false);
    }
  };

  const resetOverride = () => {
    if (!candidate?.editable_rule) return;
    Modal.confirm({
      title: t('重置分流覆盖？'),
      content: t('该渠道将恢复继承的分流能力。'),
      okType: 'danger',
      okText: t('重置分流覆盖'),
      cancelText: t('取消'),
      onOk: async () => {
        setSaving(true);
        try {
          const response = await API.delete(
            `/api/channel/routing_rules/capability/${candidate.editable_rule.id}`,
            { params: { revision: candidate.editable_rule.revision } },
          );
          if (!response.data.success) throw new Error(response.data.message);
          showSuccess(t('分流覆盖已重置'));
          await onSaved();
          onClose();
        } catch (error) {
          showError(
            error.response?.data?.message ||
              error.message ||
              t('重置分流覆盖失败'),
          );
        } finally {
          setSaving(false);
        }
      },
    });
  };

  return (
    <SideSheet
      placement='right'
      title={
        <Space>
          <Title heading={4}>{t('编辑分流覆盖')}</Title>
          <Tag color={candidate?.editable_rule ? 'blue' : 'grey'}>
            {candidate?.editable_rule ? t('数据库覆盖') : t('继承配置')}
          </Tag>
        </Space>
      }
      visible={Boolean(candidate)}
      width='min(640px, 100vw)'
      onCancel={() => !saving && onClose()}
      footer={
        <div className='flex items-center justify-between gap-2'>
          <div>
            {candidate?.editable_rule && (
              <Button
                type='danger'
                theme='borderless'
                icon={<Trash2 size={16} />}
                loading={saving}
                onClick={resetOverride}
              >
                {t('重置分流覆盖')}
              </Button>
            )}
          </div>
          <Space>
            <Button disabled={saving} onClick={onClose}>
              {t('取消')}
            </Button>
            <Button
              type='primary'
              icon={<Save size={16} />}
              loading={saving}
              onClick={saveOverride}
            >
              {t('保存分流覆盖')}
            </Button>
          </Space>
        </div>
      }
    >
      {candidate && (
        <div className='space-y-5'>
          <Descriptions
            data={[
              { key: t('渠道 ID'), value: candidate.channel_id },
              { key: t('上游模型'), value: candidate.mapping?.model || '—' },
            ]}
          />

          <RangeOverrideFields
            label={t('图片')}
            prefix='images'
            form={form}
            onChange={updateField}
            effective={candidate.capability?.images}
            t={t}
          />
          <RangeOverrideFields
            label={t('视频')}
            prefix='videos'
            form={form}
            onChange={updateField}
            effective={candidate.capability?.videos}
            t={t}
          />
          <RangeOverrideFields
            label={t('音频')}
            prefix='audios'
            form={form}
            onChange={updateField}
            effective={candidate.capability?.audios}
            t={t}
          />
          <RangeOverrideFields
            label={t('视频与音频合计')}
            prefix='video_audio_total'
            form={form}
            onChange={updateField}
            effective={candidate.capability?.video_audio_total}
            t={t}
          />

          <Divider margin='16px' />

          <section>
            <Text strong>{t('支持的画面比例')}</Text>
            <Input
              className='mt-2'
              value={form.aspect_ratios.join(', ')}
              placeholder='16:9, 9:16'
              onChange={(value) =>
                updateField(
                  'aspect_ratios',
                  value
                    .split(',')
                    .map((item) => item.trim())
                    .filter(Boolean),
                )
              }
              onBlur={() =>
                normalizeOutputField('aspect_ratios', form.aspect_ratios)
              }
            />
            {outputErrors.aspect_ratios && (
              <Text type='danger' size='small'>
                {outputErrors.aspect_ratios}
              </Text>
            )}
            {candidate.capability?.aspect_ratios?.length > 0 && (
              <div className='mt-2'>
                <Text type='tertiary' size='small'>
                  {t('生效')}: {candidate.capability.aspect_ratios.join(', ')}
                </Text>
              </div>
            )}
          </section>

          <section>
            <Text strong>{t('支持的像素尺寸')}</Text>
            <Input
              className='mt-2'
              value={form.sizes.join(', ')}
              placeholder='1280x720, 720x1280'
              onChange={(value) =>
                updateField(
                  'sizes',
                  value
                    .split(',')
                    .map((item) => item.trim())
                    .filter(Boolean),
                )
              }
              onBlur={() => normalizeOutputField('sizes', form.sizes)}
            />
            {outputErrors.sizes && (
              <Text type='danger' size='small'>
                {outputErrors.sizes}
              </Text>
            )}
            {candidate.capability?.sizes?.length > 0 && (
              <div className='mt-2'>
                <Text type='tertiary' size='small'>
                  {t('生效')}: {candidate.capability.sizes.join(', ')}
                </Text>
              </div>
            )}
          </section>

          <section>
            <Text strong>{t('支持的清晰度')}</Text>
            <Input
              className='mt-2'
              value={form.resolutions.join(', ')}
              placeholder='480p, 720p, 768p, 1080p, 4k'
              onChange={(value) =>
                updateField(
                  'resolutions',
                  value
                    .split(',')
                    .map((item) => item.trim())
                    .filter(Boolean),
                )
              }
              onBlur={() =>
                normalizeOutputField('resolutions', form.resolutions)
              }
            />
            {outputErrors.resolutions && (
              <Text type='danger' size='small'>
                {outputErrors.resolutions}
              </Text>
            )}
            {candidate.capability?.resolutions?.length > 0 && (
              <div className='mt-2'>
                <Text type='tertiary' size='small'>
                  {t('生效')}: {candidate.capability.resolutions.join(', ')}
                </Text>
              </div>
            )}
          </section>

          <Divider margin='16px' />

          <section>
            <Text strong>{t('时长')}</Text>
            <div className='mt-2 grid grid-cols-1 gap-3 sm:grid-cols-3'>
              {[
                [
                  'duration_min',
                  t('最小值'),
                  candidate.capability?.duration?.min,
                ],
                [
                  'duration_max',
                  t('最大值'),
                  candidate.capability?.duration?.max,
                ],
                [
                  'fixed_duration',
                  t('固定时长'),
                  candidate.capability?.fixed_duration,
                ],
              ].map(([field, label, placeholder]) => (
                <div key={field}>
                  <Text type='tertiary' size='small'>
                    {label}
                  </Text>
                  <Input
                    className='mt-1'
                    type='number'
                    min={1}
                    value={form[field]}
                    placeholder={numberToDraft(placeholder)}
                    onChange={(value) => updateField(field, value)}
                  />
                </div>
              ))}
            </div>
          </section>

          <Divider margin='16px' />

          <section className='space-y-4'>
            <Text strong>{t('请求语义')}</Text>
            <BooleanOverrideField
              label={t('JSON 请求')}
              value={form.require_json}
              onChange={(value) => updateField('require_json', value)}
              t={t}
            />
            <BooleanOverrideField
              label={t('文本内容')}
              value={form.require_text}
              onChange={(value) => updateField('require_text', value)}
              t={t}
            />
            <BooleanOverrideField
              label={t('显式内容优先级')}
              value={form.content_precedence}
              onChange={(value) => updateField('content_precedence', value)}
              t={t}
            />
          </section>
        </div>
      )}
    </SideSheet>
  );
};

const ChannelRouting = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const rootUser = isRoot();
  const [routingMode, setRoutingMode] = useState('video');
  const [model, setModel] = useState('');
  const [group, setGroup] = useState('');
  const [groups, setGroups] = useState([]);
  const [models, setModels] = useState([]);
  const [rules, setRules] = useState(null);
  const [rulesLoading, setRulesLoading] = useState(false);
  const [selectedCandidate, setSelectedCandidate] = useState(null);
  const [editingCandidate, setEditingCandidate] = useState(null);
  const [policySaving, setPolicySaving] = useState(false);
  const [channelSettingsDrafts, setChannelSettingsDrafts] = useState({});
  const [savingChannelSettings, setSavingChannelSettings] = useState(null);
  const [simulationResult, setSimulationResult] = useState(null);
  const [simulationLoading, setSimulationLoading] = useState(false);
  const [simulationOutputErrors, setSimulationOutputErrors] = useState({});
  const [simulation, setSimulation] = useState({
    images: 4,
    videos: 0,
    audios: 0,
    duration: 15,
    aspect_ratio: '',
    size: '',
    resolution: '',
    retry: 0,
  });

  const loadGroups = useCallback(async () => {
    try {
      const response = await API.get('/api/group/');
      if (!response.data.success) throw new Error(response.data.message);
      const nextGroups = Array.from(
        new Set(
          (response.data.data || [])
            .map((item) => String(item || '').trim())
            .filter(Boolean),
        ),
      );
      setGroups(nextGroups);
      setGroup((current) => current || nextGroups[0] || '');
    } catch (error) {
      showError(error.message);
    }
  }, []);

  const loadModels = useCallback(async () => {
    if (!group.trim()) return;
    try {
      const response = await API.get('/api/channel/models_enabled', {
        params: { group },
      });
      if (!response.data.success) throw new Error(response.data.message);
      const nextModels = Array.from(
        new Set(
          (response.data.data || [])
            .map((item) => String(item || '').trim())
            .filter(Boolean),
        ),
      ).sort((left, right) => left.localeCompare(right));
      setModels(nextModels);
      setModel((current) =>
        nextModels.includes(current) ? current : nextModels[0] || '',
      );
    } catch (error) {
      showError(error.message);
    }
  }, [group]);

  const loadRules = useCallback(async () => {
    if (!model.trim() || !group.trim()) return;
    setRulesLoading(true);
    try {
      const response = await API.get('/api/channel/routing_rules', {
        params: { model, group },
      });
      if (!response.data.success) throw new Error(response.data.message);
      setRules(response.data.data);
    } catch (error) {
      showError(error.message);
    } finally {
      setRulesLoading(false);
    }
  }, [group, model]);

  useEffect(() => {
    loadGroups();
  }, [loadGroups]);

  useEffect(() => {
    loadModels();
  }, [loadModels]);

  useEffect(() => {
    setSimulationResult(null);
    setSelectedCandidate(null);
    setEditingCandidate(null);
    const timer = window.setTimeout(loadRules, 250);
    return () => window.clearTimeout(timer);
  }, [loadRules]);

  const runSimulation = async () => {
    const normalizedOutput = normalizeVideoSimulationOutput(simulation);
    if ('error' in normalizedOutput) {
      const errors = {
        invalid_aspect_ratio: { aspect_ratio: t('格式错误') },
        invalid_size: { size: t('格式错误') },
        invalid_resolution: { resolution: t('格式错误') },
        size_aspect_ratio_conflict: {
          aspect_ratio: t('像素尺寸与画面比例冲突'),
          size: t('像素尺寸与画面比例冲突'),
        },
      };
      setSimulationOutputErrors(errors[normalizedOutput.error]);
      return;
    }
    setSimulationOutputErrors({});
    setSimulationLoading(true);
    try {
      const response = await API.post('/api/channel/routing_rules/simulate', {
        model,
        group,
        ...simulation,
        ...normalizedOutput.output,
        content_type: 'application/json',
      });
      if (!response.data.success) throw new Error(response.data.message);
      setSimulationResult(response.data.data);
    } catch (error) {
      showError(error.message);
    } finally {
      setSimulationLoading(false);
    }
  };

  const updateStrictPolicy = async (strict) => {
    if (!rootUser || !rules) return;
    setPolicySaving(true);
    try {
      const response = await API.put('/api/channel/routing_rules/policy', {
        public_model: model,
        strict,
        revision: rules.policy?.revision || 0,
      });
      if (!response.data.success) throw new Error(response.data.message);
      showSuccess(t('分流策略已保存'));
      await loadRules();
    } catch (error) {
      showError(
        error.response?.data?.message || error.message || t('保存分流策略失败'),
      );
    } finally {
      setPolicySaving(false);
    }
  };

  const getChannelSettingsDraft = (candidate) =>
    channelSettingsDrafts[candidate.channel_id] || {
      priority: String(candidate.priority),
      weight: String(candidate.weight),
    };

  const updateChannelSettingsDraft = (candidate, field, value) => {
    setChannelSettingsDrafts((current) => ({
      ...current,
      [candidate.channel_id]: {
        ...(current[candidate.channel_id] || {
          priority: String(candidate.priority),
          weight: String(candidate.weight),
        }),
        [field]: value,
      },
    }));
  };

  const saveChannelSettings = async (candidate) => {
    const draft = getChannelSettingsDraft(candidate);
    if (!/^-?\d+$/.test(draft.priority) || !/^\d+$/.test(draft.weight)) {
      showError(t('操作失败'));
      return;
    }
    setSavingChannelSettings(candidate.channel_id);
    try {
      const response = await API.put(
        '/api/channel/routing_rules/channel_settings',
        {
          channel_id: candidate.channel_id,
          priority: Number(draft.priority),
          weight: Number(draft.weight),
        },
      );
      if (!response.data.success) throw new Error(response.data.message);
      setChannelSettingsDrafts((current) => {
        const next = { ...current };
        delete next[candidate.channel_id];
        return next;
      });
      showSuccess(t('操作成功完成！'));
      await loadRules();
    } catch (error) {
      showError(
        error.response?.data?.message || error.message || t('操作失败'),
      );
    } finally {
      setSavingChannelSettings(null);
    }
  };

  const columns = useMemo(
    () => [
      {
        title: t('渠道'),
        dataIndex: 'channel_name',
        width: 520,
        ellipsis: true,
        render: (_, record) => {
          const channelName = record.channel_name || `#${record.channel_id}`;
          return (
            <button
              className='block w-full overflow-hidden text-left'
              title={channelName}
              onClick={() => setSelectedCandidate(record)}
            >
              <div className='truncate font-medium'>{channelName}</div>
              <Text type='tertiary' size='small'>
                #{record.channel_id} ·{' '}
                {channelTypeNames[record.channel_type] || record.channel_type}
              </Text>
              {record.editable_rule && (
                <Tag className='mt-1' color='green' size='small'>
                  {t('数据库覆盖')}
                </Tag>
              )}
            </button>
          );
        },
      },
      {
        title: t('上游模型'),
        dataIndex: 'mapping',
        width: 260,
        ellipsis: true,
        render: (mapping) => {
          const upstreamModel = mapping?.model || '—';
          return (
            <code className='block truncate' title={upstreamModel}>
              {upstreamModel}
            </code>
          );
        },
      },
      {
        title: t('图片'),
        width: 72,
        align: 'center',
        render: (_, record) => formatRange(record.capability?.images),
      },
      {
        title: t('视频'),
        width: 72,
        align: 'center',
        render: (_, record) => formatRange(record.capability?.videos),
      },
      {
        title: t('音频'),
        width: 72,
        align: 'center',
        render: (_, record) => formatRange(record.capability?.audios),
      },
      {
        title: t('视频与音频合计'),
        width: 128,
        align: 'center',
        render: (_, record) =>
          formatRange(record.capability?.video_audio_total),
      },
      {
        title: t('时长'),
        width: 84,
        align: 'center',
        render: (_, record) => formatDurationCapability(record.capability),
      },
      {
        title: t('画面比例'),
        width: 120,
        align: 'center',
        render: (_, record) => formatAspectRatioCapability(record.capability),
      },
      {
        title: t('像素尺寸'),
        width: 120,
        align: 'center',
        render: (_, record) => formatSizeCapability(record.capability),
      },
      {
        title: t('清晰度'),
        width: 120,
        align: 'center',
        render: (_, record) => formatResolutionCapability(record.capability),
      },
      {
        title: t('优先级'),
        width: 104,
        align: 'center',
        render: (_, record) => {
          const draft = getChannelSettingsDraft(record);
          return rootUser ? (
            <Input
              type='number'
              value={draft.priority}
              disabled={savingChannelSettings !== null}
              onChange={(value) =>
                updateChannelSettingsDraft(record, 'priority', value)
              }
            />
          ) : (
            record.priority
          );
        },
      },
      {
        title: t('权重'),
        width: 104,
        align: 'center',
        render: (_, record) => {
          const draft = getChannelSettingsDraft(record);
          return rootUser ? (
            <Input
              type='number'
              min={0}
              value={draft.weight}
              disabled={savingChannelSettings !== null}
              onChange={(value) =>
                updateChannelSettingsDraft(record, 'weight', value)
              }
            />
          ) : (
            record.weight
          );
        },
      },
      {
        title: t('状态'),
        width: 108,
        align: 'center',
        render: (_, record) => <CandidateStatus candidate={record} t={t} />,
      },
      {
        title: '',
        width: rootUser ? 132 : 52,
        render: (_, record) => (
          <Space spacing={2}>
            {rootUser && (
              <Button
                theme='borderless'
                icon={<Save size={16} />}
                aria-label={t('保存')}
                disabled={
                  savingChannelSettings !== null ||
                  (getChannelSettingsDraft(record).priority ===
                    String(record.priority) &&
                    getChannelSettingsDraft(record).weight ===
                      String(record.weight))
                }
                onClick={() => saveChannelSettings(record)}
              />
            )}
            <Button
              theme='borderless'
              icon={<Eye size={16} />}
              aria-label={t('查看详情')}
              onClick={() => setSelectedCandidate(record)}
            />
            {rootUser && (
              <Button
                theme='borderless'
                icon={<Pencil size={16} />}
                aria-label={t('编辑分流覆盖')}
                onClick={() => setEditingCandidate(record)}
              />
            )}
          </Space>
        ),
      },
    ],
    [channelSettingsDrafts, rootUser, savingChannelSettings, t],
  );

  const renderCandidateTable = (candidates, loading) => (
    <Spin spinning={loading}>
      <Table
        columns={columns}
        dataSource={candidates || []}
        rowKey={(record) => `${record.group}-${record.channel_id}`}
        pagination={false}
        tableLayout='fixed'
        scroll={{ x: 2016 }}
        empty={t('未找到分流候选渠道')}
      />
    </Spin>
  );

  const eligibleCount = simulationResult?.candidates?.filter(
    (candidate) => candidate.eligible,
  ).length;

  return (
    <div className='mt-[60px] px-2 pb-8'>
      <div className='flex flex-wrap items-center justify-between gap-3 py-4'>
        <div>
          <Title heading={4}>{t('分流规则')}</Title>
          {routingMode === 'video' && rules && (
            <Space className='mt-2' wrap>
              <Text strong>{t('严格分流')}</Text>
              <Tag color='blue'>
                {strictSourceLabel(rules.strict_source, t)}
              </Tag>
              <Switch
                checked={rules.strict}
                disabled={!rootUser || policySaving}
                loading={policySaving}
                onChange={updateStrictPolicy}
              />
            </Space>
          )}
        </div>
        <Button
          icon={<ArrowLeft size={16} />}
          onClick={() => navigate('/console/channel')}
        >
          {t('返回渠道管理')}
        </Button>
      </div>

      <RadioGroup
        type='button'
        value={routingMode}
        onChange={(event) => setRoutingMode(event.target.value)}
        className='mb-4'
      >
        <Radio value='video'>{t('视频路由')}</Radio>
        <Radio value='image'>{t('图片路由')}</Radio>
      </RadioGroup>

      {routingMode === 'image' ? (
        <ImageRoutingPanel rootUser={rootUser} />
      ) : (
        <>
          <div className='flex flex-wrap items-end gap-3 border-y border-semi-color-border py-4'>
            <div className='min-w-[220px] flex-1'>
              <Text type='tertiary'>{t('公开模型')}</Text>
              <Select
                className='mt-1 w-full'
                value={model}
                filter
                onChange={setModel}
                placeholder={t('搜索模型...')}
                emptyContent={t('暂无模型')}
                optionList={models.map((item) => ({
                  value: item,
                  label: item,
                }))}
              />
            </div>
            <div className='min-w-[220px] flex-1'>
              <Text type='tertiary'>{t('分组')}</Text>
              <Select
                className='mt-1 w-full'
                value={group}
                filter
                onChange={(value) => {
                  setGroup(value);
                  setModel('');
                  setRules(null);
                  setSimulationResult(null);
                  setSelectedCandidate(null);
                  setEditingCandidate(null);
                }}
                optionList={Array.from(
                  new Set(group ? [group, ...groups] : groups),
                ).map((item) => ({
                  value: item,
                  label: item,
                }))}
              />
            </div>
            <Button
              icon={<RefreshCw size={16} />}
              onClick={loadRules}
              loading={rulesLoading}
            >
              {t('刷新')}
            </Button>
          </div>

          <Tabs type='line' className='mt-4'>
            <Tabs.TabPane
              tab={
                <span className='inline-flex items-center gap-2'>
                  <Route size={16} />
                  {t('规则总览')}
                </span>
              }
              itemKey='rules'
            >
              {renderCandidateTable(rules?.candidates, rulesLoading)}
            </Tabs.TabPane>
            <Tabs.TabPane
              tab={
                <span className='inline-flex items-center gap-2'>
                  <FlaskConical size={16} />
                  {t('分流模拟')}
                </span>
              }
              itemKey='simulator'
            >
              <div className='grid grid-cols-2 gap-3 border-y border-semi-color-border py-4 md:grid-cols-7'>
                {['images', 'videos', 'audios', 'duration', 'retry'].map(
                  (field) => (
                    <div key={field}>
                      <Text type='tertiary'>
                        {t(
                          {
                            images: '图片',
                            videos: '视频',
                            audios: '音频',
                            duration: '时长',
                            retry: '重试次数',
                          }[field],
                        )}
                      </Text>
                      <InputNumber
                        className='mt-1 w-full'
                        min={field === 'duration' ? 1 : 0}
                        value={simulation[field]}
                        onChange={(value) =>
                          setSimulation((current) => ({
                            ...current,
                            [field]: Number(value),
                          }))
                        }
                      />
                    </div>
                  ),
                )}
                <div>
                  <label htmlFor='simulation-aspect-ratio'>
                    <Text type='tertiary'>{t('画面比例')}</Text>
                  </label>
                  <Input
                    id='simulation-aspect-ratio'
                    className='mt-1 w-full'
                    value={simulation.aspect_ratio}
                    placeholder='16:9'
                    validateStatus={
                      simulationOutputErrors.aspect_ratio ? 'error' : 'default'
                    }
                    onChange={(value) => {
                      setSimulationOutputErrors((current) => ({
                        ...current,
                        aspect_ratio: '',
                      }));
                      setSimulation((current) => ({
                        ...current,
                        aspect_ratio: value,
                      }));
                    }}
                    onBlur={(event) => {
                      const normalized = normalizeVideoOutputListValue(
                        'aspect_ratios',
                        event.target.value,
                      );
                      if (normalized) {
                        setSimulationOutputErrors((current) => ({
                          ...current,
                          aspect_ratio: '',
                        }));
                        setSimulation((current) => ({
                          ...current,
                          aspect_ratio: normalized,
                        }));
                      } else if (event.target.value.trim()) {
                        setSimulationOutputErrors((current) => ({
                          ...current,
                          aspect_ratio: t('格式错误'),
                        }));
                      }
                    }}
                  />
                  {simulationOutputErrors.aspect_ratio && (
                    <Text type='danger' size='small'>
                      {simulationOutputErrors.aspect_ratio}
                    </Text>
                  )}
                </div>
                <div>
                  <label htmlFor='simulation-size'>
                    <Text type='tertiary'>{t('像素尺寸')}</Text>
                  </label>
                  <Input
                    id='simulation-size'
                    className='mt-1 w-full'
                    value={simulation.size}
                    placeholder='1280x720'
                    validateStatus={
                      simulationOutputErrors.size ? 'error' : 'default'
                    }
                    onChange={(value) => {
                      setSimulationOutputErrors((current) => ({
                        ...current,
                        size: '',
                      }));
                      setSimulation((current) => ({ ...current, size: value }));
                    }}
                    onBlur={(event) => {
                      const normalized = normalizeVideoOutputListValue(
                        'sizes',
                        event.target.value,
                      );
                      if (normalized) {
                        setSimulationOutputErrors((current) => ({
                          ...current,
                          size: '',
                        }));
                        setSimulation((current) => ({
                          ...current,
                          size: normalized,
                        }));
                      } else if (event.target.value.trim()) {
                        setSimulationOutputErrors((current) => ({
                          ...current,
                          size: t('格式错误'),
                        }));
                      }
                    }}
                  />
                  {simulationOutputErrors.size && (
                    <Text type='danger' size='small'>
                      {simulationOutputErrors.size}
                    </Text>
                  )}
                </div>
                <div>
                  <label htmlFor='simulation-resolution'>
                    <Text type='tertiary'>{t('清晰度')}</Text>
                  </label>
                  <Input
                    id='simulation-resolution'
                    className='mt-1 w-full'
                    value={simulation.resolution}
                    placeholder='720p, 768p, 4k'
                    validateStatus={
                      simulationOutputErrors.resolution ? 'error' : 'default'
                    }
                    onChange={(resolution) => {
                      setSimulationOutputErrors((current) => ({
                        ...current,
                        resolution: '',
                      }));
                      setSimulation((current) => ({
                        ...current,
                        resolution: resolution || '',
                      }));
                    }}
                    onBlur={(event) => {
                      const normalized = normalizeVideoOutputListValue(
                        'resolutions',
                        event.target.value,
                      );
                      if (normalized) {
                        setSimulationOutputErrors((current) => ({
                          ...current,
                          resolution: '',
                        }));
                        setSimulation((current) => ({
                          ...current,
                          resolution: normalized,
                        }));
                      } else if (event.target.value.trim()) {
                        setSimulationOutputErrors((current) => ({
                          ...current,
                          resolution: t('格式错误'),
                        }));
                      }
                    }}
                  />
                  {simulationOutputErrors.resolution && (
                    <Text type='danger' size='small'>
                      {simulationOutputErrors.resolution}
                    </Text>
                  )}
                </div>
                <div className='flex items-end'>
                  <Button
                    type='primary'
                    block
                    icon={<FlaskConical size={16} />}
                    loading={simulationLoading}
                    onClick={runSimulation}
                  >
                    {t('运行')}
                  </Button>
                </div>
              </div>

              {simulationResult && (
                <Banner
                  className='my-3'
                  type='info'
                  description={`${t('可用渠道')}: ${eligibleCount} · ${t('目标优先级')}: ${simulationResult.target_priority ?? '—'} · ${t('重试次数')}: ${simulationResult.retry}`}
                />
              )}
              {renderCandidateTable(
                simulationResult?.candidates,
                simulationLoading,
              )}
            </Tabs.TabPane>
          </Tabs>

          <CandidateDetails
            candidate={selectedCandidate}
            onClose={() => setSelectedCandidate(null)}
            t={t}
          />
          <CapabilityRuleEditor
            candidate={editingCandidate}
            onClose={() => setEditingCandidate(null)}
            onSaved={async () => {
              setSimulationResult(null);
              await loadRules();
            }}
            t={t}
          />
        </>
      )}
    </div>
  );
};

export default ChannelRouting;
