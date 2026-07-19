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
import { useTranslation } from 'react-i18next';
import {
  Banner,
  Button,
  Input,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  Tabs,
  Tag,
  TagInput,
  Typography,
} from '@douyinfe/semi-ui';
import {
  ArrowDown,
  ArrowUp,
  FlaskConical,
  Plus,
  RefreshCw,
  Save,
  Trash2,
} from 'lucide-react';
import { API, showError, showSuccess } from '../../helpers';

const { Text } = Typography;
const TIERS = ['1k', '2k', '4k'];
const DEFAULT_MODEL = 'image2';
const DEFAULT_GROUP = 'creative-image';
const DEFAULT_SIZES = {
  '1k': ['1024x1024', '1536x1024', '1024x1536', '960x1280', '1280x960'],
  '2k': ['2048x2048', '2560x1440', '1440x2560', '1920x2560', '2560x1920'],
  '4k': ['2880x2880', '3840x2160', '2160x3840', '2160x2880', '2880x2160'],
};

const emptyConfig = () => ({
  configured: false,
  strict: true,
  default_size: '1024x1024',
  revision: 0,
  sizes: Object.entries(DEFAULT_SIZES).flatMap(([tier, values]) =>
    values.map((size, index) => ({ size, tier, sort: index + 1 })),
  ),
  rules: [],
  candidates: [],
});

const errorMessage = (error) =>
  error?.response?.data?.message || error?.message || String(error);

const candidateLabel = (candidate) =>
  `${candidate.channel_name || `#${candidate.channel_id}`} (#${candidate.channel_id})`;

const routeReasonLabel = (reason, t) => {
  const labels = {
    channel_not_found: t('渠道不存在'),
    channel_disabled: t('渠道已禁用'),
    model_unavailable: t('当前分组未启用该模型'),
    invalid_model_mapping: t('模型映射无效'),
    request_path_not_supported: t('请求路径不受支持'),
  };
  return labels[reason] || reason || t('可用');
};

const ImageRoutingPanel = ({ rootUser }) => {
  const { t } = useTranslation();
  const [model, setModel] = useState(DEFAULT_MODEL);
  const [group, setGroup] = useState(DEFAULT_GROUP);
  const [groups, setGroups] = useState([]);
  const [models, setModels] = useState([]);
  const [config, setConfig] = useState(emptyConfig);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [activeTier, setActiveTier] = useState('1k');
  const [pendingChannel, setPendingChannel] = useState({});
  const [simulationSize, setSimulationSize] = useState('1024x1024');
  const [simulation, setSimulation] = useState(null);
  const [simulating, setSimulating] = useState(false);

  const loadGroups = useCallback(async () => {
    try {
      const response = await API.get('/api/group/');
      if (response.data.success) setGroups(response.data.data || []);
    } catch (error) {
      showError(errorMessage(error));
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
    } catch (error) {
      showError(errorMessage(error));
    }
  }, [group]);

  const loadConfig = useCallback(async () => {
    if (!model.trim() || !group.trim()) return;
    setLoading(true);
    try {
      const response = await API.get('/api/channel/image_routing_rules', {
        params: { model, group },
      });
      if (!response.data.success) throw new Error(response.data.message);
      const incoming = response.data.data;
      setConfig(
        incoming.configured
          ? incoming
          : { ...emptyConfig(), candidates: incoming.candidates || [] },
      );
      setSimulation(null);
      if (incoming.default_size) setSimulationSize(incoming.default_size);
    } catch (error) {
      showError(errorMessage(error));
    } finally {
      setLoading(false);
    }
  }, [group, model]);

  useEffect(() => {
    loadGroups();
  }, [loadGroups]);

  useEffect(() => {
    loadModels();
  }, [loadModels]);

  useEffect(() => {
    const timer = window.setTimeout(loadConfig, 250);
    return () => window.clearTimeout(timer);
  }, [loadConfig]);

  const sizesByTier = useMemo(
    () =>
      Object.fromEntries(
        TIERS.map((tier) => [
          tier,
          config.sizes
            .filter((item) => item.tier === tier)
            .sort((a, b) => a.sort - b.sort)
            .map((item) => item.size),
        ]),
      ),
    [config.sizes],
  );

  const routesByTier = useMemo(
    () =>
      Object.fromEntries(
        TIERS.map((tier) => [
          tier,
          config.rules
            .filter((item) => item.tier === tier)
            .sort((a, b) => a.rank - b.rank),
        ]),
      ),
    [config.rules],
  );

  const candidatesById = useMemo(
    () =>
      Object.fromEntries(
        (config.candidates || []).map((candidate) => [
          candidate.channel_id,
          candidate,
        ]),
      ),
    [config.candidates],
  );

  const updateTierSizes = (tier, values) => {
    const otherSizes = config.sizes.filter((item) => item.tier !== tier);
    const tierSizes = (values || []).map((size, index) => ({
      size: String(size).trim(),
      tier,
      sort: index + 1,
    }));
    setConfig((current) => ({
      ...current,
      sizes: [...otherSizes, ...tierSizes],
    }));
  };

  const replaceTierRoutes = (tier, routes) => {
    setConfig((current) => ({
      ...current,
      rules: [
        ...current.rules.filter((item) => item.tier !== tier),
        ...routes.map((item, index) => ({
          tier,
          channel_id: item.channel_id,
          rank: index + 1,
        })),
      ],
    }));
  };

  const addChannel = (tier) => {
    const channelId = Number(pendingChannel[tier]);
    if (
      !channelId ||
      routesByTier[tier].some((rule) => rule.channel_id === channelId)
    )
      return;
    replaceTierRoutes(tier, [
      ...routesByTier[tier],
      { tier, channel_id: channelId },
    ]);
    setPendingChannel((current) => ({ ...current, [tier]: undefined }));
  };

  const moveChannel = (tier, index, direction) => {
    const target = index + direction;
    if (target < 0 || target >= routesByTier[tier].length) return;
    const next = [...routesByTier[tier]];
    [next[index], next[target]] = [next[target], next[index]];
    replaceTierRoutes(tier, next);
  };

  const saveConfig = async () => {
    if (!rootUser) return;
    setSaving(true);
    try {
      const sizes = TIERS.flatMap((tier) =>
        sizesByTier[tier].map((size, index) => ({
          size,
          tier,
          sort: index + 1,
        })),
      );
      const rules = TIERS.flatMap((tier) =>
        routesByTier[tier].map((rule, index) => ({
          tier,
          channel_id: rule.channel_id,
          rank: index + 1,
        })),
      );
      const response = await API.put(
        '/api/channel/image_routing_rules/config',
        {
          public_model: model.trim(),
          strict: config.strict,
          default_size: config.default_size,
          revision: config.revision || 0,
          sizes,
          rules,
        },
      );
      if (!response.data.success) throw new Error(response.data.message);
      showSuccess(t('图片路由配置已保存'));
      await loadConfig();
    } catch (error) {
      showError(errorMessage(error));
    } finally {
      setSaving(false);
    }
  };

  const runSimulation = async () => {
    setSimulating(true);
    try {
      const response = await API.post(
        '/api/channel/image_routing_rules/simulate',
        { model: model.trim(), group, size: simulationSize },
      );
      if (!response.data.success) throw new Error(response.data.message);
      setSimulation(response.data.data);
    } catch (error) {
      showError(errorMessage(error));
    } finally {
      setSimulating(false);
    }
  };

  const routeColumns = (tier) => [
    {
      title: t('顺序'),
      dataIndex: 'rank',
      width: 70,
      render: (_, record, index) => index + 1,
    },
    {
      title: t('渠道'),
      render: (_, record) => {
        const candidate = candidatesById[record.channel_id];
        return (
          <div>
            <Text strong>
              {candidate?.channel_name || `#${record.channel_id}`}
            </Text>
            <div>
              <Text type='tertiary' size='small'>
                #{record.channel_id}
              </Text>
            </div>
          </div>
        );
      },
    },
    {
      title: t('上游模型'),
      render: (_, record) => (
        <code>{candidatesById[record.channel_id]?.mapping?.model || '—'}</code>
      ),
    },
    {
      title: '',
      width: 132,
      render: (_, record, index) => (
        <Space spacing={2}>
          <Button
            theme='borderless'
            icon={<ArrowUp size={16} />}
            aria-label={t('上移')}
            disabled={!rootUser || index === 0}
            onClick={() => moveChannel(tier, index, -1)}
          />
          <Button
            theme='borderless'
            icon={<ArrowDown size={16} />}
            aria-label={t('下移')}
            disabled={!rootUser || index === routesByTier[tier].length - 1}
            onClick={() => moveChannel(tier, index, 1)}
          />
          <Button
            theme='borderless'
            type='danger'
            icon={<Trash2 size={16} />}
            aria-label={t('移除')}
            disabled={!rootUser}
            onClick={() =>
              replaceTierRoutes(
                tier,
                routesByTier[tier].filter(
                  (item) => item.channel_id !== record.channel_id,
                ),
              )
            }
          />
        </Space>
      ),
    },
  ];

  const allSizes = TIERS.flatMap((tier) => sizesByTier[tier]);

  return (
    <Spin spinning={loading}>
      <div className='flex flex-col gap-4'>
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
                setConfig(emptyConfig());
                setSimulation(null);
                setPendingChannel({});
              }}
              optionList={Array.from(new Set([group, ...groups])).map(
                (item) => ({
                  value: item,
                  label: item,
                }),
              )}
            />
          </div>
          <Button icon={<RefreshCw size={16} />} onClick={loadConfig}>
            {t('刷新')}
          </Button>
        </div>

        {!config.configured && (
          <Banner
            type='warning'
            description={t(
              '尚未保存图片路由配置，当前请求仍使用渠道静态优先级。',
            )}
          />
        )}

        <div className='flex flex-wrap items-center justify-between gap-3 border-b border-semi-color-border pb-4'>
          <Space wrap>
            <Text strong>{t('严格路由')}</Text>
            <Switch
              checked={config.strict}
              disabled={!rootUser}
              onChange={(strict) =>
                setConfig((current) => ({ ...current, strict }))
              }
            />
            <Text type='tertiary'>{t('默认尺寸')}</Text>
            <Select
              value={config.default_size}
              disabled={!rootUser}
              onChange={(defaultSize) =>
                setConfig((current) => ({
                  ...current,
                  default_size: defaultSize,
                }))
              }
              optionList={allSizes.map((size) => ({
                value: size,
                label: size,
              }))}
            />
          </Space>
          <Button
            type='primary'
            icon={<Save size={16} />}
            loading={saving}
            disabled={!rootUser}
            onClick={saveConfig}
          >
            {t('保存图片路由')}
          </Button>
        </div>

        <Tabs activeKey={activeTier} onChange={setActiveTier} type='line'>
          {TIERS.map((tier) => (
            <Tabs.TabPane tab={tier.toUpperCase()} itemKey={tier} key={tier}>
              <div className='flex flex-col gap-4 py-2'>
                <div>
                  <Text type='tertiary'>{t('该档分辨率')}</Text>
                  <TagInput
                    className='mt-1 w-full'
                    value={sizesByTier[tier]}
                    disabled={!rootUser}
                    separator={[',', '，', ' ']}
                    onChange={(values) => updateTierSizes(tier, values)}
                    placeholder={t('输入 WIDTHxHEIGHT 后回车')}
                  />
                </div>
                <div className='flex flex-wrap items-end gap-2'>
                  <div className='min-w-[260px] flex-1'>
                    <Text type='tertiary'>{t('添加候选渠道')}</Text>
                    <Select
                      className='mt-1 w-full'
                      filter
                      value={pendingChannel[tier]}
                      disabled={!rootUser}
                      onChange={(value) =>
                        setPendingChannel((current) => ({
                          ...current,
                          [tier]: value,
                        }))
                      }
                      optionList={(config.candidates || [])
                        .filter(
                          (candidate) =>
                            !routesByTier[tier].some(
                              (rule) =>
                                rule.channel_id === candidate.channel_id,
                            ),
                        )
                        .map((candidate) => ({
                          value: candidate.channel_id,
                          label: candidateLabel(candidate),
                        }))}
                    />
                  </div>
                  <Button
                    icon={<Plus size={16} />}
                    disabled={!rootUser || !pendingChannel[tier]}
                    onClick={() => addChannel(tier)}
                  >
                    {t('添加')}
                  </Button>
                </div>
                <Table
                  columns={routeColumns(tier)}
                  dataSource={routesByTier[tier]}
                  rowKey={(record) => `${tier}-${record.channel_id}`}
                  pagination={false}
                  empty={t('该档尚未配置渠道')}
                />
              </div>
            </Tabs.TabPane>
          ))}
        </Tabs>

        <div className='border-y border-semi-color-border py-4'>
          <div className='flex flex-wrap items-end gap-2'>
            <div className='min-w-[260px] flex-1'>
              <Text type='tertiary'>{t('模拟请求尺寸')}</Text>
              <Input
                className='mt-1'
                value={simulationSize}
                onChange={setSimulationSize}
              />
            </div>
            <Button
              type='primary'
              icon={<FlaskConical size={16} />}
              loading={simulating}
              onClick={runSimulation}
            >
              {t('运行模拟')}
            </Button>
          </div>
          {simulation && (
            <div className='mt-4 flex flex-col gap-3'>
              <Space wrap>
                <Tag color={simulation.resolved_tier ? 'blue' : 'grey'}>
                  {simulation.resolved_tier?.toUpperCase() || t('未解析档位')}
                </Tag>
                <Text>
                  {t('规范尺寸')}: {simulation.normalized_size || '—'}
                </Text>
                {simulation.used_default_size && <Tag>{t('使用默认尺寸')}</Tag>}
                {simulation.fallback && (
                  <Tag color='orange'>{t('回退静态优先级')}</Tag>
                )}
              </Space>
              {simulation.reason && (
                <Banner type='warning' description={simulation.reason} />
              )}
              <Table
                dataSource={simulation.route || []}
                rowKey={(record) => `${record.tier}-${record.channel_id}`}
                pagination={false}
                empty={t('没有匹配的图片路由')}
                columns={[
                  { title: t('顺序'), dataIndex: 'rank', width: 70 },
                  {
                    title: t('渠道'),
                    render: (_, record) =>
                      `${record.channel_name || `#${record.channel_id}`} (#${record.channel_id})`,
                  },
                  {
                    title: t('上游模型'),
                    render: (_, record) => (
                      <code>{record.mapping?.model || '—'}</code>
                    ),
                  },
                  {
                    title: t('状态'),
                    render: (_, record) => (
                      <Tag color={record.eligible ? 'green' : 'red'}>
                        {record.selected
                          ? t('首选')
                          : routeReasonLabel(record.exclusion_reason, t)}
                      </Tag>
                    ),
                  },
                ]}
              />
            </div>
          )}
        </div>
      </div>
    </Spin>
  );
};

export default ImageRoutingPanel;
