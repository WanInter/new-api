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
*/
import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Edit, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { BadgeCell, StaticDataTable } from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import { SettingsPageActionsPortal } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { ModelPricingRuleDialog } from './model-pricing-rule-dialog'
import {
  createModelPricingRule,
  deleteModelPricingRule,
  getModelPricingRules,
  updateModelPricingRule,
} from './model-pricing-rules-api'
import type {
  ModelPricingRule,
  ModelPricingRulePayload,
} from './model-pricing-rules-types'

const MODEL_PRICING_RULES_QUERY_KEY = ['model-pricing-rules'] as const

function getErrorMessage(error: unknown, fallback: string): string {
  if (typeof error === 'object' && error !== null && 'response' in error) {
    const response = error.response as { data?: { message?: string } }
    if (response.data?.message) return response.data.message
  }
  return fallback
}

export function ModelPricingRulesSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<ModelPricingRule | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<ModelPricingRule | null>(
    null
  )
  const rulesQuery = useQuery({
    queryKey: MODEL_PRICING_RULES_QUERY_KEY,
    queryFn: getModelPricingRules,
  })
  const saveMutation = useMutation({
    mutationFn: (input: {
      id: number | null
      payload: ModelPricingRulePayload
    }) =>
      input.id === null
        ? createModelPricingRule(input.payload)
        : updateModelPricingRule(input.id, input.payload),
  })
  const deleteMutation = useMutation({
    mutationFn: deleteModelPricingRule,
  })

  const rules = rulesQuery.data?.success ? rulesQuery.data.data : []
  const hasLoadError = rulesQuery.isError || rulesQuery.data?.success === false

  const openCreate = () => {
    setEditingRule(null)
    setDialogOpen(true)
  }

  const openEdit = (rule: ModelPricingRule) => {
    setEditingRule(rule)
    setDialogOpen(true)
  }

  const handleSave = async (payload: ModelPricingRulePayload) => {
    try {
      const result = await saveMutation.mutateAsync({
        id: editingRule?.id ?? null,
        payload,
      })
      if (!result.success) {
        toast.error(result.message || t('Request failed'))
        return false
      }
      await queryClient.invalidateQueries({
        queryKey: MODEL_PRICING_RULES_QUERY_KEY,
      })
      toast.success(t(editingRule ? 'Rule updated' : 'Rule created'))
      return true
    } catch (error) {
      toast.error(getErrorMessage(error, t('Request failed')))
      return false
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    try {
      const result = await deleteMutation.mutateAsync(deleteTarget.id)
      if (!result.success) {
        toast.error(result.message || t('Request failed'))
        return
      }
      await queryClient.invalidateQueries({
        queryKey: MODEL_PRICING_RULES_QUERY_KEY,
      })
      toast.success(t('Rule deleted'))
      setDeleteTarget(null)
    } catch (error) {
      toast.error(getErrorMessage(error, t('Request failed')))
    }
  }

  const columns = useMemo(
    () => [
      {
        id: 'subject',
        header: t('Billing Subject'),
        cell: (rule: ModelPricingRule) => (
          <div className='flex min-w-0 items-center gap-2'>
            <StatusBadge
              label={rule.subject_type === 'user' ? t('User') : t('User Group')}
              variant={rule.subject_type === 'user' ? 'info' : 'purple'}
              copyable={false}
            />
            <span className='truncate font-mono text-sm'>
              {rule.subject_value}
            </span>
          </div>
        ),
      },
      {
        id: 'model',
        header: t('Model'),
        cellClassName: 'max-w-[280px] font-mono text-sm',
        cell: (rule: ModelPricingRule) => rule.model,
      },
      {
        id: 'routing-group',
        header: t('Routing Group'),
        cell: (rule: ModelPricingRule) => rule.using_group || t('Any'),
      },
      {
        id: 'ratio',
        header: t('Ratio'),
        cellClassName: 'font-mono text-sm tabular-nums',
        cell: (rule: ModelPricingRule) => rule.ratio,
      },
      {
        id: 'status',
        header: t('Status'),
        cell: (rule: ModelPricingRule) => (
          <BadgeCell>
            <StatusBadge
              label={rule.enabled ? t('Enabled') : t('Disabled')}
              variant={rule.enabled ? 'success' : 'neutral'}
              copyable={false}
            />
          </BadgeCell>
        ),
      },
      {
        id: 'actions',
        header: t('Actions'),
        className: 'w-[96px] text-right',
        cellClassName: 'text-right',
        cell: (rule: ModelPricingRule) => (
          <TooltipProvider>
            <div className='flex justify-end gap-1'>
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant='ghost'
                      size='sm'
                      aria-label={t('Edit')}
                      onClick={() => openEdit(rule)}
                    >
                      <Edit className='h-4 w-4' />
                    </Button>
                  }
                />
                <TooltipContent>{t('Edit')}</TooltipContent>
              </Tooltip>
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant='ghost'
                      size='sm'
                      aria-label={t('Delete')}
                      onClick={() => setDeleteTarget(rule)}
                    >
                      <Trash2 className='text-destructive h-4 w-4' />
                    </Button>
                  }
                />
                <TooltipContent>{t('Delete')}</TooltipContent>
              </Tooltip>
            </div>
          </TooltipProvider>
        ),
      },
    ],
    [t]
  )

  return (
    <SettingsSection title={t('Precise Model Pricing')}>
      <SettingsPageActionsPortal>
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type='button'
                  size='sm'
                  variant='outline'
                  aria-label={t('Refresh')}
                  onClick={() => rulesQuery.refetch()}
                  disabled={rulesQuery.isFetching}
                >
                  <RefreshCw
                    className={
                      rulesQuery.isFetching ? 'h-4 w-4 animate-spin' : 'h-4 w-4'
                    }
                  />
                </Button>
              }
            />
            <TooltipContent>{t('Refresh')}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
        <Button type='button' size='sm' onClick={openCreate}>
          <Plus data-icon='inline-start' />
          {t('Create Rule')}
        </Button>
      </SettingsPageActionsPortal>

      <StaticDataTable
        data={rules}
        getRowKey={(rule) => rule.id}
        empty={rulesQuery.isLoading || hasLoadError || rules.length === 0}
        emptyClassName='text-muted-foreground py-8 text-sm'
        emptyContent={
          rulesQuery.isLoading
            ? t('Loading settings...')
            : hasLoadError
              ? t('Request failed')
              : t('No precise model pricing rules')
        }
        columns={columns}
      />

      <ModelPricingRuleDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        rule={editingRule}
        isSaving={saveMutation.isPending}
        onSave={handleSave}
      />
      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t('Delete Rule')}
        desc={t('Delete this precise model pricing rule?')}
        confirmText={t('Delete')}
        destructive
        isLoading={deleteMutation.isPending}
        handleConfirm={handleDelete}
      />
    </SettingsSection>
  )
}
