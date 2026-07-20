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

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useMemo, useState } from 'react';
import {
  Banner,
  Button,
  Card,
  Checkbox,
  Col,
  Input,
  InputNumber,
  Row,
  Select,
  Spin,
  Switch,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSave, IconTickCircle } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';

const { Text, Title } = Typography;

const emptyDraft = {
  enabled: false,
  domains: '',
  backend: 'tencent_cos',
  upload_endpoint: '',
  bucket: '',
  region: '',
  public_base_url: '',
  use_path_style: false,
  signed_url_expiry_hours: 168,
  prefix: 'generated/newapi/videos',
  max_mb: 512,
  timeout_seconds: 180,
  access_key_id: '',
  access_key_secret: '',
  proxy: '',
  clear_credentials: false,
  clear_proxy: false,
};

const fromSettings = (settings) => ({
  ...emptyDraft,
  enabled: settings.enabled,
  domains: settings.domains,
  backend: settings.backend,
  upload_endpoint: settings.upload_endpoint,
  bucket: settings.bucket,
  region: settings.region,
  public_base_url: settings.public_base_url,
  use_path_style: settings.use_path_style,
  signed_url_expiry_hours: settings.signed_url_expiry_hours,
  prefix: settings.prefix,
  max_mb: settings.max_mb,
  timeout_seconds: settings.timeout_seconds,
});

const sourceLabels = {
  database: '数据库',
  environment: '环境变量',
  default: '默认值',
  none: '未配置',
};

const Field = ({ label, description, children }) => (
  <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
    <Text strong>{label}</Text>
    {children}
    {description ? (
      <Text type='tertiary' size='small'>
        {description}
      </Text>
    ) : null}
  </div>
);

const TaskResultStorageSetting = () => {
  const { t } = useTranslation();
  const [settings, setSettings] = useState(null);
  const [draft, setDraft] = useState(emptyDraft);
  const [loading, setLoading] = useState(true);
  const [action, setAction] = useState('');
  const [testResult, setTestResult] = useState(null);

  const dirty = useMemo(
    () =>
      settings &&
      JSON.stringify(draft) !== JSON.stringify(fromSettings(settings)),
    [draft, settings],
  );

  const update = (key, value) => {
    setDraft((current) => ({ ...current, [key]: value }));
    setTestResult(null);
  };

  const load = async () => {
    setLoading(true);
    try {
      const response = await API.get('/api/option/task-result-rehost');
      if (!response.data.success) {
        showError(response.data.message);
        return;
      }
      setSettings(response.data.data);
      setDraft(fromSettings(response.data.data));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const payload = () => {
    const value = {
      ...draft,
      domains: draft.domains.trim(),
      upload_endpoint: draft.upload_endpoint.trim(),
      bucket: draft.bucket.trim(),
      region: draft.region.trim(),
      public_base_url: draft.public_base_url.trim(),
      prefix: draft.prefix.trim(),
    };
    if (!value.access_key_id) delete value.access_key_id;
    if (!value.access_key_secret) delete value.access_key_secret;
    if (!value.proxy) delete value.proxy;
    return value;
  };

  const validate = (testing) => {
    if (!!draft.access_key_id !== !!draft.access_key_secret) {
      showError(t('SecretId 和 SecretKey 必须同时填写'));
      return false;
    }
    if (draft.enabled && !draft.domains.trim()) {
      showError(t('启用转存时至少需要一个源域名'));
      return false;
    }
    if (
      (draft.enabled || testing) &&
      (!draft.upload_endpoint ||
        !draft.bucket ||
        !draft.region ||
        (draft.backend !== 'idrive' && !draft.public_base_url))
    ) {
      showError(t('请填写完整的对象存储连接配置'));
      return false;
    }
    if (!draft.prefix.trim() || draft.max_mb < 1 || draft.timeout_seconds < 1) {
      showError(t('对象前缀、文件大小和超时时间配置无效'));
      return false;
    }
    return true;
  };

  const runAction = async (kind) => {
    if (!validate(kind === 'test')) return;
    setAction(kind);
    setTestResult(null);
    try {
      const response =
        kind === 'test'
          ? await API.post('/api/option/task-result-rehost/test', payload(), {
              skipErrorHandler: true,
            })
          : await API.put('/api/option/task-result-rehost', payload(), {
              skipErrorHandler: true,
            });
      if (!response.data.success) {
        showError(response.data.message);
        return;
      }
      if (kind === 'test') {
        setTestResult(response.data.data);
        showSuccess(t('对象存储连接成功'));
      } else {
        setSettings(response.data.data);
        setDraft(fromSettings(response.data.data));
        showSuccess(t('任务结果存储设置已保存'));
      }
    } catch (error) {
      showError(error?.response?.data?.message || error);
    } finally {
      setAction('');
    }
  };

  const applyCosDefaults = () => {
    const region = draft.region.trim();
    const bucket = draft.bucket.trim();
    setDraft((current) => ({
      ...current,
      upload_endpoint: region
        ? `https://cos-internal.${region}.myqcloud.com`
        : current.upload_endpoint,
      public_base_url:
        region && bucket
          ? `https://${bucket}.cos.${region}.myqcloud.com`
          : current.public_base_url,
    }));
  };

  const applyIDriveDefaults = () => {
    const region = draft.region.trim();
    setDraft((current) => ({
      ...current,
      upload_endpoint: region
        ? `https://s3.${region}.idrivee2.com`
        : current.upload_endpoint,
      public_base_url: '',
      use_path_style: true,
    }));
  };

  return (
    <Card style={{ marginTop: 10 }}>
      <Spin spinning={loading}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
          <div
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              gap: 12,
              flexWrap: 'wrap',
            }}
          >
            <div>
              <Title heading={5} style={{ margin: 0 }}>
                {t('任务结果存储')}
              </Title>
              <Text type='tertiary'>
                {t('将匹配的远程任务结果转存到托管对象存储')}
              </Text>
            </div>
            {settings ? (
              <Tag color='blue'>
                {t(sourceLabels[settings.config_source] || '未配置')}
              </Tag>
            ) : null}
          </div>

          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              gap: 16,
            }}
          >
            <div>
              <Text strong>{t('启用远程结果转存')}</Text>
              <br />
              <Text type='tertiary' size='small'>
                {t('关闭转存时仍可保留配置')}
              </Text>
            </div>
            <Switch
              checked={draft.enabled}
              onChange={(value) => update('enabled', value)}
              disabled={!!action}
            />
          </div>

          <Row gutter={[16, 16]}>
            <Col xs={24} md={12}>
              <Field label={t('存储后端')}>
                <Select
                  value={draft.backend}
                  onChange={(value) => {
                    update('backend', value);
                    if (value === 'idrive') {
                      update('public_base_url', '');
                      update('use_path_style', true);
                    }
                  }}
                  style={{ width: '100%' }}
                  optionList={[
                    { value: 'tencent_cos', label: 'Tencent COS' },
                    { value: 's3', label: 'S3' },
                    { value: 'idrive', label: 'iDrive E2' },
                    { value: 'aliyun_oss', label: 'Aliyun OSS' },
                  ]}
                />
              </Field>
            </Col>
            <Col xs={24} md={12}>
              <Field
                label={t('源域名')}
                description={t('多个域名使用逗号分隔')}
              >
                <TextArea
                  value={draft.domains}
                  onChange={(value) => update('domains', value)}
                  autosize={{ minRows: 1, maxRows: 3 }}
                  placeholder='vidgen.x.ai'
                />
              </Field>
            </Col>
            <Col xs={24} md={12}>
              <Field label='Bucket'>
                <Input
                  value={draft.bucket}
                  onChange={(value) => update('bucket', value)}
                  placeholder='media-1250000000'
                />
              </Field>
            </Col>
            <Col xs={24} md={12}>
              <Field label={t('区域')}>
                <Input
                  value={draft.region}
                  onChange={(value) => update('region', value)}
                  placeholder='ap-guangzhou'
                />
              </Field>
            </Col>
            <Col xs={24} md={12}>
              <Field
                label={t('上传端点')}
                description={t('同地域腾讯云服务优先使用内网端点')}
              >
                <Input
                  value={draft.upload_endpoint}
                  onChange={(value) => update('upload_endpoint', value)}
                  placeholder='https://cos-internal.ap-guangzhou.myqcloud.com'
                />
              </Field>
            </Col>
            {draft.backend !== 'idrive' ? (
              <Col xs={24} md={12}>
                <Field
                  label={t('公开访问地址')}
                  description={t('该地址会返回给客户端')}
                >
                  <Input
                    value={draft.public_base_url}
                    onChange={(value) => update('public_base_url', value)}
                    placeholder='https://bucket.cos.ap-guangzhou.myqcloud.com'
                  />
                </Field>
              </Col>
            ) : null}
            {draft.backend === 's3' ? (
              <Col span={24}>
                <Checkbox
                  checked={draft.use_path_style}
                  onChange={(event) =>
                    update('use_path_style', event.target.checked)
                  }
                  disabled={!!action}
                >
                  {t('使用路径式 S3 寻址')}
                </Checkbox>
                <Text type='tertiary' size='small'>
                  {t(
                    '不支持虚拟主机寻址的存储服务，请使用 endpoint/bucket/object 形式请求',
                  )}
                </Text>
              </Col>
            ) : null}
            {draft.backend === 'tencent_cos' ? (
              <Col span={24}>
                <Button
                  icon={<IconRefresh />}
                  onClick={applyCosDefaults}
                  disabled={!!action}
                >
                  {t('应用推荐 COS 端点')}
                </Button>
              </Col>
            ) : null}
            {draft.backend === 'idrive' ? (
              <Col span={24}>
                <Button
                  icon={<IconRefresh />}
                  onClick={applyIDriveDefaults}
                  disabled={!!action}
                >
                  {t('应用推荐 iDrive E2 端点')}
                </Button>
              </Col>
            ) : null}
            {draft.backend === 'idrive' ? (
              <Col xs={12} md={6}>
                <Field label={t('签名链接有效期（小时）')}>
                  <InputNumber
                    value={draft.signed_url_expiry_hours}
                    min={1}
                    max={168}
                    onChange={(value) =>
                      update('signed_url_expiry_hours', Number(value))
                    }
                    style={{ width: '100%' }}
                  />
                </Field>
              </Col>
            ) : null}
            <Col xs={24} md={12}>
              <Field label={t('对象前缀')}>
                <Input
                  value={draft.prefix}
                  onChange={(value) => update('prefix', value)}
                />
              </Field>
            </Col>
            <Col xs={12} md={6}>
              <Field label={t('最大文件大小（MB）')}>
                <InputNumber
                  value={draft.max_mb}
                  min={1}
                  max={10240}
                  onChange={(value) => update('max_mb', Number(value))}
                  style={{ width: '100%' }}
                />
              </Field>
            </Col>
            <Col xs={12} md={6}>
              <Field label={t('超时时间（秒）')}>
                <InputNumber
                  value={draft.timeout_seconds}
                  min={1}
                  max={1800}
                  onChange={(value) => update('timeout_seconds', Number(value))}
                  style={{ width: '100%' }}
                />
              </Field>
            </Col>
          </Row>

          {settings ? (
            <Banner
              type='info'
              description={`${t('凭证来源')}：${t(sourceLabels[settings.credential_source] || '未配置')}；${t('代理来源')}：${t(sourceLabels[settings.proxy_source] || '未配置')}`}
            />
          ) : null}
          <Row gutter={[16, 16]}>
            <Col xs={24} md={12}>
              <Field
                label='SecretId'
                description={
                  settings?.credentials_configured
                    ? t('留空以保留当前凭证')
                    : ''
                }
              >
                <Input
                  mode='password'
                  autoComplete='new-password'
                  value={draft.access_key_id}
                  onChange={(value) => update('access_key_id', value)}
                  disabled={draft.clear_credentials}
                />
              </Field>
            </Col>
            <Col xs={24} md={12}>
              <Field
                label='SecretKey'
                description={
                  settings?.credentials_configured
                    ? t('留空以保留当前凭证')
                    : ''
                }
              >
                <Input
                  mode='password'
                  autoComplete='new-password'
                  value={draft.access_key_secret}
                  onChange={(value) => update('access_key_secret', value)}
                  disabled={draft.clear_credentials}
                />
              </Field>
            </Col>
            <Col span={24}>
              <Field
                label={t('下载代理')}
                description={
                  settings?.proxy_configured
                    ? t('留空以保留当前代理')
                    : t('仅用于下载原始任务结果')
                }
              >
                <Input
                  mode='password'
                  autoComplete='new-password'
                  value={draft.proxy}
                  onChange={(value) => update('proxy', value)}
                  disabled={draft.clear_proxy}
                  placeholder='http://host:port'
                />
              </Field>
            </Col>
          </Row>
          {settings?.credentials_configured ? (
            <Checkbox
              checked={draft.clear_credentials}
              disabled={draft.enabled}
              onChange={(event) =>
                update('clear_credentials', event.target.checked)
              }
            >
              {t('清除已保存凭证（需先关闭转存）')}
            </Checkbox>
          ) : null}
          {settings?.proxy_configured ? (
            <Checkbox
              checked={draft.clear_proxy}
              onChange={(event) => update('clear_proxy', event.target.checked)}
            >
              {t('清除已保存代理')}
            </Checkbox>
          ) : null}
          {testResult ? (
            <Banner
              type='success'
              icon={<IconTickCircle />}
              description={t('上传、读取和清理均成功，耗时 {{latency}} ms', {
                latency: testResult.latency_ms,
              })}
            />
          ) : null}
          <div
            style={{
              display: 'flex',
              justifyContent: 'flex-end',
              gap: 8,
              flexWrap: 'wrap',
            }}
          >
            <Button
              icon={<IconTickCircle />}
              loading={action === 'test'}
              disabled={!!action}
              onClick={() => runAction('test')}
            >
              {t('测试连接')}
            </Button>
            <Button
              theme='solid'
              type='primary'
              icon={<IconSave />}
              loading={action === 'save'}
              disabled={!!action || !dirty}
              onClick={() => runAction('save')}
            >
              {t('保存设置')}
            </Button>
          </div>
        </div>
      </Spin>
    </Card>
  );
};

export default TaskResultStorageSetting;
