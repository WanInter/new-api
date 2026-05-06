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
import { Banner, Button, Form, Row, Col, Spin } from '@douyinfe/semi-ui';
import {
  API,
  removeTrailingSlash,
  showError,
  showSuccess,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

export default function SettingsPaymentGatewayWeChat(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const formApiRef = useRef(null);
  const [inputs, setInputs] = useState({
    WeChatPayEnabled: false,
    WeChatPayMchID: '',
    WeChatPayAppID: '',
    WeChatPayAPIv3Key: '',
    WeChatPayPrivateKey: '',
    WeChatPayMerchantSerialNo: '',
    WeChatPayPublicKeyID: '',
    WeChatPayPublicKey: '',
    WeChatPayUnitPrice: 1,
    WeChatPayMinTopUp: 1,
    WeChatPayNotifyUrl: '',
    WeChatPayOrderDescription: '',
  });
  const [originInputs, setOriginInputs] = useState({});

  useEffect(() => {
    if (props.options && formApiRef.current) {
      const currentInputs = {
        WeChatPayEnabled:
          props.options.WeChatPayEnabled !== undefined
            ? props.options.WeChatPayEnabled === true ||
              props.options.WeChatPayEnabled === 'true'
            : false,
        WeChatPayMchID: props.options.WeChatPayMchID || '',
        WeChatPayAppID: props.options.WeChatPayAppID || '',
        WeChatPayAPIv3Key: props.options.WeChatPayAPIv3Key || '',
        WeChatPayPrivateKey: props.options.WeChatPayPrivateKey || '',
        WeChatPayMerchantSerialNo: props.options.WeChatPayMerchantSerialNo || '',
        WeChatPayPublicKeyID: props.options.WeChatPayPublicKeyID || '',
        WeChatPayPublicKey: props.options.WeChatPayPublicKey || '',
        WeChatPayUnitPrice:
          props.options.WeChatPayUnitPrice !== undefined
            ? parseFloat(props.options.WeChatPayUnitPrice)
            : 1,
        WeChatPayMinTopUp:
          props.options.WeChatPayMinTopUp !== undefined
            ? parseFloat(props.options.WeChatPayMinTopUp)
            : 1,
        WeChatPayNotifyUrl: props.options.WeChatPayNotifyUrl || '',
        WeChatPayOrderDescription: props.options.WeChatPayOrderDescription || '',
      };
      setInputs(currentInputs);
      setOriginInputs({ ...currentInputs });
      formApiRef.current.setValues(currentInputs);
    }
  }, [props.options]);

  const handleFormChange = (values) => {
    setInputs(values);
  };

  const submitWeChatPaySettings = async () => {
    if (
      (!props.options?.ServerAddress || props.options.ServerAddress === '') &&
      !inputs.WeChatPayNotifyUrl
    ) {
      showError(t('请先填写服务器地址'));
      return;
    }

    setLoading(true);
    try {
      const options = [];
      if (originInputs.WeChatPayEnabled !== inputs.WeChatPayEnabled) {
        options.push({
          key: 'WeChatPayEnabled',
          value: inputs.WeChatPayEnabled ? 'true' : 'false',
        });
      }
      if (originInputs.WeChatPayMchID !== inputs.WeChatPayMchID) {
        options.push({ key: 'WeChatPayMchID', value: inputs.WeChatPayMchID || '' });
      }
      if (originInputs.WeChatPayAppID !== inputs.WeChatPayAppID) {
        options.push({ key: 'WeChatPayAppID', value: inputs.WeChatPayAppID || '' });
      }
      if (
        originInputs.WeChatPayMerchantSerialNo !== inputs.WeChatPayMerchantSerialNo
      ) {
        options.push({
          key: 'WeChatPayMerchantSerialNo',
          value: inputs.WeChatPayMerchantSerialNo || '',
        });
      }
      if (originInputs.WeChatPayPublicKeyID !== inputs.WeChatPayPublicKeyID) {
        options.push({
          key: 'WeChatPayPublicKeyID',
          value: inputs.WeChatPayPublicKeyID || '',
        });
      }
      if (originInputs.WeChatPayUnitPrice !== inputs.WeChatPayUnitPrice) {
        options.push({
          key: 'WeChatPayUnitPrice',
          value: String(inputs.WeChatPayUnitPrice || 1),
        });
      }
      if (originInputs.WeChatPayMinTopUp !== inputs.WeChatPayMinTopUp) {
        options.push({
          key: 'WeChatPayMinTopUp',
          value: String(inputs.WeChatPayMinTopUp || 1),
        });
      }
      if (originInputs.WeChatPayNotifyUrl !== inputs.WeChatPayNotifyUrl) {
        options.push({
          key: 'WeChatPayNotifyUrl',
          value: inputs.WeChatPayNotifyUrl || '',
        });
      }
      if (
        originInputs.WeChatPayOrderDescription !==
        inputs.WeChatPayOrderDescription
      ) {
        options.push({
          key: 'WeChatPayOrderDescription',
          value: inputs.WeChatPayOrderDescription || '',
        });
      }

      if (inputs.WeChatPayAPIv3Key && inputs.WeChatPayAPIv3Key !== '') {
        options.push({
          key: 'WeChatPayAPIv3Key',
          value: inputs.WeChatPayAPIv3Key,
        });
      }
      if (inputs.WeChatPayPrivateKey && inputs.WeChatPayPrivateKey !== '') {
        options.push({
          key: 'WeChatPayPrivateKey',
          value: inputs.WeChatPayPrivateKey,
        });
      }
      if (inputs.WeChatPayPublicKey && inputs.WeChatPayPublicKey !== '') {
        options.push({
          key: 'WeChatPayPublicKey',
          value: inputs.WeChatPayPublicKey,
        });
      }

      const requestQueue = options.map((opt) =>
        API.put('/api/option/', {
          key: opt.key,
          value: opt.value,
        }),
      );
      const results = await Promise.all(requestQueue);
      const errorResults = results.filter((res) => !res.data.success);
      if (errorResults.length > 0) {
        errorResults.forEach((res) => {
          showError(res.data.message);
        });
      } else {
        showSuccess(t('更新成功'));
        setOriginInputs({ ...inputs });
        props.refresh?.();
      }
    } catch (error) {
      showError(t('更新失败'));
    }
    setLoading(false);
  };

  const defaultNotifyUrl = `${
    props.options?.ServerAddress
      ? removeTrailingSlash(props.options.ServerAddress)
      : t('网站地址')
  }/api/wechat/notify`;

  return (
    <Spin spinning={loading}>
      <Form
        initValues={inputs}
        onValueChange={handleFormChange}
        getFormApi={(api) => (formApiRef.current = api)}
      >
        <Form.Section text={t('微信支付设置')}>
          <Banner
            type='info'
            description={`${t('默认回调地址')}：${defaultNotifyUrl}`}
          />
          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Switch
                field='WeChatPayEnabled'
                size='default'
                checkedText='｜'
                uncheckedText='〇'
                label={t('启用微信支付')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='WeChatPayMchID'
                label={t('商户号')}
                placeholder={t('请输入微信支付商户号')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='WeChatPayAppID'
                label={t('微信支付 AppID')}
                placeholder={t('请输入微信支付 AppID')}
              />
            </Col>
          </Row>

          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='WeChatPayAPIv3Key'
                label={t('APIv3 密钥')}
                placeholder={t('敏感信息不显示，留空则不更新')}
                type='password'
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='WeChatPayMerchantSerialNo'
                label={t('商户证书序列号')}
                placeholder={t('请输入商户证书序列号')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='WeChatPayPublicKeyID'
                label={t('微信支付公钥 ID')}
                placeholder={t('请输入微信支付公钥 ID')}
              />
            </Col>
          </Row>

          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.TextArea
                field='WeChatPayPrivateKey'
                label={t('商户私钥 PEM')}
                placeholder={t('敏感信息不显示，留空则不更新')}
                autosize
              />
            </Col>
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.TextArea
                field='WeChatPayPublicKey'
                label={t('微信支付公钥 PEM')}
                placeholder={t('敏感信息不显示，留空则不更新')}
                autosize
              />
            </Col>
          </Row>

          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={6} lg={6} xl={6}>
              <Form.InputNumber
                field='WeChatPayUnitPrice'
                precision={2}
                label={t('充值价格（x元/美金）')}
                placeholder={t('例如：7，就是7元/美金')}
              />
            </Col>
            <Col xs={24} sm={24} md={6} lg={6} xl={6}>
              <Form.InputNumber
                field='WeChatPayMinTopUp'
                label={t('最低充值美元数量')}
                placeholder={t('例如：2，就是最低充值2$')}
              />
            </Col>
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.Input
                field='WeChatPayNotifyUrl'
                label={t('微信支付回调地址')}
                placeholder={t('留空则使用默认回调地址')}
              />
            </Col>
          </Row>

          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col span={24}>
              <Form.Input
                field='WeChatPayOrderDescription'
                label={t('订单描述')}
                placeholder={t('例如：账户充值')}
              />
            </Col>
          </Row>

          <Button style={{ marginTop: 16 }} onClick={submitWeChatPaySettings}>
            {t('更新微信支付设置')}
          </Button>
        </Form.Section>
      </Form>
    </Spin>
  );
}
