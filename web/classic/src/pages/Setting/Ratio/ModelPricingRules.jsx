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

import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import {
  Button,
  Form,
  Modal,
  Space,
  Table,
  Tag,
  Tooltip,
} from '@douyinfe/semi-ui';
import {
  IconDelete,
  IconEdit,
  IconHelpCircle,
  IconPlus,
} from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../../helpers';

const EMPTY_RULE = {
  subject_type: 'user',
  subject_value: '',
  model: '',
  using_group: '',
  ratio: 1,
  enabled: true,
};

const replaceOptions = (current, incoming, selectedValue) => {
  const selected =
    incoming.find((option) => option.value === selectedValue) ||
    current.find((option) => option.value === selectedValue);
  if (!selected || incoming.some((option) => option.value === selected.value)) {
    return incoming;
  }
  return [selected, ...incoming];
};

export default function ModelPricingRules() {
  const { t } = useTranslation();
  const [rules, setRules] = useState([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [modalVisible, setModalVisible] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState(null);
  const [draft, setDraft] = useState(EMPTY_RULE);
  const [userOptions, setUserOptions] = useState([]);
  const [modelOptions, setModelOptions] = useState([]);
  const [userOptionsLoading, setUserOptionsLoading] = useState(false);
  const [modelOptionsLoading, setModelOptionsLoading] = useState(false);
  const userSearchTimer = useRef(null);
  const userRequestId = useRef(0);
  const modelRequestId = useRef(0);

  const loadRules = useCallback(async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/model-pricing-rules/');
      if (res.data.success) {
        setRules(res.data.data || []);
      } else {
        showError(res.data.message);
      }
    } catch (error) {
      showError(t('刷新失败'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    loadRules();
  }, [loadRules]);

  useEffect(
    () => () => {
      clearTimeout(userSearchTimer.current);
      userRequestId.current += 1;
      modelRequestId.current += 1;
    },
    [],
  );

  const loadUserOptions = useCallback(
    async (keyword = '', selectedValue = '') => {
      const requestId = ++userRequestId.current;
      setUserOptionsLoading(true);
      try {
        const res = await API.get('/api/user/search', {
          params: { keyword, group: '', status: 1, p: 1, page_size: 100 },
        });
        if (!res.data.success) {
          showError(res.data.message);
          return;
        }
        const options = (res.data.data?.items || []).map((user) => ({
          value: String(user.id),
          label: `${user.username} (#${user.id})`,
        }));
        if (requestId === userRequestId.current) {
          setUserOptions((current) =>
            replaceOptions(current, options, selectedValue),
          );
        }
      } catch (error) {
        if (requestId === userRequestId.current) {
          showError(t('加载用户失败'));
        }
      } finally {
        if (requestId === userRequestId.current) {
          setUserOptionsLoading(false);
        }
      }
    },
    [t],
  );

  const loadModelOptions = useCallback(
    async (selectedValue = '') => {
      const requestId = ++modelRequestId.current;
      setModelOptionsLoading(true);
      try {
        const res = await API.get('/api/channel/models_enabled');
        if (!res.data.success) {
          showError(res.data.message);
          return;
        }
        const options = Array.from(
          new Set(
            (res.data.data || [])
              .map((model) => String(model || '').trim())
              .filter(Boolean),
          ),
        )
          .sort((left, right) => left.localeCompare(right))
          .map((model) => ({ value: model, label: model }));
        const selectedModel = String(selectedValue || '').trim();
        if (
          selectedModel &&
          !options.some((option) => option.value === selectedModel)
        ) {
          options.unshift({ value: selectedModel, label: selectedModel });
        }
        if (requestId === modelRequestId.current) {
          setModelOptions(options);
        }
      } catch (error) {
        if (requestId === modelRequestId.current) {
          showError(t('加载模型失败'));
        }
      } finally {
        if (requestId === modelRequestId.current) {
          setModelOptionsLoading(false);
        }
      }
    },
    [t],
  );

  const searchUsers = (keyword) => {
    clearTimeout(userSearchTimer.current);
    userSearchTimer.current = setTimeout(
      () => loadUserOptions(keyword.trim(), draft.subject_value),
      300,
    );
  };

  const openCreate = () => {
    setDraft(EMPTY_RULE);
    setUserOptions([]);
    setModelOptions([]);
    setModalVisible(true);
    loadUserOptions();
    loadModelOptions();
  };

  const openEdit = (rule) => {
    setDraft({
      id: rule.id,
      subject_type: rule.subject_type,
      subject_value: rule.subject_value,
      model: rule.model,
      using_group: rule.using_group || '',
      ratio: rule.ratio,
      enabled: rule.enabled,
    });
    setUserOptions([]);
    setModelOptions([]);
    setModalVisible(true);
    loadUserOptions(
      rule.subject_type === 'user' ? rule.subject_value : '',
      rule.subject_type === 'user' ? rule.subject_value : '',
    );
    loadModelOptions(rule.model);
  };

  const saveRule = async () => {
    const subjectValue = String(draft.subject_value || '').trim();
    const model = String(draft.model || '').trim();
    const usingGroup = String(draft.using_group || '').trim();
    const ratio = Number(draft.ratio);
    if (!subjectValue || !model || !Number.isFinite(ratio) || ratio < 0) {
      showError(t('请检查输入'));
      return;
    }

    const payload = {
      subject_type: draft.subject_type,
      subject_value: subjectValue,
      model,
      using_group: usingGroup,
      ratio,
      enabled: !!draft.enabled,
    };
    setSaving(true);
    try {
      const res = draft.id
        ? await API.put(`/api/model-pricing-rules/${draft.id}`, payload)
        : await API.post('/api/model-pricing-rules/', payload);
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      showSuccess(t(draft.id ? '更新成功' : '创建成功'));
      setModalVisible(false);
      await loadRules();
    } catch (error) {
      showError(t('保存失败'));
    } finally {
      setSaving(false);
    }
  };

  const deleteRule = async () => {
    if (!deleteTarget) return;
    setSaving(true);
    try {
      const res = await API.delete(
        `/api/model-pricing-rules/${deleteTarget.id}`,
      );
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      showSuccess(t('删除成功'));
      setDeleteTarget(null);
      await loadRules();
    } catch (error) {
      showError(t('删除失败'));
    } finally {
      setSaving(false);
    }
  };

  const columns = useMemo(
    () => [
      {
        title: t('计费主体'),
        dataIndex: 'subject_value',
        render: (value, record) => (
          <Space spacing={4}>
            <Tag color={record.subject_type === 'user' ? 'blue' : 'violet'}>
              {record.subject_type === 'user' ? t('用户') : t('用户组')}
            </Tag>
            <span>{value}</span>
          </Space>
        ),
      },
      { title: t('模型'), dataIndex: 'model' },
      {
        title: t('路由分组'),
        dataIndex: 'using_group',
        render: (value) => value || t('任意'),
      },
      { title: t('倍率'), dataIndex: 'ratio' },
      {
        title: t('状态'),
        dataIndex: 'enabled',
        render: (value) => (
          <Tag color={value ? 'green' : 'grey'}>
            {value ? t('启用') : t('禁用')}
          </Tag>
        ),
      },
      {
        title: t('操作'),
        key: 'actions',
        render: (_, record) => (
          <Space spacing={4}>
            <Tooltip content={t('编辑')}>
              <Button
                theme='borderless'
                icon={<IconEdit />}
                aria-label={t('编辑')}
                onClick={() => openEdit(record)}
              />
            </Tooltip>
            <Tooltip content={t('删除')}>
              <Button
                theme='borderless'
                type='danger'
                icon={<IconDelete />}
                aria-label={t('删除')}
                onClick={() => setDeleteTarget(record)}
              />
            </Tooltip>
          </Space>
        ),
      },
    ],
    [t],
  );

  return (
    <>
      <div
        style={{
          display: 'flex',
          justifyContent: 'flex-end',
          margin: '12px 0',
        }}
      >
        <Button type='primary' icon={<IconPlus />} onClick={openCreate}>
          {t('创建规则')}
        </Button>
      </div>
      <Table
        columns={columns}
        dataSource={rules}
        rowKey='id'
        loading={loading}
        pagination={{ pageSize: 10, showSizeChanger: true }}
        scroll={{ x: 'max-content' }}
        empty={t('暂无精确模型计费规则')}
      />

      <Modal
        title={draft.id ? t('编辑规则') : t('创建规则')}
        visible={modalVisible}
        onOk={saveRule}
        onCancel={() => setModalVisible(false)}
        okText={t('保存')}
        cancelText={t('取消')}
        confirmLoading={saving}
      >
        <Form layout='vertical' initValues={draft} key={draft.id || 'new'}>
          <Form.Select
            field='subject_type'
            label={t('主体类型')}
            value={draft.subject_type}
            optionList={[
              { value: 'user', label: t('用户') },
              { value: 'user_group', label: t('用户组') },
            ]}
            onChange={(value) =>
              setDraft((current) => ({ ...current, subject_type: value }))
            }
          />
          {draft.subject_type === 'user' ? (
            <Form.Select
              field='subject_value'
              label={t('用户 ID')}
              placeholder={t('搜索用户名或 ID')}
              searchPlaceholder={t('搜索用户名或 ID')}
              filter
              remote
              optionList={userOptions}
              loading={userOptionsLoading}
              emptyContent={t('未找到用户')}
              value={draft.subject_value || undefined}
              onSearch={searchUsers}
              onChange={(value) =>
                setDraft((current) => ({
                  ...current,
                  subject_value: String(value || ''),
                }))
              }
              style={{ width: '100%' }}
            />
          ) : (
            <Form.Input
              field='subject_value'
              label={t('用户组')}
              value={draft.subject_value}
              onChange={(value) =>
                setDraft((current) => ({ ...current, subject_value: value }))
              }
            />
          )}
          <Form.Select
            field='model'
            label={t('模型')}
            placeholder={t('搜索模型...')}
            searchPlaceholder={t('搜索模型...')}
            filter
            allowCreate
            optionList={modelOptions}
            loading={modelOptionsLoading}
            emptyContent={t('未找到模型')}
            value={draft.model || undefined}
            onChange={(value) =>
              setDraft((current) => ({
                ...current,
                model: String(value || ''),
              }))
            }
            style={{ width: '100%' }}
          />
          <Form.Input
            field='using_group'
            label={
              <Space spacing={4}>
                <span>{t('路由分组')}</span>
                <Tooltip
                  content={t('请求最终实际命中的渠道分组；留空表示任意分组。')}
                >
                  <IconHelpCircle className='cursor-help text-gray-400' />
                </Tooltip>
              </Space>
            }
            value={draft.using_group}
            onChange={(value) =>
              setDraft((current) => ({ ...current, using_group: value }))
            }
          />
          <Form.InputNumber
            field='ratio'
            label={t('倍率')}
            min={0}
            precision={2}
            step={0.01}
            value={draft.ratio}
            onChange={(value) =>
              setDraft((current) => ({ ...current, ratio: value }))
            }
            style={{ width: '100%' }}
          />
          <Form.Switch
            field='enabled'
            label={t('启用')}
            checked={draft.enabled}
            onChange={(value) =>
              setDraft((current) => ({ ...current, enabled: value }))
            }
          />
        </Form>
      </Modal>

      <Modal
        title={t('删除规则')}
        visible={!!deleteTarget}
        onOk={deleteRule}
        onCancel={() => setDeleteTarget(null)}
        okText={t('删除')}
        cancelText={t('取消')}
        confirmLoading={saving}
        type='warning'
        okButtonProps={{ type: 'danger', theme: 'solid' }}
      >
        {t('确定删除此精确模型计费规则吗？')}
      </Modal>
    </>
  );
}
