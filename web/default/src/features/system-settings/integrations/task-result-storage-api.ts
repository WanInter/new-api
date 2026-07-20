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
import { api } from '@/lib/api'

export type TaskResultStorageSource =
  | 'database'
  | 'environment'
  | 'default'
  | 'none'

export type TaskResultStorageSettings = {
  enabled: boolean
  domains: string
  backend: 'tencent_cos' | 's3' | 'aliyun_oss' | 'idrive'
  upload_endpoint: string
  bucket: string
  region: string
  public_base_url: string
  use_path_style: boolean
  signed_url_expiry_hours: number
  prefix: string
  max_mb: number
  timeout_seconds: number
  credentials_configured: boolean
  proxy_configured: boolean
  config_source: TaskResultStorageSource
  credential_source: TaskResultStorageSource
  proxy_source: TaskResultStorageSource
}

export type TaskResultStorageUpdate = {
  enabled: boolean
  domains: string
  backend: TaskResultStorageSettings['backend']
  upload_endpoint: string
  bucket: string
  region: string
  public_base_url: string
  use_path_style: boolean
  signed_url_expiry_hours: number
  prefix: string
  max_mb: number
  timeout_seconds: number
  access_key_id?: string
  access_key_secret?: string
  proxy?: string
  clear_credentials?: boolean
  clear_proxy?: boolean
}

type ApiResponse<T> = {
  success: boolean
  message: string
  data: T
}

export type TaskResultStorageConnectionResult = {
  object_url: string
  latency_ms: number
  uploaded: boolean
  readable: boolean
  cleaned_up: boolean
}

export async function getTaskResultStorageSettings() {
  const response = await api.get<ApiResponse<TaskResultStorageSettings>>(
    '/api/option/task-result-rehost'
  )
  return response.data
}

export async function saveTaskResultStorageSettings(
  update: TaskResultStorageUpdate
) {
  const response = await api.put<ApiResponse<TaskResultStorageSettings>>(
    '/api/option/task-result-rehost',
    update
  )
  return response.data
}

export async function testTaskResultStorageSettings(
  update: TaskResultStorageUpdate
) {
  const response = await api.post<
    ApiResponse<TaskResultStorageConnectionResult>
  >('/api/option/task-result-rehost/test', update)
  return response.data
}
