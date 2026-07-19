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
import { useMemo, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import {
  ArrowDown,
  ArrowUp,
  FlaskConical,
  Plus,
  RefreshCw,
  Save,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import {
  getImageRoutingRules,
  replaceImageRoutingConfig,
  simulateImageRouting,
} from '../api'
import type {
  ImageRoutingChannel,
  ImageRoutingConfig,
  ImageRoutingRule,
  ImageRoutingSimulationResult,
  ImageRoutingTier,
} from '../types'

const TIERS: ImageRoutingTier[] = ['1k', '2k', '4k']
const DEFAULT_SIZES: Record<ImageRoutingTier, string[]> = {
  '1k': ['1024x1024', '1536x1024', '1024x1536', '960x1280', '1280x960'],
  '2k': ['2048x2048', '2560x1440', '1440x2560', '1920x2560', '2560x1920'],
  '4k': ['2880x2880', '3840x2160', '2160x3840', '2160x2880', '2880x2160'],
}

function createEmptyConfig(): ImageRoutingConfig {
  return {
    public_model: 'image2',
    configured: false,
    strict: true,
    default_size: '1024x1024',
    revision: 0,
    sizes: TIERS.flatMap((tier) =>
      DEFAULT_SIZES[tier].map((size, index) => ({
        size,
        tier,
        sort: index + 1,
      }))
    ),
    rules: [],
    candidates: [],
  }
}

function sizeDrafts(config: ImageRoutingConfig) {
  return Object.fromEntries(
    TIERS.map((tier) => [
      tier,
      config.sizes
        .filter((item) => item.tier === tier)
        .sort((a, b) => a.sort - b.sort)
        .map((item) => item.size)
        .join('\n'),
    ])
  ) as Record<ImageRoutingTier, string>
}

function parseSizes(value: string) {
  return [
    ...new Set(
      value
        .split(/[\s,，]+/)
        .map((item) => item.trim())
        .filter(Boolean)
    ),
  ]
}

function routeReason(reason: string | undefined, t: (key: string) => string) {
  const labels: Record<string, string> = {
    channel_not_found: t('Channel not found'),
    channel_disabled: t('Channel disabled'),
    model_unavailable: t('Model unavailable in this group'),
    invalid_model_mapping: t('Invalid model mapping'),
    request_path_not_supported: t('Request path not supported'),
  }
  return labels[reason || ''] || reason || t('Available')
}

type ImageRoutingPanelProps = {
  groups: string[]
  isRoot: boolean
}

export function ImageRoutingPanel({ groups, isRoot }: ImageRoutingPanelProps) {
  const { t } = useTranslation()
  const [model, setModel] = useState('image2')
  const [group, setGroup] = useState('default')
  const [draft, setDraft] = useState<ImageRoutingConfig>(createEmptyConfig)
  const [sizes, setSizes] = useState<Record<ImageRoutingTier, string>>(() =>
    sizeDrafts(createEmptyConfig())
  )
  const [pendingChannels, setPendingChannels] = useState<
    Partial<Record<ImageRoutingTier, string>>
  >({})
  const [simulationSize, setSimulationSize] = useState('1024x1024')
  const [loadedAt, setLoadedAt] = useState(0)

  const configQuery = useQuery({
    queryKey: ['image-routing-rules', model, group],
    queryFn: () => getImageRoutingRules(model, group),
    enabled: Boolean(model.trim() && group.trim()),
  })

  const routesByTier = useMemo(
    () =>
      Object.fromEntries(
        TIERS.map((tier) => [
          tier,
          draft.rules
            .filter((item) => item.tier === tier)
            .sort((a, b) => a.rank - b.rank),
        ])
      ) as Record<ImageRoutingTier, ImageRoutingRule[]>,
    [draft.rules]
  )

  const candidatesById = useMemo(
    () =>
      new Map(
        draft.candidates.map((candidate) => [candidate.channel_id, candidate])
      ),
    [draft.candidates]
  )

  const allSizes = TIERS.flatMap((tier) => parseSizes(sizes[tier]))

  const saveMutation = useMutation({
    mutationFn: replaceImageRoutingConfig,
    onSuccess: async () => {
      toast.success(t('Image routing configuration saved'))
      await configQuery.refetch()
    },
    onError: (error) => {
      toast.error(
        (error as { response?: { data?: { message?: string } } }).response?.data
          ?.message || t('Failed to save image routing configuration')
      )
    },
  })
  const simulationMutation = useMutation({
    mutationFn: simulateImageRouting,
    onError: () => toast.error(t('Image routing simulation failed')),
  })

  const incoming = configQuery.data?.data
  if (incoming && configQuery.dataUpdatedAt !== loadedAt) {
    const next = incoming.configured
      ? incoming
      : { ...createEmptyConfig(), candidates: incoming.candidates || [] }
    setLoadedAt(configQuery.dataUpdatedAt)
    setDraft(next)
    setSizes(sizeDrafts(next))
    setSimulationSize(next.default_size)
  }

  const replaceTierRoutes = (
    tier: ImageRoutingTier,
    routes: ImageRoutingRule[]
  ) => {
    setDraft((current) => ({
      ...current,
      rules: [
        ...current.rules.filter((item) => item.tier !== tier),
        ...routes.map((item, index) => ({
          tier,
          channel_id: item.channel_id,
          rank: index + 1,
        })),
      ],
    }))
  }

  const moveRoute = (
    tier: ImageRoutingTier,
    index: number,
    direction: number
  ) => {
    const target = index + direction
    if (target < 0 || target >= routesByTier[tier].length) return
    const next = [...routesByTier[tier]]
    ;[next[index], next[target]] = [next[target], next[index]]
    replaceTierRoutes(tier, next)
  }

  const addRoute = (tier: ImageRoutingTier) => {
    const channelId = Number(pendingChannels[tier])
    if (
      !channelId ||
      routesByTier[tier].some((item) => item.channel_id === channelId)
    )
      return
    replaceTierRoutes(tier, [
      ...routesByTier[tier],
      { tier, channel_id: channelId, rank: routesByTier[tier].length + 1 },
    ])
    setPendingChannels((current) => ({ ...current, [tier]: '' }))
  }

  const save = () => {
    saveMutation.mutate({
      public_model: model.trim(),
      strict: draft.strict,
      default_size: draft.default_size,
      revision: draft.revision || 0,
      sizes: TIERS.flatMap((tier) =>
        parseSizes(sizes[tier]).map((size, index) => ({
          size,
          tier,
          sort: index + 1,
        }))
      ),
      rules: TIERS.flatMap((tier) =>
        routesByTier[tier].map((rule, index) => ({
          tier,
          channel_id: rule.channel_id,
          rank: index + 1,
        }))
      ),
    })
  }

  if (configQuery.isLoading) {
    return (
      <div className='flex flex-col gap-3'>
        <Skeleton className='h-16 w-full' />
        <Skeleton className='h-32 w-full' />
        <Skeleton className='h-48 w-full' />
      </div>
    )
  }

  return (
    <div className='flex flex-col gap-4'>
      <FieldGroup className='border-y py-4 sm:grid sm:grid-cols-[1fr_1fr_auto] sm:items-end sm:gap-3'>
        <Field>
          <FieldLabel htmlFor='image-routing-model'>
            {t('Public Model')}
          </FieldLabel>
          <Input
            id='image-routing-model'
            value={model}
            onChange={(event) => setModel(event.target.value)}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor='image-routing-group'>{t('Group')}</FieldLabel>
          <NativeSelect
            id='image-routing-group'
            value={group}
            onChange={(event) => setGroup(event.target.value)}
          >
            {!groups.includes(group) && (
              <NativeSelectOption value={group}>{group}</NativeSelectOption>
            )}
            {groups.map((item) => (
              <NativeSelectOption key={item} value={item}>
                {item}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        <Button
          variant='outline'
          size='icon'
          onClick={() => configQuery.refetch()}
          title={t('Refresh')}
        >
          <RefreshCw />
        </Button>
      </FieldGroup>

      {!draft.configured && (
        <Alert>
          <AlertDescription>
            {t(
              'Image routing has not been saved. Requests still use static channel priorities.'
            )}
          </AlertDescription>
        </Alert>
      )}

      <FieldGroup className='border-b pb-4 sm:grid sm:grid-cols-[1fr_1fr_auto] sm:items-end sm:gap-3'>
        <Field orientation='horizontal'>
          <FieldLabel htmlFor='image-routing-strict'>
            {t('Strict image routing')}
          </FieldLabel>
          <Switch
            id='image-routing-strict'
            checked={draft.strict}
            disabled={!isRoot}
            onCheckedChange={(strict) =>
              setDraft((current) => ({ ...current, strict }))
            }
          />
        </Field>
        <Field>
          <FieldLabel htmlFor='image-routing-default-size'>
            {t('Default Size')}
          </FieldLabel>
          <NativeSelect
            id='image-routing-default-size'
            value={draft.default_size}
            disabled={!isRoot}
            onChange={(event) =>
              setDraft((current) => ({
                ...current,
                default_size: event.target.value,
              }))
            }
          >
            {allSizes.map((size) => (
              <NativeSelectOption key={size} value={size}>
                {size}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        <Button disabled={!isRoot || saveMutation.isPending} onClick={save}>
          {saveMutation.isPending ? (
            <Spinner />
          ) : (
            <Save data-icon='inline-start' />
          )}
          {t('Save Image Routing')}
        </Button>
      </FieldGroup>

      <Tabs defaultValue='1k'>
        <TabsList>
          {TIERS.map((tier) => (
            <TabsTrigger key={tier} value={tier}>
              {tier.toUpperCase()}
            </TabsTrigger>
          ))}
        </TabsList>
        {TIERS.map((tier) => (
          <TabsContent key={tier} value={tier} className='flex flex-col gap-4'>
            <Field>
              <FieldLabel htmlFor={`image-routing-sizes-${tier}`}>
                {t('Resolution values for this tier')}
              </FieldLabel>
              <Textarea
                id={`image-routing-sizes-${tier}`}
                value={sizes[tier]}
                disabled={!isRoot}
                rows={3}
                onChange={(event) =>
                  setSizes((current) => ({
                    ...current,
                    [tier]: event.target.value,
                  }))
                }
              />
            </Field>

            <FieldGroup className='sm:grid sm:grid-cols-[1fr_auto] sm:items-end sm:gap-2'>
              <Field>
                <FieldLabel htmlFor={`image-routing-channel-${tier}`}>
                  {t('Add Candidate Channel')}
                </FieldLabel>
                <NativeSelect
                  id={`image-routing-channel-${tier}`}
                  value={pendingChannels[tier] || ''}
                  disabled={!isRoot}
                  onChange={(event) =>
                    setPendingChannels((current) => ({
                      ...current,
                      [tier]: event.target.value,
                    }))
                  }
                >
                  <NativeSelectOption value=''>
                    {t('Select a channel')}
                  </NativeSelectOption>
                  {draft.candidates
                    .filter(
                      (candidate) =>
                        !routesByTier[tier].some(
                          (rule) => rule.channel_id === candidate.channel_id
                        )
                    )
                    .map((candidate) => (
                      <NativeSelectOption
                        key={candidate.channel_id}
                        value={String(candidate.channel_id)}
                      >
                        {candidate.channel_name || `#${candidate.channel_id}`}{' '}
                        (#{candidate.channel_id})
                      </NativeSelectOption>
                    ))}
                </NativeSelect>
              </Field>
              <Button
                variant='outline'
                disabled={!isRoot || !pendingChannels[tier]}
                onClick={() => addRoute(tier)}
              >
                <Plus data-icon='inline-start' /> {t('Add')}
              </Button>
            </FieldGroup>

            <RoutingTable
              routes={routesByTier[tier]}
              candidatesById={candidatesById}
              editable={isRoot}
              onMove={(index, direction) => moveRoute(tier, index, direction)}
              onRemove={(channelId) =>
                replaceTierRoutes(
                  tier,
                  routesByTier[tier].filter(
                    (item) => item.channel_id !== channelId
                  )
                )
              }
            />
          </TabsContent>
        ))}
      </Tabs>

      <div className='flex flex-col gap-4 border-y py-4'>
        <FieldGroup className='sm:grid sm:grid-cols-[1fr_auto] sm:items-end sm:gap-2'>
          <Field>
            <FieldLabel htmlFor='image-routing-simulation-size'>
              {t('Request Size')}
            </FieldLabel>
            <Input
              id='image-routing-simulation-size'
              value={simulationSize}
              onChange={(event) => setSimulationSize(event.target.value)}
            />
          </Field>
          <Button
            disabled={simulationMutation.isPending}
            onClick={() =>
              simulationMutation.mutate({ model, group, size: simulationSize })
            }
          >
            {simulationMutation.isPending ? (
              <Spinner />
            ) : (
              <FlaskConical data-icon='inline-start' />
            )}
            {t('Run Simulation')}
          </Button>
        </FieldGroup>
        {simulationMutation.data?.data && (
          <SimulationResult result={simulationMutation.data.data} />
        )}
      </div>
    </div>
  )
}

function RoutingTable({
  routes,
  candidatesById,
  editable,
  onMove,
  onRemove,
}: {
  routes: ImageRoutingRule[]
  candidatesById: Map<number, ImageRoutingChannel>
  editable: boolean
  onMove: (index: number, direction: number) => void
  onRemove: (channelId: number) => void
}) {
  const { t } = useTranslation()
  if (routes.length === 0) {
    return (
      <Empty>
        <EmptyTitle>{t('No channels configured for this tier')}</EmptyTitle>
        <EmptyDescription>
          {t('Add a candidate channel above.')}
        </EmptyDescription>
      </Empty>
    )
  }
  return (
    <div className='overflow-x-auto'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className='w-16'>{t('Order')}</TableHead>
            <TableHead>{t('Channel')}</TableHead>
            <TableHead>{t('Upstream Model')}</TableHead>
            <TableHead className='w-36' />
          </TableRow>
        </TableHeader>
        <TableBody>
          {routes.map((route, index) => {
            const candidate = candidatesById.get(route.channel_id)
            return (
              <TableRow key={route.channel_id}>
                <TableCell>{index + 1}</TableCell>
                <TableCell>
                  <div className='flex flex-col gap-0.5'>
                    <span className='font-medium'>
                      {candidate?.channel_name || `#${route.channel_id}`}
                    </span>
                    <span className='text-muted-foreground text-xs'>
                      #{route.channel_id}
                    </span>
                  </div>
                </TableCell>
                <TableCell className='font-mono text-xs'>
                  {candidate?.mapping?.model || '—'}
                </TableCell>
                <TableCell>
                  <div className='flex justify-end gap-1'>
                    <Button
                      variant='ghost'
                      size='icon-sm'
                      disabled={!editable || index === 0}
                      onClick={() => onMove(index, -1)}
                      title={t('Move Up')}
                    >
                      <ArrowUp />
                    </Button>
                    <Button
                      variant='ghost'
                      size='icon-sm'
                      disabled={!editable || index === routes.length - 1}
                      onClick={() => onMove(index, 1)}
                      title={t('Move Down')}
                    >
                      <ArrowDown />
                    </Button>
                    <Button
                      variant='ghost'
                      size='icon-sm'
                      disabled={!editable}
                      onClick={() => onRemove(route.channel_id)}
                      title={t('Remove')}
                    >
                      <Trash2 />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}

function SimulationResult({
  result,
}: {
  result: ImageRoutingSimulationResult
}) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-col gap-3'>
      <div className='flex flex-wrap items-center gap-2'>
        <Badge variant='outline'>
          {result.resolved_tier?.toUpperCase() || t('Unresolved Tier')}
        </Badge>
        <span className='text-sm'>
          {t('Normalized Size')}: {result.normalized_size || '—'}
        </span>
        {result.used_default_size && (
          <Badge variant='secondary'>{t('Default Size Used')}</Badge>
        )}
        {result.fallback && (
          <Badge variant='secondary'>{t('Static Priority Fallback')}</Badge>
        )}
      </div>
      {result.reason && (
        <Alert>
          <AlertDescription>{result.reason}</AlertDescription>
        </Alert>
      )}
      {result.route.length === 0 ? (
        <Empty>
          <EmptyTitle>{t('No matching image route')}</EmptyTitle>
        </Empty>
      ) : (
        <div className='overflow-x-auto'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className='w-16'>{t('Order')}</TableHead>
                <TableHead>{t('Channel')}</TableHead>
                <TableHead>{t('Upstream Model')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {result.route.map((candidate) => (
                <TableRow key={candidate.channel_id}>
                  <TableCell>{candidate.rank}</TableCell>
                  <TableCell>
                    {candidate.channel_name || `#${candidate.channel_id}`} (#
                    {candidate.channel_id})
                  </TableCell>
                  <TableCell className='font-mono text-xs'>
                    {candidate.mapping?.model || '—'}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={candidate.eligible ? 'default' : 'secondary'}
                    >
                      {candidate.selected
                        ? t('Primary')
                        : routeReason(candidate.exclusion_reason, t)}
                    </Badge>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}
