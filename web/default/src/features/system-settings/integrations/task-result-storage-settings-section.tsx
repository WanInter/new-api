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
import { useEffect, useMemo } from 'react'
import { z } from 'zod'
import { useForm, useWatch } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, PlugZap, RefreshCw, Save, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import {
  SettingsPageActionsPortal,
  SettingsPageTitleStatusPortal,
} from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import {
  getTaskResultStorageSettings,
  saveTaskResultStorageSettings,
  testTaskResultStorageSettings,
  type TaskResultStorageSettings,
  type TaskResultStorageSource,
  type TaskResultStorageUpdate,
} from './task-result-storage-api'

const QUERY_KEY = ['task-result-storage-settings'] as const

const createSchema = (t: (key: string) => string) =>
  z
    .object({
      enabled: z.boolean(),
      domains: z.string(),
      backend: z.enum(['tencent_cos', 's3', 'aliyun_oss', 'idrive']),
      uploadEndpoint: z.string(),
      bucket: z.string(),
      region: z.string(),
      publicBaseURL: z.string(),
      usePathStyle: z.boolean(),
      signedURLExpiryHours: z.number().int().min(1).max(168),
      prefix: z.string().trim().min(1, t('Object prefix is required')),
      maxMB: z.number().int().min(1).max(10240),
      timeoutSeconds: z.number().int().min(1).max(1800),
      accessKeyID: z.string(),
      accessKeySecret: z.string(),
      proxy: z.string(),
      clearCredentials: z.boolean(),
      clearProxy: z.boolean(),
    })
    .superRefine((values, context) => {
      const hasAccessID = values.accessKeyID.trim() !== ''
      const hasAccessSecret = values.accessKeySecret.trim() !== ''
      if (hasAccessID !== hasAccessSecret) {
        context.addIssue({
          code: 'custom',
          path: hasAccessID ? ['accessKeySecret'] : ['accessKeyID'],
          message: t('Enter both access key fields'),
        })
      }
      if (values.enabled && values.domains.trim() === '') {
        context.addIssue({
          code: 'custom',
          path: ['domains'],
          message: t('At least one source domain is required'),
        })
      }
      if (values.enabled && values.clearCredentials) {
        context.addIssue({
          code: 'custom',
          path: ['clearCredentials'],
          message: t('Disable storage before clearing credentials'),
        })
      }
    })

type FormValues = z.infer<ReturnType<typeof createSchema>>

const EMPTY_VALUES: FormValues = {
  enabled: false,
  domains: '',
  backend: 'tencent_cos',
  uploadEndpoint: '',
  bucket: '',
  region: '',
  publicBaseURL: '',
  usePathStyle: false,
  signedURLExpiryHours: 168,
  prefix: 'generated/newapi/videos',
  maxMB: 512,
  timeoutSeconds: 180,
  accessKeyID: '',
  accessKeySecret: '',
  proxy: '',
  clearCredentials: false,
  clearProxy: false,
}

function settingsToValues(settings: TaskResultStorageSettings): FormValues {
  return {
    enabled: settings.enabled,
    domains: settings.domains,
    backend: settings.backend,
    uploadEndpoint: settings.upload_endpoint,
    bucket: settings.bucket,
    region: settings.region,
    publicBaseURL: settings.public_base_url,
    usePathStyle: settings.use_path_style,
    signedURLExpiryHours: settings.signed_url_expiry_hours,
    prefix: settings.prefix,
    maxMB: settings.max_mb,
    timeoutSeconds: settings.timeout_seconds,
    accessKeyID: '',
    accessKeySecret: '',
    proxy: '',
    clearCredentials: false,
    clearProxy: false,
  }
}

function valuesToUpdate(values: FormValues): TaskResultStorageUpdate {
  const update: TaskResultStorageUpdate = {
    enabled: values.enabled,
    domains: values.domains.trim(),
    backend: values.backend,
    upload_endpoint: values.uploadEndpoint.trim(),
    bucket: values.bucket.trim(),
    region: values.region.trim(),
    public_base_url: values.publicBaseURL.trim(),
    use_path_style: values.usePathStyle,
    signed_url_expiry_hours: values.signedURLExpiryHours,
    prefix: values.prefix.trim(),
    max_mb: values.maxMB,
    timeout_seconds: values.timeoutSeconds,
    clear_credentials: values.clearCredentials,
    clear_proxy: values.clearProxy,
  }
  if (values.accessKeyID.trim() !== '') {
    update.access_key_id = values.accessKeyID.trim()
    update.access_key_secret = values.accessKeySecret.trim()
  }
  if (values.proxy.trim() !== '') {
    update.proxy = values.proxy.trim()
  }
  return update
}

function sourceLabel(source: TaskResultStorageSource) {
  switch (source) {
    case 'database':
      return 'Database'
    case 'environment':
      return 'Environment variables'
    case 'default':
      return 'Defaults'
    default:
      return 'Not configured'
  }
}

export function TaskResultStorageSettingsSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const schema = useMemo(() => createSchema(t), [t])
  const query = useQuery({
    queryKey: QUERY_KEY,
    queryFn: getTaskResultStorageSettings,
  })
  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: EMPTY_VALUES,
  })
  const backend = useWatch({ control: form.control, name: 'backend' })

  useEffect(() => {
    if (query.data?.data) {
      form.reset(settingsToValues(query.data.data))
    }
  }, [form, query.data?.data])

  const saveMutation = useMutation({
    mutationFn: saveTaskResultStorageSettings,
    onSuccess: (response) => {
      queryClient.setQueryData(QUERY_KEY, response)
      form.reset(settingsToValues(response.data))
      toast.success(t('Task result storage settings saved'))
    },
    onError: (error: Error) => toast.error(error.message),
  })
  const testMutation = useMutation({
    mutationFn: testTaskResultStorageSettings,
    onSuccess: () => toast.success(t('Object storage connection succeeded')),
    onError: (error: Error) => toast.error(error.message),
  })

  const onSave = form.handleSubmit((values) =>
    saveMutation.mutateAsync(valuesToUpdate(values))
  )
  const onTest = form.handleSubmit((values) =>
    testMutation.mutateAsync(valuesToUpdate(values))
  )
  const applyTencentDefaults = () => {
    const bucket = form.getValues('bucket').trim()
    const region = form.getValues('region').trim()
    if (region !== '') {
      form.setValue(
        'uploadEndpoint',
        `https://cos-internal.${region}.myqcloud.com`,
        { shouldDirty: true }
      )
    }
    if (bucket !== '' && region !== '') {
      form.setValue(
        'publicBaseURL',
        `https://${bucket}.cos.${region}.myqcloud.com`,
        { shouldDirty: true }
      )
    }
  }
  const applyIDriveDefaults = () => {
    const region = form.getValues('region').trim()
    if (region !== '') {
      form.setValue('uploadEndpoint', `https://s3.${region}.idrivee2.com`, {
        shouldDirty: true,
      })
    }
    form.setValue('publicBaseURL', '', { shouldDirty: true })
    form.setValue('usePathStyle', true, { shouldDirty: true })
  }

  if (query.isLoading) {
    return (
      <SettingsSection title={t('Task Result Storage')}>
        <div className='flex flex-col gap-4'>
          <Skeleton className='h-16 w-full' />
          <Skeleton className='h-48 w-full' />
        </div>
      </SettingsSection>
    )
  }

  if (query.isError || !query.data?.data) {
    return (
      <SettingsSection title={t('Task Result Storage')}>
        <Alert variant='destructive'>
          <AlertTitle>{t('Unable to load storage settings')}</AlertTitle>
          <AlertDescription>{query.error?.message}</AlertDescription>
        </Alert>
      </SettingsSection>
    )
  }

  const settings = query.data.data
  const busy = saveMutation.isPending || testMutation.isPending

  return (
    <SettingsSection title={t('Task Result Storage')}>
      <SettingsPageTitleStatusPortal>
        <Badge variant='outline'>
          {t(sourceLabel(settings.config_source))}
        </Badge>
      </SettingsPageTitleStatusPortal>
      <SettingsPageActionsPortal>
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={onTest}
          disabled={busy}
        >
          {testMutation.isPending ? (
            <Spinner data-icon='inline-start' />
          ) : (
            <PlugZap data-icon='inline-start' />
          )}
          {t('Test Connection')}
        </Button>
        <Button
          type='button'
          size='sm'
          onClick={onSave}
          disabled={busy || !form.formState.isDirty}
        >
          {saveMutation.isPending ? (
            <Spinner data-icon='inline-start' />
          ) : (
            <Save data-icon='inline-start' />
          )}
          {t(saveMutation.isPending ? 'Saving...' : 'Save Changes')}
        </Button>
      </SettingsPageActionsPortal>

      <Form {...form}>
        <SettingsForm onSubmit={onSave} autoComplete='off'>
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable remote result rehosting')}</FormLabel>
                  <FormDescription>
                    {t('Copy matching task results to managed object storage')}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={busy}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='backend'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Storage backend')}</FormLabel>
                <Select
                  value={field.value}
                  onValueChange={(value) => {
                    field.onChange(value)
                    if (value === 'idrive') {
                      form.setValue('publicBaseURL', '', { shouldDirty: true })
                      form.setValue('usePathStyle', true, { shouldDirty: true })
                    }
                  }}
                  disabled={busy}
                >
                  <FormControl>
                    <SelectTrigger className='w-full'>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectItem value='tencent_cos'>
                        {t('Tencent COS')}
                      </SelectItem>
                      <SelectItem value='s3'>
                        {t('S3-compatible storage')}
                      </SelectItem>
                      <SelectItem value='idrive'>{t('iDrive E2')}</SelectItem>
                      <SelectItem value='aliyun_oss'>
                        {t('Aliyun OSS')}
                      </SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='domains'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Source domains')}</FormLabel>
                <FormControl>
                  <Textarea
                    placeholder='vidgen.x.ai,example.com'
                    rows={3}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Comma-separated domains whose task results should be rehosted'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='bucket'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Bucket')}</FormLabel>
                <FormControl>
                  <Input placeholder='media-1250000000' {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='region'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Region')}</FormLabel>
                <FormControl>
                  <Input placeholder='ap-guangzhou' {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='uploadEndpoint'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Upload endpoint')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder='https://cos-internal.ap-guangzhou.myqcloud.com'
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t('Use the same-region private endpoint when available')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          {backend !== 'idrive' ? (
            <FormField
              control={form.control}
              name='publicBaseURL'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Public base URL')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='https://media-1250000000.cos.ap-guangzhou.myqcloud.com'
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Returned to clients after an object is stored')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ) : null}

          {backend === 's3' ? (
            <FormField
              control={form.control}
              name='usePathStyle'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Use path-style addressing')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Send requests as endpoint/bucket/object for storage providers that do not support virtual-hosted buckets'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={busy}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
          ) : null}

          {backend === 'tencent_cos' ? (
            <div data-settings-form-span='full'>
              <Button
                type='button'
                size='sm'
                variant='outline'
                onClick={applyTencentDefaults}
                disabled={busy}
              >
                <RefreshCw data-icon='inline-start' />
                {t('Apply recommended COS endpoints')}
              </Button>
            </div>
          ) : null}
          {backend === 'idrive' ? (
            <div data-settings-form-span='full'>
              <Button
                type='button'
                size='sm'
                variant='outline'
                onClick={applyIDriveDefaults}
                disabled={busy}
              >
                <RefreshCw data-icon='inline-start' />
                {t('Apply recommended iDrive E2 endpoint')}
              </Button>
            </div>
          ) : null}

          {backend === 'idrive' ? (
            <FormField
              control={form.control}
              name='signedURLExpiryHours'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Signed URL expiry (hours)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={168}
                      value={field.value}
                      onChange={(event) =>
                        field.onChange(Number(event.target.value))
                      }
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          ) : null}

          <FormField
            control={form.control}
            name='prefix'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Object prefix')}</FormLabel>
                <FormControl>
                  <Input placeholder='generated/newapi/videos' {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='maxMB'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Maximum file size (MB)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={1}
                    max={10240}
                    value={field.value}
                    onChange={(event) =>
                      field.onChange(Number(event.target.value))
                    }
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='timeoutSeconds'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Timeout (seconds)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={1}
                    max={1800}
                    value={field.value}
                    onChange={(event) =>
                      field.onChange(Number(event.target.value))
                    }
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <div
            data-settings-form-span='full'
            className='flex flex-wrap items-center gap-2'
          >
            <span className='text-sm font-medium'>
              {t('Access credentials')}
            </span>
            <Badge
              variant={
                settings.credentials_configured ? 'secondary' : 'outline'
              }
            >
              {t(
                settings.credentials_configured
                  ? 'Configured'
                  : 'Not configured'
              )}
            </Badge>
            <span className='text-muted-foreground text-xs'>
              {t(sourceLabel(settings.credential_source))}
            </span>
          </div>
          <FormField
            control={form.control}
            name='accessKeyID'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Access Key ID')}</FormLabel>
                <FormControl>
                  <Input
                    type='password'
                    autoComplete='new-password'
                    placeholder={
                      settings.credentials_configured
                        ? t('Leave blank to keep the configured value')
                        : t('Enter Access Key ID')
                    }
                    disabled={busy || form.watch('clearCredentials')}
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='accessKeySecret'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Access Key Secret')}</FormLabel>
                <FormControl>
                  <Input
                    type='password'
                    autoComplete='new-password'
                    placeholder={
                      settings.credentials_configured
                        ? t('Leave blank to keep the configured value')
                        : t('Enter Access Key Secret')
                    }
                    disabled={busy || form.watch('clearCredentials')}
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          {settings.credentials_configured ? (
            <FormField
              control={form.control}
              name='clearCredentials'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Clear stored credentials')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Disable rehosting before removing active credentials'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={busy || form.watch('enabled')}
                    />
                  </FormControl>
                  <FormMessage />
                </SettingsSwitchItem>
              )}
            />
          ) : null}

          <div
            data-settings-form-span='full'
            className='flex flex-wrap items-center gap-2'
          >
            <span className='text-sm font-medium'>{t('Download proxy')}</span>
            <Badge
              variant={settings.proxy_configured ? 'secondary' : 'outline'}
            >
              {t(settings.proxy_configured ? 'Configured' : 'Not configured')}
            </Badge>
            <span className='text-muted-foreground text-xs'>
              {t(sourceLabel(settings.proxy_source))}
            </span>
          </div>
          <FormField
            control={form.control}
            name='proxy'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Proxy URL')}</FormLabel>
                <FormControl>
                  <Input
                    type='password'
                    autoComplete='new-password'
                    placeholder={
                      settings.proxy_configured
                        ? t('Leave blank to keep the configured value')
                        : 'http://host:port'
                    }
                    disabled={busy || form.watch('clearProxy')}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t('Used only when downloading the original task result')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          {settings.proxy_configured ? (
            <FormField
              control={form.control}
              name='clearProxy'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Clear stored proxy')}</FormLabel>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={busy}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
          ) : null}

          {testMutation.isSuccess ? (
            <Alert>
              <CheckCircle2 />
              <AlertTitle>{t('Connection successful')}</AlertTitle>
              <AlertDescription>
                {t(
                  'Upload, read, and cleanup completed in {{latency}} ms',
                  { latency: testMutation.data.data.latency_ms }
                )}
              </AlertDescription>
            </Alert>
          ) : null}
          {testMutation.isError ? (
            <Alert variant='destructive'>
              <XCircle />
              <AlertTitle>{t('Connection failed')}</AlertTitle>
              <AlertDescription>{testMutation.error.message}</AlertDescription>
            </Alert>
          ) : null}
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
