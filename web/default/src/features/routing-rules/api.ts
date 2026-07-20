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
import type {
  ImageRoutingConfig,
  ImageRoutingSimulationRequest,
  ImageRoutingSimulationResult,
  ReplaceImageRoutingConfigRequest,
  RoutingApiResponse,
  UpdateVideoRoutingPolicyRequest,
  UpdateVideoRoutingChannelSettingsRequest,
  UpsertVideoRoutingCapabilityRequest,
  VideoRoutingChannelSettings,
  VideoRoutingCapabilityRule,
  VideoRoutingPolicy,
  VideoRoutingRuleSet,
  VideoRoutingSimulationRequest,
  VideoRoutingSimulationResult,
} from './types'

export async function getImageRoutingRules(model: string, group: string) {
  const response = await api.get<RoutingApiResponse<ImageRoutingConfig>>(
    '/api/channel/image_routing_rules',
    { params: { model, group } }
  )
  return response.data
}

export async function replaceImageRoutingConfig(
  request: ReplaceImageRoutingConfigRequest
) {
  const response = await api.put<RoutingApiResponse<ImageRoutingConfig>>(
    '/api/channel/image_routing_rules/config',
    request
  )
  return response.data
}

export async function simulateImageRouting(
  request: ImageRoutingSimulationRequest
) {
  const response = await api.post<
    RoutingApiResponse<ImageRoutingSimulationResult>
  >('/api/channel/image_routing_rules/simulate', request)
  return response.data
}

export async function getVideoRoutingRules(model: string, group: string) {
  const response = await api.get<RoutingApiResponse<VideoRoutingRuleSet>>(
    '/api/channel/routing_rules',
    { params: { model, group } }
  )
  return response.data
}

export async function simulateVideoRouting(
  request: VideoRoutingSimulationRequest
) {
  const response = await api.post<
    RoutingApiResponse<VideoRoutingSimulationResult>
  >('/api/channel/routing_rules/simulate', request)
  return response.data
}

export async function updateVideoRoutingPolicy(
  request: UpdateVideoRoutingPolicyRequest
) {
  const response = await api.put<RoutingApiResponse<VideoRoutingPolicy>>(
    '/api/channel/routing_rules/policy',
    request
  )
  return response.data
}

export async function updateVideoRoutingChannelSettings(
  request: UpdateVideoRoutingChannelSettingsRequest
) {
  const response = await api.put<
    RoutingApiResponse<VideoRoutingChannelSettings>
  >('/api/channel/routing_rules/channel_settings', request)
  return response.data
}

export async function upsertVideoRoutingCapability(
  request: UpsertVideoRoutingCapabilityRequest
) {
  const response = await api.put<
    RoutingApiResponse<VideoRoutingCapabilityRule>
  >('/api/channel/routing_rules/capability', request)
  return response.data
}

export async function deleteVideoRoutingCapability(
  id: number,
  revision: number
) {
  const response = await api.delete<
    RoutingApiResponse<VideoRoutingCapabilityRule>
  >(`/api/channel/routing_rules/capability/${id}`, {
    params: { revision },
  })
  return response.data
}
