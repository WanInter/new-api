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
import { Button, Modal, Space, Typography } from '@douyinfe/semi-ui';
import { QRCodeSVG } from 'qrcode.react';
import { useTranslation } from 'react-i18next';

const { Text } = Typography;

export default function WeChatPayModal({
  visible,
  codeUrl,
  amount,
  tradeNo,
  onClose,
  onCopy,
  onOpenHistory,
}) {
  const { t } = useTranslation();

  return (
    <Modal
      visible={visible}
      title={t('微信扫码支付')}
      footer={null}
      onCancel={onClose}
      maskClosable={false}
      centered
    >
      <div className='flex flex-col items-center gap-3'>
        <QRCodeSVG value={codeUrl || ''} size={220} />
        <Text>
          {t('订单号')}：{tradeNo}
        </Text>
        <Text>
          {t('应付金额')}：¥{amount}
        </Text>
        <Text type='tertiary'>
          {t('请使用微信扫码支付，支付成功后到账可能有短暂延迟。')}
        </Text>
        <Space>
          <Button onClick={onCopy}>{t('复制支付链接')}</Button>
          <Button onClick={onOpenHistory}>{t('查看充值账单')}</Button>
          <Button type='primary' onClick={onClose}>
            {t('关闭')}
          </Button>
        </Space>
      </div>
    </Modal>
  );
}
