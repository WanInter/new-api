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
import React, { useState, useEffect, useMemo } from 'react';
import {
  Modal,
  Table,
  Badge,
  Typography,
  Toast,
  Empty,
  Button,
  Input,
  Tag,
} from '@douyinfe/semi-ui';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import { Coins } from 'lucide-react';
import { IconSearch } from '@douyinfe/semi-icons';
import { API, timestamp2string } from '../../../helpers';
import { getQuotaPerUnit, renderQuota } from '../../../helpers/render';
import { isAdmin } from '../../../helpers/utils';
import { useIsMobile } from '../../../hooks/common/useIsMobile';
const { Text } = Typography;

// 状态映射配置
const STATUS_CONFIG = {
  success: { type: 'success', key: '成功' },
  pending: { type: 'warning', key: '待支付' },
  failed: { type: 'danger', key: '失败' },
  expired: { type: 'danger', key: '已过期' },
};

// 支付方式映射
const PAYMENT_METHOD_MAP = {
  stripe: 'Stripe',
  creem: 'Creem',
  waffo: 'Waffo',
  alipay: '支付宝（易支付）',
  alipay_direct: '支付宝',
  wxpay: '微信',
  wechat_pay: '微信支付',
};

const getPaymentCurrencySymbol = (record) => {
  const provider = record?.payment_provider || '';
  const method = record?.payment_method || '';
  if (
    provider === 'stripe' ||
    method === 'stripe' ||
    provider === 'waffo' ||
    method === 'waffo'
  ) {
    return '$';
  }
  return '¥';
};

const getPaymentAccountDisplay = (record) => {
  return (
    record?.payment_account ||
    record?.buyer_user_name ||
    record?.buyer_logon_id ||
    '-'
  );
};

const getPaymentAccountSubText = (record) => {
  const display = getPaymentAccountDisplay(record);
  const parts = [];
  if (record?.buyer_user_name && record.buyer_user_name !== display) {
    parts.push(record.buyer_user_name);
  }
  if (record?.buyer_logon_id && record.buyer_logon_id !== display) {
    parts.push(record.buyer_logon_id);
  }
  if (record?.buyer_user_id && record.buyer_user_id !== display) {
    parts.push(record.buyer_user_id);
  }
  return parts.join(' · ');
};

const getUserDisplay = (record) => {
  return (
    record?.display_name ||
    record?.username ||
    record?.email ||
    (record?.user_id ? String(record.user_id) : '-')
  );
};

const getUserSubText = (record) => {
  const parts = [];
  if (record?.user_id) {
    parts.push(`ID: ${record.user_id}`);
  }
  if (record?.username && record.username !== getUserDisplay(record)) {
    parts.push(record.username);
  }
  if (record?.user_group) {
    parts.push(record.user_group);
  }
  return parts.join(' · ');
};

const formatPaymentMoney = (money, record) => {
  const value = Number(money || 0);
  if (!Number.isFinite(value)) {
    return '-';
  }
  return `${getPaymentCurrencySymbol(record)}${value.toFixed(2)}`;
};

const formatTopupQuotaAmount = (amount) => {
  const value = Number(amount || 0);
  if (!Number.isFinite(value)) {
    return '-';
  }
  const quotaPerUnit = getQuotaPerUnit() || 1;
  return renderQuota(value * quotaPerUnit);
};

const noWrapStyle = { whiteSpace: 'nowrap' };

const NoWrap = ({ children, className = '', style = {} }) => (
  <span className={className} style={{ ...noWrapStyle, ...style }}>
    {children}
  </span>
);

const TopupHistoryModal = ({ visible, onCancel, t }) => {
  const [loading, setLoading] = useState(false);
  const [topups, setTopups] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [keyword, setKeyword] = useState('');
  const [detailRecord, setDetailRecord] = useState(null);
  const isMobile = useIsMobile();

  const loadTopups = async (currentPage, currentPageSize) => {
    setLoading(true);
    try {
      const base = isAdmin() ? '/api/user/topup' : '/api/user/topup/self';
      const qs =
        `p=${currentPage}&page_size=${currentPageSize}` +
        (keyword ? `&keyword=${encodeURIComponent(keyword)}` : '');
      const endpoint = `${base}?${qs}`;
      const res = await API.get(endpoint);
      const { success, message, data } = res.data;
      if (success) {
        setTopups(data.items || []);
        setTotal(data.total || 0);
      } else {
        Toast.error({ content: message || t('加载失败') });
      }
    } catch (error) {
      Toast.error({ content: t('加载账单失败') });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (visible) {
      loadTopups(page, pageSize);
    }
  }, [visible, page, pageSize, keyword]);

  const handlePageChange = (currentPage) => {
    setPage(currentPage);
  };

  const handlePageSizeChange = (currentPageSize) => {
    setPageSize(currentPageSize);
    setPage(1);
  };

  const handleKeywordChange = (value) => {
    setKeyword(value);
    setPage(1);
  };

  // 管理员补单
  const handleAdminComplete = async (tradeNo) => {
    try {
      const res = await API.post('/api/user/topup/complete', {
        trade_no: tradeNo,
      });
      const { success, message } = res.data;
      if (success) {
        Toast.success({ content: t('补单成功') });
        await loadTopups(page, pageSize);
      } else {
        Toast.error({ content: message || t('补单失败') });
      }
    } catch (e) {
      Toast.error({ content: t('补单失败') });
    }
  };

  const confirmAdminComplete = (tradeNo) => {
    Modal.confirm({
      title: t('确认补单'),
      content: t('是否将该订单标记为成功并为用户入账？'),
      onOk: () => handleAdminComplete(tradeNo),
    });
  };

  // 渲染状态徽章
  const renderStatusBadge = (status) => {
    const config = STATUS_CONFIG[status] || { type: 'primary', key: status };
    return (
      <span className='flex items-center gap-2'>
        <Badge dot type={config.type} />
        <span>{t(config.key)}</span>
      </span>
    );
  };

  // 渲染支付方式
  const renderPaymentMethod = (pm) => {
    const displayName = PAYMENT_METHOD_MAP[pm];
    return <Text>{displayName ? t(displayName) : pm || '-'}</Text>;
  };

  const formatCompleteTime = (record) => {
    return record?.complete_time > 0
      ? timestamp2string(record.complete_time)
      : '-';
  };

  const DetailItem = ({ label, value, mono, danger }) => (
    <div
      className='rounded-xl border p-4'
      style={{ borderColor: '#e5e7eb', background: '#f8fafc' }}
    >
      <div className='mb-2 text-xs font-medium text-gray-500'>{label}</div>
      <div
        className={`${mono ? 'font-mono text-sm' : 'text-base font-semibold'} ${
          danger ? 'text-red-500' : 'text-gray-900'
        } break-words`}
      >
        {value || '-'}
      </div>
    </div>
  );

  const isSubscriptionTopup = (record) => {
    const tradeNo = (record?.trade_no || '').toLowerCase();
    return Number(record?.amount || 0) === 0 && tradeNo.startsWith('sub');
  };

  // 检查是否为管理员
  const userIsAdmin = useMemo(() => isAdmin(), []);

  const columns = useMemo(() => {
    const baseColumns = [
      {
        title: <NoWrap>{t('订单号')}</NoWrap>,
        dataIndex: 'trade_no',
        key: 'trade_no',
        width: 375,
        render: (text) => (
          <div className='flex w-full items-center gap-2 whitespace-nowrap'>
            <span className='font-mono text-sm'>{text || '-'}</span>
            <span className='inline-flex w-5 shrink-0 justify-center'>
              <Text copyable={{ content: text }} />
            </span>
          </div>
        ),
      },
      {
        title: <NoWrap>{t('支付方式')}</NoWrap>,
        dataIndex: 'payment_method',
        key: 'payment_method',
        width: 90,
        render: (pm) => <NoWrap>{renderPaymentMethod(pm)}</NoWrap>,
      },
      ...(userIsAdmin
        ? [
            {
              title: <NoWrap>{t('用户')}</NoWrap>,
              dataIndex: 'user_id',
              key: 'user_id',
              width: 190,
              render: (_, record) => (
                <div style={{ minWidth: 0 }}>
                  <div className='flex min-w-0 items-center gap-1 whitespace-nowrap'>
                    <Text
                      strong
                      ellipsis={{ showTooltip: true }}
                      style={{ maxWidth: 150 }}
                    >
                      {getUserDisplay(record)}
                    </Text>
                    <Text
                      copyable={{ content: String(record.user_id || '') }}
                    />
                  </div>
                  <div className='text-xs text-gray-500'>
                    <Text
                      type='tertiary'
                      ellipsis={{ showTooltip: true }}
                      style={{ maxWidth: 170 }}
                    >
                      {getUserSubText(record)}
                    </Text>
                  </div>
                </div>
              ),
            },
          ]
        : []),
      {
        title: <NoWrap>{t('支付账号')}</NoWrap>,
        key: 'payment_account',
        width: 110,
        render: (_, record) => (
          <Text
            strong
            ellipsis={{ showTooltip: true }}
            style={{ maxWidth: 90 }}
          >
            {getPaymentAccountDisplay(record)}
          </Text>
        ),
      },
      {
        title: <NoWrap>{t('到账额度')}</NoWrap>,
        dataIndex: 'amount',
        key: 'amount',
        width: 105,
        render: (amount, record) => {
          if (isSubscriptionTopup(record)) {
            return (
              <Tag color='purple' shape='circle' size='small'>
                {t('订阅套餐')}
              </Tag>
            );
          }
          return (
            <NoWrap className='flex items-center gap-1'>
              <Coins size={16} />
              <Text>{formatTopupQuotaAmount(amount)}</Text>
            </NoWrap>
          );
        },
      },
      {
        title: <NoWrap>{t('实付金额')}</NoWrap>,
        dataIndex: 'money',
        key: 'money',
        width: 105,
        render: (money, record) => (
          <NoWrap>
            <Text type='danger'>{formatPaymentMoney(money, record)}</Text>
          </NoWrap>
        ),
      },
      {
        title: <NoWrap>{t('状态')}</NoWrap>,
        dataIndex: 'status',
        key: 'status',
        width: 90,
        render: (status) => <NoWrap>{renderStatusBadge(status)}</NoWrap>,
      },
    ];

    // 管理员才显示操作列
    if (userIsAdmin) {
      baseColumns.push({
        title: <NoWrap>{t('操作')}</NoWrap>,
        key: 'action',
        width: 105,
        render: (_, record) => {
          const actions = [];
          if (record.status === 'pending') {
            actions.push(
              <Button
                key='complete'
                size='small'
                type='primary'
                theme='outline'
                onClick={() => confirmAdminComplete(record.trade_no)}
              >
                {t('补单')}
              </Button>,
            );
          }
          actions.push(
            <Button
              key='detail'
              size='small'
              theme='borderless'
              onClick={() => setDetailRecord(record)}
            >
              {t('详情')}
            </Button>,
          );
          return actions.length > 0 ? <>{actions}</> : null;
        },
      });
    } else {
      baseColumns.push({
        title: <NoWrap>{t('操作')}</NoWrap>,
        key: 'action',
        width: 90,
        render: (_, record) => (
          <Button
            size='small'
            theme='borderless'
            onClick={() => setDetailRecord(record)}
          >
            {t('详情')}
          </Button>
        ),
      });
    }

    baseColumns.push({
      title: <NoWrap>{t('完成时间')}</NoWrap>,
      dataIndex: 'complete_time',
      key: 'complete_time',
      width: 165,
      render: (_, record) => <NoWrap>{formatCompleteTime(record)}</NoWrap>,
    });

    baseColumns.push({
      title: <NoWrap>{t('创建时间')}</NoWrap>,
      dataIndex: 'create_time',
      key: 'create_time',
      width: 165,
      render: (time) => <NoWrap>{timestamp2string(time)}</NoWrap>,
    });

    return baseColumns;
  }, [t, userIsAdmin]);

  return (
    <>
      <Modal
        title={t('充值账单')}
        visible={visible}
        onCancel={onCancel}
        footer={null}
        width={isMobile ? '100vw' : 'min(96vw, 1680px)'}
        bodyStyle={{ maxHeight: 'calc(100vh - 180px)', overflow: 'hidden' }}
      >
        <div className='mb-3'>
          <Input
            prefix={<IconSearch />}
            placeholder={t('订单号')}
            value={keyword}
            onChange={handleKeywordChange}
            showClear
          />
        </div>
        <Table
          columns={columns}
          dataSource={topups}
          loading={loading}
          rowKey='id'
          pagination={{
            currentPage: page,
            pageSize: pageSize,
            total: total,
            showSizeChanger: true,
            pageSizeOpts: [10, 20, 50, 100],
            onPageChange: handlePageChange,
            onPageSizeChange: handlePageSizeChange,
          }}
          size='small'
          scroll={{ x: userIsAdmin ? 1500 : 1300, y: 'calc(100vh - 330px)' }}
          empty={
            <Empty
              image={
                <IllustrationNoResult style={{ width: 150, height: 150 }} />
              }
              darkModeImage={
                <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
              }
              description={t('暂无充值记录')}
              style={{ padding: 30 }}
            />
          }
        />
      </Modal>

      <Modal
        title={t('充值订单详情')}
        visible={!!detailRecord}
        onCancel={() => setDetailRecord(null)}
        footer={null}
        width={isMobile ? '94vw' : 860}
        bodyStyle={{ maxHeight: 'calc(100vh - 190px)', overflowY: 'auto' }}
      >
        {detailRecord && (
          <div className='space-y-4 pb-1'>
            <div
              className='flex flex-wrap items-center justify-between gap-3 rounded-xl border p-4'
              style={{ borderColor: '#e5e7eb', background: '#f8fafc' }}
            >
              <Text
                copyable
                strong
                ellipsis={{ showTooltip: true }}
                style={{ maxWidth: isMobile ? '70vw' : 640 }}
                className='font-mono'
              >
                {detailRecord.trade_no}
              </Text>
              {renderStatusBadge(detailRecord.status)}
            </div>
            <div className='grid grid-cols-1 gap-3 md:grid-cols-2'>
              {userIsAdmin && (
                <DetailItem
                  label={t('用户')}
                  value={`${getUserDisplay(detailRecord)} (${getUserSubText(
                    detailRecord,
                  )})`}
                />
              )}
              <DetailItem
                label={t('支付方式')}
                value={
                  PAYMENT_METHOD_MAP[detailRecord.payment_method]
                    ? t(PAYMENT_METHOD_MAP[detailRecord.payment_method])
                    : detailRecord.payment_method
                }
              />
              <DetailItem
                label={t('支付提供方')}
                value={detailRecord.payment_provider}
              />
              <DetailItem
                label={t('支付模式')}
                value={detailRecord.payment_mode}
              />
              <DetailItem
                label={t('支付账号')}
                value={getPaymentAccountDisplay(detailRecord)}
              />
              <DetailItem
                label={t('支付账号详情')}
                value={getPaymentAccountSubText(detailRecord)}
              />
              <DetailItem
                label={t('到账额度')}
                value={
                  isSubscriptionTopup(detailRecord)
                    ? t('订阅套餐')
                    : formatTopupQuotaAmount(detailRecord.amount)
                }
              />
              <DetailItem
                label={t('实付金额')}
                value={formatPaymentMoney(detailRecord.money, detailRecord)}
                danger
              />
              <DetailItem
                label={t('创建时间')}
                value={timestamp2string(detailRecord.create_time)}
              />
              <DetailItem
                label={t('完成时间')}
                value={formatCompleteTime(detailRecord)}
              />
              <DetailItem label={t('记录 ID')} value={detailRecord.id} mono />
            </div>
          </div>
        )}
      </Modal>
    </>
  );
};

export default TopupHistoryModal;
