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

import React, { useEffect, useState } from 'react';
import { Button, Modal, Radio, Space, Typography } from '@douyinfe/semi-ui';
import { QRCodeSVG } from 'qrcode.react';
import { useTranslation } from 'react-i18next';

const { Text } = Typography;

export default function AlipayCheckoutModal({
  visible,
  title,
  amount,
  tradeNo,
  payMode,
  defaultMode = 'page',
  qrCode,
  checking,
  creating,
  onClose,
  onCreate,
  onCheck,
}) {
  const { t } = useTranslation();
  const [selectedMode, setSelectedMode] = useState(defaultMode);

  useEffect(() => {
    if (visible) {
      setSelectedMode(defaultMode || 'page');
    }
  }, [visible, defaultMode]);

  const showQRCode = visible && payMode === 'qr' && !!qrCode;
  const showManualCheck = visible && !!tradeNo;

  return (
    <Modal
      visible={visible}
      title={title}
      footer={null}
      centered
      maskClosable={false}
      onCancel={onClose}
    >
      <div className='flex flex-col gap-3'>
        {!tradeNo && (
          <>
            <Text>{t('选择支付方式')}</Text>
            <Radio.Group
              value={selectedMode}
              onChange={(event) => setSelectedMode(event.target.value)}
            >
              <Space vertical>
                <Radio value='page'>{t('支付宝收银台支付')}</Radio>
                <Radio value='qr'>{t('支付宝扫码支付')}</Radio>
              </Space>
            </Radio.Group>
            <Text>{t('应付金额')}：¥{amount}</Text>
            <Button
              type='primary'
              loading={creating}
              onClick={() => onCreate(selectedMode)}
            >
              {t('确认并发起支付')}
            </Button>
          </>
        )}
        {tradeNo && (
          <>
            <Text>{t('订单号')}：{tradeNo}</Text>
            <Text>{t('应付金额')}：¥{amount}</Text>
            {showQRCode && <QRCodeSVG value={qrCode} size={220} />}
            <Text type='tertiary'>
              {payMode === 'page'
                ? t('已打开支付宝收银台，请完成支付后返回检查状态。')
                : t('请使用支付宝扫码支付，支付成功后到账可能有短暂延迟。')}
            </Text>
            {showManualCheck && (
              <Button loading={checking} onClick={onCheck}>
                {t('我已完成支付，检查状态')}
              </Button>
            )}
            <Button onClick={onClose}>{t('关闭')}</Button>
          </>
        )}
      </div>
    </Modal>
  );
}
