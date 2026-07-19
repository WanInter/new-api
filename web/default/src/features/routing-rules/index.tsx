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
import { Link } from '@tanstack/react-router'
import {
  AlertCircle,
  ArrowLeft,
  CheckCircle2,
  Eye,
  FlaskConical,
  Pencil,
  RefreshCw,
  Route as RouteIcon,
  XCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
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
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { SectionPageLayout } from '@/components/layout'
import { getChannelTypeLabel } from '@/features/channels/lib/channel-utils'
import { getGroups } from '@/features/users/api'
import { USER_ROLE } from '@/features/users/constants'
import {
  getVideoRoutingRules,
  simulateVideoRouting,
  updateVideoRoutingPolicy,
} from './api'
import { CapabilityRuleEditor } from './components/capability-rule-editor'
import { ImageRoutingPanel } from './components/image-routing-panel'
import type {
  VideoMediaRange,
  VideoRoutingCandidate,
  VideoRoutingSimulationRequest,
  VideoRoutingSimulationResult,
} from './types'

const DEFAULT_MODEL = 'sd-bak-1'
const DEFAULT_GROUP = 'creative-video'

function formatRange(range?: VideoMediaRange) {
  if (!range || (range.min === undefined && range.max === undefined)) return '—'
  if (range.min !== undefined && range.max !== undefined) {
    return range.min === range.max
      ? String(range.min)
      : `${range.min}–${range.max}`
  }
  if (range.min !== undefined) return `≥ ${range.min}`
  return `≤ ${range.max}`
}

function formatDurationCapability(
  capability?: VideoRoutingCandidate['capability']
) {
  if (capability?.fixed_duration !== undefined) {
    return `${capability.fixed_duration}s`
  }
  const range = formatRange(capability?.duration)
  return range === '—' ? range : `${range}s`
}

function violationText(
  violation: NonNullable<VideoRoutingCandidate['violations']>[number],
  t: (key: string, options?: Record<string, unknown>) => string
) {
  const values = { actual: violation.actual, expected: violation.expected }
  const messages: Record<string, string> = {
    images_below_min: t('Requires at least {{expected}} images', values),
    images_above_max: t('Supports at most {{expected}} images', values),
    videos_above_max: t('Supports at most {{expected}} videos', values),
    audios_above_max: t('Supports at most {{expected}} audios', values),
    duration_mismatch: t('Requires a duration of {{expected}} seconds', values),
    duration_below_min: t(
      'Requires a duration of at least {{expected}} seconds',
      values
    ),
    duration_above_max: t(
      'Supports a duration of at most {{expected}} seconds',
      values
    ),
    content_type_mismatch: t('Requires an application/json request'),
    missing_capability: t('No capability profile is configured'),
    invalid_content: t('Explicit content contains an invalid item'),
    text_below_min: t('Explicit content must include a non-empty text item'),
  }
  return messages[violation.code] || violation.code
}

function configurationErrorText(error: string, t: (key: string) => string) {
  if (error === 'request_path_not_supported') {
    return t('Channel does not support the video request path')
  }
  return error
}

function CandidateStatus({ candidate }: { candidate: VideoRoutingCandidate }) {
  const { t } = useTranslation()
  if (candidate.configuration_error) {
    return (
      <span className='text-destructive inline-flex items-center gap-1.5'>
        <AlertCircle className='size-4' /> {t('Invalid configuration')}
      </span>
    )
  }
  if (candidate.eligible === true) {
    return (
      <span className='inline-flex items-center gap-1.5 text-emerald-600 dark:text-emerald-400'>
        <CheckCircle2 className='size-4' />
        {candidate.selected_priority ? t('Selected tier') : t('Eligible')}
      </span>
    )
  }
  if (candidate.eligible === false || candidate.violations?.length) {
    return (
      <span className='text-muted-foreground inline-flex items-center gap-1.5'>
        <XCircle className='size-4' /> {t('Excluded')}
      </span>
    )
  }
  return (
    <span className='inline-flex items-center gap-1.5 text-emerald-600 dark:text-emerald-400'>
      <CheckCircle2 className='size-4' /> {t('Configured')}
    </span>
  )
}

function CandidateTable({
  candidates,
  loading,
  onInspect,
  onEdit,
}: {
  candidates: VideoRoutingCandidate[]
  loading?: boolean
  onInspect: (candidate: VideoRoutingCandidate) => void
  onEdit?: (candidate: VideoRoutingCandidate) => void
}) {
  const { t } = useTranslation()
  if (loading) {
    return <Skeleton className='h-64 w-full' />
  }
  return (
    <div className='overflow-x-auto rounded-md border'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className='min-w-44'>{t('Channel')}</TableHead>
            <TableHead className='min-w-36'>{t('Upstream Model')}</TableHead>
            <TableHead>{t('Images')}</TableHead>
            <TableHead>{t('Videos')}</TableHead>
            <TableHead>{t('Audios')}</TableHead>
            <TableHead>{t('Duration')}</TableHead>
            <TableHead>{t('Priority')}</TableHead>
            <TableHead>{t('Weight')}</TableHead>
            <TableHead className='min-w-32'>{t('Status')}</TableHead>
            <TableHead className='w-24' />
          </TableRow>
        </TableHeader>
        <TableBody>
          {candidates.length === 0 ? (
            <TableRow>
              <TableCell
                colSpan={10}
                className='text-muted-foreground h-28 text-center'
              >
                {t('No routing candidates found')}
              </TableCell>
            </TableRow>
          ) : (
            candidates.map((candidate) => (
              <TableRow key={`${candidate.group}-${candidate.channel_id}`}>
                <TableCell>
                  <button
                    className='text-left'
                    onClick={() => onInspect(candidate)}
                  >
                    <span className='block font-medium'>
                      {candidate.channel_name || `#${candidate.channel_id}`}
                    </span>
                    <span className='text-muted-foreground text-xs'>
                      #{candidate.channel_id} ·{' '}
                      {getChannelTypeLabel(candidate.channel_type)}
                    </span>
                    {candidate.editable_rule && (
                      <Badge variant='secondary' className='mt-1'>
                        {t('Database override')}
                      </Badge>
                    )}
                  </button>
                </TableCell>
                <TableCell className='font-mono text-xs'>
                  {candidate.mapping?.model || '—'}
                </TableCell>
                <TableCell>
                  {formatRange(candidate.capability?.images)}
                </TableCell>
                <TableCell>
                  {formatRange(candidate.capability?.videos)}
                </TableCell>
                <TableCell>
                  {formatRange(candidate.capability?.audios)}
                </TableCell>
                <TableCell>
                  {formatDurationCapability(candidate.capability)}
                </TableCell>
                <TableCell>{candidate.priority}</TableCell>
                <TableCell>{candidate.weight}</TableCell>
                <TableCell>
                  <CandidateStatus candidate={candidate} />
                </TableCell>
                <TableCell>
                  <div className='flex items-center justify-end gap-1'>
                    <Button
                      variant='ghost'
                      size='icon-sm'
                      onClick={() => onInspect(candidate)}
                      title={t('View details')}
                    >
                      <Eye className='size-4' />
                    </Button>
                    {onEdit && (
                      <Button
                        variant='ghost'
                        size='icon-sm'
                        onClick={() => onEdit(candidate)}
                        title={t('Edit routing override')}
                      >
                        <Pencil />
                      </Button>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  )
}

function CandidateDetails({
  candidate,
  onClose,
}: {
  candidate: VideoRoutingCandidate | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  return (
    <Sheet
      open={Boolean(candidate)}
      onOpenChange={(open) => !open && onClose()}
    >
      <SheetContent className='sm:max-w-xl'>
        <SheetHeader className='border-b p-5'>
          <SheetTitle>
            {candidate?.channel_name || t('Routing details')}
          </SheetTitle>
          <SheetDescription>
            {candidate ? `#${candidate.channel_id} · ${candidate.group}` : ''}
          </SheetDescription>
        </SheetHeader>
        {candidate && (
          <div className='flex-1 space-y-6 overflow-y-auto p-5'>
            <section className='space-y-3'>
              <h3 className='text-sm font-semibold'>
                {t('Model mapping chain')}
              </h3>
              <div className='flex flex-wrap items-center gap-2 font-mono text-xs'>
                {(candidate.mapping?.chain || []).map((modelName, index) => (
                  <span key={`${modelName}-${index}`} className='contents'>
                    {index > 0 && (
                      <span className='text-muted-foreground'>→</span>
                    )}
                    <code className='bg-muted rounded px-2 py-1'>
                      {modelName}
                    </code>
                  </span>
                ))}
              </div>
            </section>
            <section className='space-y-3'>
              <h3 className='text-sm font-semibold'>
                {t('Effective capability')}
              </h3>
              <dl className='grid grid-cols-2 gap-x-6 gap-y-3 text-sm'>
                <dt className='text-muted-foreground'>{t('Images')}</dt>
                <dd>{formatRange(candidate.capability?.images)}</dd>
                <dt className='text-muted-foreground'>{t('Videos')}</dt>
                <dd>{formatRange(candidate.capability?.videos)}</dd>
                <dt className='text-muted-foreground'>{t('Audios')}</dt>
                <dd>{formatRange(candidate.capability?.audios)}</dd>
                <dt className='text-muted-foreground'>{t('Duration')}</dt>
                <dd>{formatDurationCapability(candidate.capability)}</dd>
                <dt className='text-muted-foreground'>{t('Content Type')}</dt>
                <dd>
                  {candidate.capability?.require_json
                    ? 'application/json'
                    : t('Any')}
                </dd>
              </dl>
            </section>
            <section className='space-y-3'>
              <h3 className='text-sm font-semibold'>
                {t('Configuration sources')}
              </h3>
              <div className='flex flex-wrap gap-2'>
                {(candidate.sources || []).map((source) => (
                  <Badge key={source} variant='outline'>
                    {source}
                  </Badge>
                ))}
                {!candidate.sources?.length && (
                  <span className='text-muted-foreground text-sm'>—</span>
                )}
              </div>
            </section>
            {(candidate.configuration_error ||
              candidate.violations?.length) && (
              <section className='space-y-3'>
                <h3 className='text-sm font-semibold'>
                  {t('Exclusion reasons')}
                </h3>
                <ul className='space-y-2 text-sm'>
                  {candidate.configuration_error && (
                    <li className='text-destructive'>
                      {configurationErrorText(candidate.configuration_error, t)}
                    </li>
                  )}
                  {(candidate.violations || []).map((violation, index) => (
                    <li
                      key={`${violation.code}-${index}`}
                      className='text-muted-foreground'
                    >
                      {violationText(violation, t)}
                    </li>
                  ))}
                </ul>
              </section>
            )}
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}

export function RoutingRules() {
  const { t } = useTranslation()
  const isRoot =
    useAuthStore((state) => state.auth.user?.role) === USER_ROLE.ROOT
  const [model, setModel] = useState(DEFAULT_MODEL)
  const [group, setGroup] = useState(DEFAULT_GROUP)
  const [routingMode, setRoutingMode] = useState<'video' | 'image'>('video')
  const [selectedCandidate, setSelectedCandidate] =
    useState<VideoRoutingCandidate | null>(null)
  const [editingCandidate, setEditingCandidate] =
    useState<VideoRoutingCandidate | null>(null)
  const [simulation, setSimulation] = useState<VideoRoutingSimulationRequest>({
    model: DEFAULT_MODEL,
    group: DEFAULT_GROUP,
    images: 4,
    videos: 0,
    audios: 0,
    duration: 15,
    content_type: 'application/json',
    retry: 0,
  })

  const groupsQuery = useQuery({
    queryKey: ['groups', 'routing-rules'],
    queryFn: getGroups,
  })
  const rulesQuery = useQuery({
    queryKey: ['video-routing-rules', model, group],
    queryFn: () => getVideoRoutingRules(model, group),
    enabled: Boolean(model.trim() && group.trim()),
  })
  const simulationMutation = useMutation({ mutationFn: simulateVideoRouting })
  const policyMutation = useMutation({
    mutationFn: updateVideoRoutingPolicy,
    onSuccess: async () => {
      toast.success(t('Routing policy saved'))
      await rulesQuery.refetch()
    },
    onError: () => {
      toast.error(t('Failed to save routing policy'))
    },
  })
  const groups = useMemo(() => groupsQuery.data?.data || [], [groupsQuery.data])

  const runSimulation = () => {
    simulationMutation.mutate({ ...simulation, model, group })
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Routing Rules')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button variant='outline' size='sm' render={<Link to='/channels' />}>
          <ArrowLeft className='size-4' /> {t('Channels')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-4 px-3 pb-6 sm:px-4'>
          <ToggleGroup
            value={[routingMode]}
            onValueChange={(value) => {
              const next = value.find((item) => item !== routingMode)
              if (next === 'video' || next === 'image') setRoutingMode(next)
            }}
            variant='outline'
            aria-label={t('Routing Type')}
          >
            <ToggleGroupItem value='video'>
              {t('Video Routing')}
            </ToggleGroupItem>
            <ToggleGroupItem value='image'>
              {t('Image Routing')}
            </ToggleGroupItem>
          </ToggleGroup>

          {routingMode === 'image' ? (
            <ImageRoutingPanel groups={groups} isRoot={isRoot} />
          ) : (
            <>
              <div className='flex flex-wrap items-end gap-3 border-y py-3'>
                <div className='min-w-48 flex-1 space-y-1.5'>
                  <Label htmlFor='routing-model'>{t('Public Model')}</Label>
                  <Input
                    id='routing-model'
                    value={model}
                    onChange={(event) => {
                      setModel(event.target.value)
                      simulationMutation.reset()
                      setSelectedCandidate(null)
                      setEditingCandidate(null)
                    }}
                  />
                </div>
                <div className='min-w-48 flex-1 space-y-1.5'>
                  <Label htmlFor='routing-group'>{t('Group')}</Label>
                  <NativeSelect
                    id='routing-group'
                    className='w-full'
                    value={group}
                    onChange={(event) => {
                      setGroup(event.target.value)
                      simulationMutation.reset()
                      setSelectedCandidate(null)
                      setEditingCandidate(null)
                    }}
                  >
                    {!groups.includes(group) && (
                      <NativeSelectOption value={group}>
                        {group}
                      </NativeSelectOption>
                    )}
                    {groups.map((item) => (
                      <NativeSelectOption key={item} value={item}>
                        {item}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </div>
                <Button
                  variant='outline'
                  size='icon'
                  onClick={() => rulesQuery.refetch()}
                  title={t('Refresh')}
                >
                  <RefreshCw className='size-4' />
                </Button>
              </div>

              <Tabs defaultValue='rules' className='gap-4'>
                <TabsList>
                  <TabsTrigger value='rules'>
                    <RouteIcon className='size-4' /> {t('Rules')}
                  </TabsTrigger>
                  <TabsTrigger value='simulator'>
                    <FlaskConical className='size-4' /> {t('Simulator')}
                  </TabsTrigger>
                </TabsList>
                <TabsContent value='rules' className='space-y-3'>
                  {rulesQuery.data?.data && (
                    <div className='flex flex-wrap items-center justify-between gap-3 border-y py-3'>
                      <div className='flex items-center gap-2'>
                        <Label htmlFor='strict-routing'>
                          {t('Strict routing')}
                        </Label>
                        <Badge variant='outline'>
                          {rulesQuery.data.data.strict_source === 'database'
                            ? t('Database policy')
                            : rulesQuery.data.data.strict_source === 'built_in'
                              ? t('Built-in policy')
                              : t('Default policy')}
                        </Badge>
                      </div>
                      <Switch
                        id='strict-routing'
                        checked={rulesQuery.data.data.strict}
                        disabled={!isRoot || policyMutation.isPending}
                        onCheckedChange={(strict) =>
                          policyMutation.mutate({
                            public_model: model,
                            strict,
                            revision:
                              rulesQuery.data?.data.policy?.revision || 0,
                          })
                        }
                      />
                    </div>
                  )}
                  {rulesQuery.isError && (
                    <Alert variant='destructive'>
                      <AlertDescription>
                        {t('Failed to load routing rules')}
                      </AlertDescription>
                    </Alert>
                  )}
                  <CandidateTable
                    candidates={rulesQuery.data?.data.candidates || []}
                    loading={rulesQuery.isLoading}
                    onInspect={setSelectedCandidate}
                    onEdit={isRoot ? setEditingCandidate : undefined}
                  />
                </TabsContent>
                <TabsContent value='simulator' className='space-y-4'>
                  <div className='grid gap-3 border-y py-4 sm:grid-cols-3 lg:grid-cols-6'>
                    {(
                      [
                        'images',
                        'videos',
                        'audios',
                        'duration',
                        'retry',
                      ] as const
                    ).map((field) => (
                      <div key={field} className='space-y-1.5'>
                        <Label htmlFor={`simulation-${field}`}>
                          {t(field.charAt(0).toUpperCase() + field.slice(1))}
                        </Label>
                        <Input
                          id={`simulation-${field}`}
                          type='number'
                          min={field === 'duration' ? 1 : 0}
                          value={simulation[field] ?? ''}
                          onChange={(event) =>
                            setSimulation((current) => ({
                              ...current,
                              [field]:
                                event.target.value === ''
                                  ? undefined
                                  : Number(event.target.value),
                            }))
                          }
                        />
                      </div>
                    ))}
                    <div className='flex items-end'>
                      <Button
                        className='w-full'
                        onClick={runSimulation}
                        disabled={simulationMutation.isPending}
                      >
                        <FlaskConical className='size-4' /> {t('Run')}
                      </Button>
                    </div>
                  </div>
                  {simulationMutation.isError && (
                    <Alert variant='destructive'>
                      <AlertDescription>
                        {t('Routing simulation failed')}
                      </AlertDescription>
                    </Alert>
                  )}
                  {simulationMutation.data && (
                    <SimulationSummary result={simulationMutation.data.data} />
                  )}
                  <CandidateTable
                    candidates={simulationMutation.data?.data.candidates || []}
                    loading={simulationMutation.isPending}
                    onInspect={setSelectedCandidate}
                    onEdit={isRoot ? setEditingCandidate : undefined}
                  />
                </TabsContent>
              </Tabs>
            </>
          )}
        </div>
        {routingMode === 'video' && (
          <>
            <CandidateDetails
              candidate={selectedCandidate}
              onClose={() => setSelectedCandidate(null)}
            />
            <CapabilityRuleEditor
              candidate={editingCandidate}
              onClose={() => setEditingCandidate(null)}
              onSaved={async () => {
                simulationMutation.reset()
                await rulesQuery.refetch()
              }}
            />
          </>
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function SimulationSummary({
  result,
}: {
  result: VideoRoutingSimulationResult
}) {
  const { t } = useTranslation()
  const eligible = result.candidates.filter(
    (candidate) => candidate.eligible
  ).length
  return (
    <div className='text-muted-foreground flex flex-wrap gap-x-5 gap-y-1 text-sm'>
      <span>
        {t('Eligible channels')}:{' '}
        <strong className='text-foreground'>{eligible}</strong>
      </span>
      <span>
        {t('Target priority')}:{' '}
        <strong className='text-foreground'>
          {result.target_priority ?? '—'}
        </strong>
      </span>
      <span>
        {t('Retry')}:{' '}
        <strong className='text-foreground'>{result.retry}</strong>
      </span>
    </div>
  )
}
