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

import React from 'react';
import { Button, Modal, Typography } from '@douyinfe/semi-ui';
import { QRCodeSVG } from 'qrcode.react';
import { useTranslation } from 'react-i18next';

const { Text } = Typography;

export default function AlipayCheckoutModal({
  visible,
  title,
  amount,
  tradeNo,
  qrCode,
  checking,
  onClose,
  onCheck,
  currencySymbol = '¥',
}) {
  const { t } = useTranslation();
  const closeDisabled = checking;

  return (
    <Modal
      visible={visible}
      title={title}
      footer={null}
      centered
      maskClosable={!closeDisabled}
      onCancel={closeDisabled ? undefined : onClose}
    >
      <div className='flex flex-col gap-3'>
        <Text>{t('订单号')}：{tradeNo}</Text>
        <Text>{t('应付金额')}：{currencySymbol}{amount}</Text>
        {qrCode ? <QRCodeSVG value={qrCode} size={220} /> : null}
        <Text type='tertiary'>
          {t('请使用支付宝扫码支付，支付成功后到账可能有短暂延迟。')}
        </Text>
        <Button loading={checking} onClick={onCheck}>
          {t('我已完成支付，检查状态')}
        </Button>
        <Button onClick={onClose} disabled={closeDisabled}>
          {t('关闭')}
        </Button>
      </div>
    </Modal>
  );
}
