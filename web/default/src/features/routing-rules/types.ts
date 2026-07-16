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
  fixed_duration?: number
  require_json?: boolean
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
}

export type VideoRoutingRuleSet = {
  public_model: string
  group?: string
  strict: boolean
  candidates: VideoRoutingCandidate[]
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
