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
  Checkbox,
  Input,
  Modal,
  Space,
  Spin,
  Switch,
  Table,
  Tabs,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconAlertTriangle, IconRefresh } from '@douyinfe/semi-icons';
import {
  API,
  showError,
  showSuccess,
  timestamp2string,
} from '../../../../helpers';
import { useSecureVerification } from '../../../../hooks/common/useSecureVerification';
import SecureVerificationModal from '../../../common/modals/SecureVerificationModal';

const protocolOptions = [
  {
    value: 'openai.chat_completions',
    label: 'OpenAI Chat Completions',
    path: '/v1/chat/completions',
  },
  {
    value: 'anthropic.messages',
    label: 'Anthropic Messages',
    path: '/v1/messages',
  },
  {
    value: 'openai.responses',
    label: 'OpenAI Responses',
    path: '/v1/responses',
  },
];

const protocolLabel = (protocol, t) => {
  const option = protocolOptions.find((item) => item.value === protocol);
  return option ? t(option.label) : protocol;
};

const captureOutcomeLabel = (capture, t) => {
  switch (capture.skipped_reason) {
    case 'streaming_not_supported':
      return t('不支持流式响应');
    case 'request_too_large':
      return t('请求过大');
    case 'response_too_large':
      return t('响应过大');
    case 'unsupported_request_content_type':
      return t('不支持的请求内容类型');
    case 'unsupported_response_content_type':
      return t('不支持的响应内容类型');
    default:
      return capture.outcome === 'success'
        ? t('成功')
        : capture.outcome === 'error'
          ? t('失败')
          : capture.outcome;
  }
};

const RelayCaptureModal = ({ visible, channel, onCancel }) => {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState('policy');
  const [loadingPolicy, setLoadingPolicy] = useState(false);
  const [loadingCaptures, setLoadingCaptures] = useState(false);
  const [saving, setSaving] = useState(false);
  const [loadingContent, setLoadingContent] = useState(false);
  const [enabled, setEnabled] = useState(false);
  const [protocols, setProtocols] = useState([]);
  const [captures, setCaptures] = useState([]);
  const [captureTotal, setCaptureTotal] = useState(0);
  const [content, setContent] = useState('');
  const [contentTitle, setContentTitle] = useState('');

  const applySecureResult = useCallback(
    (result) => {
      if (!result) return;
      if (result.kind === 'policy') {
        setEnabled(result.policy.enabled === true);
        setProtocols(result.policy.protocols || []);
        showSuccess(t('采集策略已更新'));
      }
      if (result.kind === 'payload') {
        setContent(result.body);
        setContentTitle(result.title);
      }
    },
    [t],
  );

  const {
    isModalVisible,
    verificationMethods,
    verificationState,
    withVerification,
    executeVerification,
    cancelVerification,
    setVerificationCode,
    switchVerificationMethod,
  } = useSecureVerification({ onSuccess: applySecureResult });

  const loadPolicy = useCallback(async () => {
    if (!channel?.id) return;
    setLoadingPolicy(true);
    try {
      const response = await API.get(
        `/api/channel/${channel.id}/relay_capture`,
      );
      if (!response.data?.success) {
        throw new Error(response.data?.message || t('更新采集策略失败'));
      }
      const policy = response.data.data || { enabled: false, protocols: [] };
      setEnabled(policy.enabled === true);
      setProtocols(policy.protocols || []);
    } catch (error) {
      showError(error.message || t('更新采集策略失败'));
    } finally {
      setLoadingPolicy(false);
    }
  }, [channel?.id, t]);

  const loadCaptures = useCallback(async () => {
    if (!channel?.id) return;
    setLoadingCaptures(true);
    try {
      const response = await API.get('/api/relay-captures', {
        params: { channel_id: channel.id, page_size: 20 },
      });
      if (!response.data?.success) {
        throw new Error(response.data?.message || t('加载采集请求失败'));
      }
      setCaptures(response.data.data?.items || []);
      setCaptureTotal(response.data.data?.total || 0);
    } catch (error) {
      showError(error.message || t('加载采集请求失败'));
    } finally {
      setLoadingCaptures(false);
    }
  }, [channel?.id, t]);

  useEffect(() => {
    if (!visible) {
      setContent('');
      setContentTitle('');
      return;
    }
    setActiveTab('policy');
    loadPolicy();
    loadCaptures();
  }, [visible, loadCaptures, loadPolicy]);

  const handleProtocolChange = (protocol, checked) => {
    setProtocols((current) => {
      if (checked) return Array.from(new Set([...current, protocol]));
      return current.filter((item) => item !== protocol);
    });
  };

  const updatePolicy = async () => {
    const response = await API.put(
      `/api/channel/${channel.id}/relay_capture`,
      { enabled, protocols },
      { skipErrorHandler: true },
    );
    if (!response.data?.success) {
      throw new Error(response.data?.message || t('更新采集策略失败'));
    }
    return { kind: 'policy', policy: response.data.data };
  };

  const handleSave = async () => {
    if (enabled && protocols.length === 0) {
      showError(t('请至少选择一个采集协议'));
      return;
    }
    setSaving(true);
    try {
      const result = await withVerification(updatePolicy, {
        preferredMethod: 'passkey',
        title: t('验证后更新中继采集策略'),
        description: t('请使用 Passkey 或双重验证确认对客户端采集报文的修改。'),
      });
      applySecureResult(result);
    } catch (error) {
      showError(error.message || t('更新采集策略失败'));
    } finally {
      setSaving(false);
    }
  };

  const loadCapturePart = async (capture, part) => {
    const response = await API.get(
      `/api/relay-captures/${capture.id}/${part}`,
      { responseType: 'text', skipErrorHandler: true, disableDuplicate: true },
    );
    return {
      kind: 'payload',
      body: response.data,
      title: `${part === 'request' ? t('请求') : t('响应')} · ${capture.id}`,
    };
  };

  const handleViewPart = async (capture, part) => {
    setLoadingContent(true);
    try {
      const result = await withVerification(
        () => loadCapturePart(capture, part),
        {
          preferredMethod: 'passkey',
          title: t('验证后查看采集报文'),
          description: t('请使用 Passkey 或双重验证查看此客户端采集报文。'),
        },
      );
      applySecureResult(result);
    } catch (error) {
      showError(error.message || t('加载采集正文失败'));
    } finally {
      setLoadingContent(false);
    }
  };

  const captureColumns = useMemo(
    () => [
      {
        title: t('采集时间'),
        dataIndex: 'created_at',
        width: 176,
        render: (value, record) => (
          <div>
            <div>{timestamp2string(value)}</div>
            <Typography.Text type='tertiary' size='small'>
              {record.model || '-'}
            </Typography.Text>
          </div>
        ),
      },
      {
        title: t('协议'),
        dataIndex: 'protocol',
        width: 190,
        render: (value, record) => (
          <div>
            <div>{protocolLabel(value, t)}</div>
            <Typography.Text type='tertiary' size='small'>
              {record.path}
            </Typography.Text>
          </div>
        ),
      },
      {
        title: t('状态'),
        dataIndex: 'status_code',
        width: 76,
        render: (value) => value || '-',
      },
      {
        title: t('结果'),
        dataIndex: 'outcome',
        width: 176,
        render: (_, record) => (
          <Tag color={record.outcome === 'success' ? 'green' : 'red'}>
            {captureOutcomeLabel(record, t)}
          </Tag>
        ),
      },
      {
        title: t('操作'),
        width: 188,
        fixed: 'right',
        render: (_, record) => (
          <Space>
            <Button
              size='small'
              theme='outline'
              disabled={!record.request?.stored || loadingContent}
              onClick={() => handleViewPart(record, 'request')}
            >
              {t('查看请求')}
            </Button>
            <Button
              size='small'
              theme='outline'
              disabled={!record.response?.stored || loadingContent}
              onClick={() => handleViewPart(record, 'response')}
            >
              {t('查看响应')}
            </Button>
          </Space>
        ),
      },
    ],
    [loadingContent, t],
  );

  return (
    <>
      <Modal
        visible={visible}
        title={t('中继报文采集')}
        onCancel={onCancel}
        width={1040}
        style={{ maxWidth: 'calc(100vw - 32px)' }}
        footer={
          <Space>
            <Button onClick={onCancel} disabled={saving}>
              {t('关闭')}
            </Button>
            <Button
              type='primary'
              theme='solid'
              loading={saving}
              disabled={loadingPolicy}
              onClick={handleSave}
            >
              {t('保存')}
            </Button>
          </Space>
        }
      >
        <Tabs activeKey={activeTab} onChange={setActiveTab} type='line'>
          <Tabs.TabPane tab={t('采集策略')} itemKey='policy'>
            <div className='space-y-4 py-3'>
              <Banner
                type='warning'
                icon={<IconAlertTriangle />}
                description={t('仅采集非流式 JSON 和纯文本报文。')}
              />
              <Spin spinning={loadingPolicy}>
                <div className='rounded-lg border border-[var(--semi-color-border)] p-4'>
                  <div className='flex items-start justify-between gap-4'>
                    <div>
                      <Typography.Text strong>
                        {t('启用中继采集')}
                      </Typography.Text>
                      <Typography.Paragraph
                        type='tertiary'
                        className='!mb-0 !mt-1'
                      >
                        {t('采集正文会加密静态存储，读取时需要额外验证。')}
                      </Typography.Paragraph>
                    </div>
                    <Switch
                      checked={enabled}
                      onChange={setEnabled}
                      disabled={saving}
                    />
                  </div>
                </div>
                <div className='mt-4'>
                  <Typography.Text strong>{t('采集协议')}</Typography.Text>
                  <Checkbox.Group value={protocols} className='mt-2 block'>
                    {protocolOptions.map((protocol) => (
                      <div
                        key={protocol.value}
                        className='mb-2 flex items-center justify-between gap-4 rounded-lg border border-[var(--semi-color-border)] px-3 py-2.5'
                      >
                        <Checkbox
                          value={protocol.value}
                          checked={protocols.includes(protocol.value)}
                          disabled={!enabled || saving}
                          onChange={(event) =>
                            handleProtocolChange(
                              protocol.value,
                              event.target.checked,
                            )
                          }
                        >
                          {t(protocol.label)}
                        </Checkbox>
                        <Typography.Text type='tertiary' size='small'>
                          {protocol.path}
                        </Typography.Text>
                      </div>
                    ))}
                  </Checkbox.Group>
                </div>
              </Spin>
            </div>
          </Tabs.TabPane>
          <Tabs.TabPane
            tab={`${t('已采集请求')}${captureTotal ? ` (${captureTotal})` : ''}`}
            itemKey='captures'
          >
            <div className='space-y-4 py-3'>
              <div className='flex items-center justify-between gap-3'>
                <Typography.Text type='tertiary'>
                  {t('采集正文会加密静态存储，读取时需要额外验证。')}
                </Typography.Text>
                <Button
                  size='small'
                  theme='outline'
                  icon={<IconRefresh />}
                  loading={loadingCaptures}
                  onClick={loadCaptures}
                >
                  {t('刷新')}
                </Button>
              </div>
              <Table
                columns={captureColumns}
                dataSource={captures}
                rowKey='id'
                size='small'
                pagination={false}
                loading={loadingCaptures}
                scroll={{ x: 840 }}
                empty={
                  <Typography.Text type='tertiary'>
                    {t('未找到采集请求')}
                  </Typography.Text>
                }
              />
              {contentTitle && (
                <div className='space-y-2 border-t border-[var(--semi-color-border)] pt-4'>
                  <div className='flex items-center justify-between gap-3'>
                    <Typography.Text strong>{contentTitle}</Typography.Text>
                    <Button
                      size='small'
                      theme='borderless'
                      onClick={() => {
                        setContent('');
                        setContentTitle('');
                      }}
                    >
                      {t('关闭')}
                    </Button>
                  </div>
                  <Input.TextArea
                    readOnly
                    value={content}
                    autosize={{ minRows: 12, maxRows: 24 }}
                    className='font-mono text-xs'
                  />
                </div>
              )}
            </div>
          </Tabs.TabPane>
        </Tabs>
      </Modal>
      <SecureVerificationModal
        visible={isModalVisible}
        verificationMethods={verificationMethods}
        verificationState={verificationState}
        onVerify={executeVerification}
        onCancel={cancelVerification}
        onCodeChange={setVerificationCode}
        onMethodSwitch={switchVerificationMethod}
        title={verificationState.title}
        description={verificationState.description}
      />
    </>
  );
};

export default RelayCaptureModal;
