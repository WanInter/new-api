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
  Input,
  InputNumber,
  Select,
  SideSheet,
  Space,
  Spin,
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
  RefreshCw,
  Route,
  XCircle,
} from 'lucide-react';
import { API, showError } from '../../helpers';
import { CHANNEL_OPTIONS } from '../../constants';

const { Title, Text } = Typography;
const DEFAULT_MODEL = 'sd-bak-1';
const DEFAULT_GROUP = 'creative-video';

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

const violationText = (violation, t) => {
  const options = { actual: violation.actual, expected: violation.expected };
  const messages = {
    images_below_min: t('至少需要 {{expected}} 张图片', options),
    images_above_max: t('最多支持 {{expected}} 张图片', options),
    videos_above_max: t('最多支持 {{expected}} 个视频', options),
    audios_above_max: t('最多支持 {{expected}} 个音频', options),
    duration_mismatch: t('仅支持 {{expected}} 秒时长', options),
    content_type_mismatch: t('仅支持 application/json 请求'),
    missing_capability: t('未配置能力档案'),
  };
  return messages[violation.code] || violation.code;
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
                key: t('时长'),
                value: candidate.capability?.fixed_duration
                  ? `${candidate.capability.fixed_duration}s`
                  : '—',
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
                <Text type='danger'>{candidate.configuration_error}</Text>
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

const ChannelRouting = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [model, setModel] = useState(DEFAULT_MODEL);
  const [group, setGroup] = useState(DEFAULT_GROUP);
  const [groups, setGroups] = useState([]);
  const [rules, setRules] = useState(null);
  const [rulesLoading, setRulesLoading] = useState(false);
  const [selectedCandidate, setSelectedCandidate] = useState(null);
  const [simulationResult, setSimulationResult] = useState(null);
  const [simulationLoading, setSimulationLoading] = useState(false);
  const [simulation, setSimulation] = useState({
    images: 4,
    videos: 0,
    audios: 0,
    duration: 15,
    retry: 0,
  });

  const loadGroups = useCallback(async () => {
    try {
      const response = await API.get('/api/group/');
      if (response.data.success) setGroups(response.data.data || []);
    } catch (error) {
      showError(error.message);
    }
  }, []);

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
    const timer = window.setTimeout(loadRules, 250);
    return () => window.clearTimeout(timer);
  }, [loadRules]);

  const runSimulation = async () => {
    setSimulationLoading(true);
    try {
      const response = await API.post('/api/channel/routing_rules/simulate', {
        model,
        group,
        ...simulation,
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

  const columns = useMemo(
    () => [
      {
        title: t('渠道'),
        dataIndex: 'channel_name',
        render: (_, record) => (
          <button
            className='text-left'
            onClick={() => setSelectedCandidate(record)}
          >
            <div className='font-medium'>
              {record.channel_name || `#${record.channel_id}`}
            </div>
            <Text type='tertiary' size='small'>
              #{record.channel_id} ·{' '}
              {channelTypeNames[record.channel_type] || record.channel_type}
            </Text>
          </button>
        ),
      },
      {
        title: t('上游模型'),
        dataIndex: 'mapping',
        render: (mapping) => <code>{mapping?.model || '—'}</code>,
      },
      {
        title: t('图片'),
        render: (_, record) => formatRange(record.capability?.images),
      },
      {
        title: t('视频'),
        render: (_, record) => formatRange(record.capability?.videos),
      },
      {
        title: t('音频'),
        render: (_, record) => formatRange(record.capability?.audios),
      },
      {
        title: t('时长'),
        render: (_, record) =>
          record.capability?.fixed_duration
            ? `${record.capability.fixed_duration}s`
            : '—',
      },
      { title: t('优先级'), dataIndex: 'priority' },
      { title: t('权重'), dataIndex: 'weight' },
      {
        title: t('状态'),
        render: (_, record) => <CandidateStatus candidate={record} t={t} />,
      },
      {
        title: '',
        width: 52,
        render: (_, record) => (
          <Button
            theme='borderless'
            icon={<Eye size={16} />}
            aria-label={t('查看详情')}
            onClick={() => setSelectedCandidate(record)}
          />
        ),
      },
    ],
    [t],
  );

  const renderCandidateTable = (candidates, loading) => (
    <Spin spinning={loading}>
      <Table
        columns={columns}
        dataSource={candidates || []}
        rowKey={(record) => `${record.group}-${record.channel_id}`}
        pagination={false}
        scroll={{ x: 1100 }}
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
          {rules?.strict && <Tag color='blue'>{t('严格分流')}</Tag>}
        </div>
        <Button
          icon={<ArrowLeft size={16} />}
          onClick={() => navigate('/console/channel')}
        >
          {t('返回渠道管理')}
        </Button>
      </div>

      <div className='flex flex-wrap items-end gap-3 border-y border-semi-color-border py-4'>
        <div className='min-w-[220px] flex-1'>
          <Text type='tertiary'>{t('公开模型')}</Text>
          <Input className='mt-1' value={model} onChange={setModel} />
        </div>
        <div className='min-w-[220px] flex-1'>
          <Text type='tertiary'>{t('分组')}</Text>
          <Select
            className='mt-1 w-full'
            value={group}
            filter
            onChange={setGroup}
            optionList={Array.from(new Set([group, ...groups])).map((item) => ({
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
          <div className='grid grid-cols-2 gap-3 border-y border-semi-color-border py-4 md:grid-cols-6'>
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
    </div>
  );
};

export default ChannelRouting;
