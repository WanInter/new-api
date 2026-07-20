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
export type VideoMediaRange = {
  min?: number
  max?: number
}

export type VideoModelCapability = {
  images?: VideoMediaRange
  videos?: VideoMediaRange
  audios?: VideoMediaRange
  duration?: VideoMediaRange
  fixed_duration?: number
  require_json?: boolean
  require_text?: boolean
  content_precedence?: boolean
}

export type VideoRoutingPolicy = {
  id: number
  public_model: string
  strict: boolean
  revision: number
  updated_by: number
  created_time: number
  updated_time: number
}

export type VideoRoutingCapabilityRule = {
  id: number
  scope: string
  channel_type: number
  channel_id: number
  upstream_model: string
  capability: VideoModelCapability
  revision: number
  updated_by: number
  created_time: number
  updated_time: number
}

export type ModelMappingResolution = {
  origin: string
  model: string
  mapped: boolean
  chain: string[]
}

export type VideoConstraintViolation = {
  code: string
  field?: string
  actual?: number
  expected?: number
}

export type VideoRoutingCandidate = {
  group: string
  channel_id: number
  channel_name: string
  channel_type: number
  channel_status: number
  priority: number
  weight: number
  mapping: ModelMappingResolution
  capability?: VideoModelCapability
  sources?: string[]
  configuration_error?: string
  eligible?: boolean
  selected_priority?: boolean
  violations?: VideoConstraintViolation[]
  editable_rule?: VideoRoutingCapabilityRule
}

export type VideoRoutingRuleSet = {
  public_model: string
  group?: string
  strict: boolean
  strict_source: string
  policy?: VideoRoutingPolicy
  candidates: VideoRoutingCandidate[]
}

export type UpdateVideoRoutingPolicyRequest = {
  public_model: string
  strict: boolean
  revision: number
}

export type UpsertVideoRoutingCapabilityRequest = {
  channel_id: number
  upstream_model: string
  capability: VideoModelCapability
  revision: number
}

export type UpdateVideoRoutingChannelSettingsRequest = {
  channel_id: number
  priority: number
  weight: number
}

export type VideoRoutingChannelSettings = {
  channel_id: number
  priority: number
  weight: number
}

export type VideoRoutingSimulationRequest = {
  model: string
  group: string
  images: number
  videos: number
  audios: number
  duration?: number
  content_type: string
  retry: number
  request_path?: string
}

export type VideoRoutingSimulationResult = VideoRoutingRuleSet & {
  features: {
    images: number
    videos: number
    audios: number
    duration?: number
    content_type?: string
  }
  retry: number
  target_priority?: number
}

export type RoutingApiResponse<T> = {
  success: boolean
  message?: string
  data: T
}

export type ImageRoutingTier = '1k' | '2k' | '4k'

export type ImageRoutingSize = {
  size: string
  tier: ImageRoutingTier
  sort: number
}

export type ImageRoutingRule = {
  tier: ImageRoutingTier
  channel_id: number
  rank: number
}

export type ImageRoutingPolicy = {
  id: number
  public_model: string
  strict: boolean
  default_size: string
  revision: number
  updated_by: number
  created_time: number
  updated_time: number
}

export type ImageRoutingChannel = {
  channel_id: number
  channel_name?: string
  channel_type?: number
  channel_status?: number
  priority?: number
  weight?: number
  group?: string
  mapping: ModelMappingResolution
  tier?: ImageRoutingTier
  rank?: number
  eligible: boolean
  selected?: boolean
  exclusion_reason?: string
  configuration_error?: string
}

export type ImageRoutingConfig = {
  public_model: string
  group?: string
  configured: boolean
  policy?: ImageRoutingPolicy
  strict: boolean
  default_size: string
  revision: number
  sizes: ImageRoutingSize[]
  rules: ImageRoutingRule[]
  candidates: ImageRoutingChannel[]
}

export type ReplaceImageRoutingConfigRequest = {
  public_model: string
  strict: boolean
  default_size: string
  revision: number
  sizes: ImageRoutingSize[]
  rules: ImageRoutingRule[]
}

export type ImageRoutingSimulationRequest = {
  model: string
  group: string
  size?: string
}

export type ImageRoutingSimulationResult = ImageRoutingConfig & {
  requested_size: string
  normalized_size?: string
  resolved_tier?: ImageRoutingTier
  used_default_size: boolean
  fallback: boolean
  reason?: string
  route: ImageRoutingChannel[]
}
