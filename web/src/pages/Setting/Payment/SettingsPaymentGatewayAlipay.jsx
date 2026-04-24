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

import React, { useEffect, useRef, useState } from 'react';
import { Banner, Button, Col, Form, Row, Spin } from '@douyinfe/semi-ui';
import {
  API,
  removeTrailingSlash,
  showError,
  showSuccess,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

export default function SettingsPaymentGatewayAlipay(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const formApiRef = useRef(null);
  const [inputs, setInputs] = useState({
    AlipayEnabled: false,
    AlipaySandbox: false,
    AlipayAppID: '',
    AlipayPrivateKey: '',
    AlipayPublicKey: '',
    AlipayUnitPrice: 1,
    AlipayMinTopUp: 1,
    AlipayPayMode: 'page',
    AlipayNotifyURL: '',
    AlipayReturnURL: '',
    AlipaySubscriptionReturnURL: '',
    AlipayOrderDescription: '',
  });

  useEffect(() => {
    if (props.options && formApiRef.current) {
      const current = {
        AlipayEnabled:
          props.options.AlipayEnabled === true ||
          props.options.AlipayEnabled === 'true',
        AlipaySandbox:
          props.options.AlipaySandbox === true ||
          props.options.AlipaySandbox === 'true',
        AlipayAppID: props.options.AlipayAppID || '',
        AlipayPrivateKey: props.options.AlipayPrivateKey || '',
        AlipayPublicKey: props.options.AlipayPublicKey || '',
        AlipayUnitPrice: parseFloat(props.options.AlipayUnitPrice || 1),
        AlipayMinTopUp: parseFloat(props.options.AlipayMinTopUp || 1),
        AlipayPayMode: props.options.AlipayPayMode || 'page',
        AlipayNotifyURL: props.options.AlipayNotifyURL || '',
        AlipayReturnURL: props.options.AlipayReturnURL || '',
        AlipaySubscriptionReturnURL:
          props.options.AlipaySubscriptionReturnURL || '',
        AlipayOrderDescription: props.options.AlipayOrderDescription || '',
      };
      setInputs(current);
      formApiRef.current.setValues(current);
    }
  }, [props.options]);

  const submitAlipay = async () => {
    setLoading(true);
    try {
      const options = [
        {
          key: 'AlipayEnabled',
          value: inputs.AlipayEnabled ? 'true' : 'false',
        },
        {
          key: 'AlipaySandbox',
          value: inputs.AlipaySandbox ? 'true' : 'false',
        },
        { key: 'AlipayAppID', value: inputs.AlipayAppID || '' },
        { key: 'AlipayUnitPrice', value: String(inputs.AlipayUnitPrice || 1) },
        { key: 'AlipayMinTopUp', value: String(inputs.AlipayMinTopUp || 1) },
        { key: 'AlipayPayMode', value: inputs.AlipayPayMode || 'page' },
        {
          key: 'AlipayNotifyURL',
          value: removeTrailingSlash(inputs.AlipayNotifyURL || ''),
        },
        {
          key: 'AlipayReturnURL',
          value: removeTrailingSlash(inputs.AlipayReturnURL || ''),
        },
        {
          key: 'AlipaySubscriptionReturnURL',
          value: removeTrailingSlash(inputs.AlipaySubscriptionReturnURL || ''),
        },
        {
          key: 'AlipayOrderDescription',
          value: inputs.AlipayOrderDescription || '',
        },
      ];
      if (inputs.AlipayPrivateKey) {
        options.push({ key: 'AlipayPrivateKey', value: inputs.AlipayPrivateKey });
      }
      if (inputs.AlipayPublicKey) {
        options.push({ key: 'AlipayPublicKey', value: inputs.AlipayPublicKey });
      }
      const results = await Promise.allSettled(
        options.map((item) => API.put('/api/option/', item)),
      );
      const rejected = results.filter((result) => result.status === 'rejected');
      const fulfilled = results
        .filter((result) => result.status === 'fulfilled')
        .map((result) => result.value);
      const errorResults = fulfilled.filter((res) => !res.data.success);

      if (rejected.length > 0) {
        rejected.forEach(() => showError(t('部分配置保存失败，请刷新后确认实际生效项。')));
      }
      if (errorResults.length > 0) {
        errorResults.forEach((res) => showError(res.data.message));
      }
      if (rejected.length === 0 && errorResults.length === 0) {
        showSuccess(t('更新成功'));
      }
      await props.refresh?.();
    } catch (error) {
      showError(t('更新失败'));
      await props.refresh?.();
    } finally {
      setLoading(false);
    }
  };

  return (
    <Spin spinning={loading}>
      <Form
        initValues={inputs}
        onValueChange={setInputs}
        getFormApi={(api) => (formApiRef.current = api)}
      >
        <Form.Section text={t('支付宝官方直连设置')}>
          <Banner
            type='info'
            description={t(
              '推荐先在沙箱环境验证 page 与 qr 两种模式，再切换正式环境。',
            )}
          />
          <Row gutter={{ xs: 8, sm: 16, md: 24 }} style={{ marginTop: 16 }}>
            <Col xs={24} sm={12} md={8}>
              <Form.Switch
                field='AlipayEnabled'
                label={t('启用支付宝官方直连')}
              />
            </Col>
            <Col xs={24} sm={12} md={8}>
              <Form.Switch
                field='AlipaySandbox'
                label={t('启用沙箱模式')}
              />
            </Col>
            <Col xs={24} sm={24} md={8}>
              <Form.Input field='AlipayAppID' label={t('支付宝 AppID')} />
            </Col>
          </Row>
          <Row gutter={{ xs: 8, sm: 16, md: 24 }} style={{ marginTop: 16 }}>
            <Col xs={24} sm={24} md={12}>
              <Form.TextArea
                field='AlipayPrivateKey'
                label={t('应用私钥')}
                autosize
                placeholder={t('敏感信息不显示，留空则不更新')}
              />
            </Col>
            <Col xs={24} sm={24} md={12}>
              <Form.TextArea
                field='AlipayPublicKey'
                label={t('支付宝公钥')}
                autosize
                placeholder={t('敏感信息不显示，留空则不更新')}
              />
            </Col>
          </Row>
          <Row gutter={{ xs: 8, sm: 16, md: 24 }} style={{ marginTop: 16 }}>
            <Col xs={24} sm={12} md={6}>
              <Form.InputNumber
                field='AlipayUnitPrice'
                label={t('单价（元）')}
                min={0.01}
                step={0.01}
              />
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Form.InputNumber
                field='AlipayMinTopUp'
                label={t('最小充值数量')}
                min={1}
                step={1}
              />
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Form.Select
                field='AlipayPayMode'
                label={t('支付宝支付模式')}
                optionList={[
                  { label: t('收银台支付'), value: 'page' },
                  { label: t('扫码支付'), value: 'qr' },
                ]}
              />
            </Col>
            <Col xs={24} sm={24} md={6}>
              <Form.Input
                field='AlipayOrderDescription'
                label={t('订单描述')}
              />
            </Col>
          </Row>
          <Row gutter={{ xs: 8, sm: 16, md: 24 }} style={{ marginTop: 16 }}>
            <Col xs={24} sm={24} md={8}>
              <Form.Input field='AlipayNotifyURL' label={t('异步通知地址')} type='url' />
            </Col>
            <Col xs={24} sm={24} md={8}>
              <Form.Input field='AlipayReturnURL' label={t('充值回跳地址')} type='url' />
            </Col>
            <Col xs={24} sm={24} md={8}>
              <Form.Input
                field='AlipaySubscriptionReturnURL'
                label={t('订阅回跳地址')}
                type='url'
              />
            </Col>
          </Row>
          <Button style={{ marginTop: 16 }} onClick={submitAlipay}>
            {t('保存支付宝设置')}
          </Button>
        </Form.Section>
      </Form>
    </Spin>
  );
}
