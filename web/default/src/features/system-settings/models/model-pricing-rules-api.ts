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
import { api } from '@/lib/api'
import type {
  ModelPricingRulePayload,
  ModelPricingRuleResponse,
  ModelPricingRulesResponse,
} from './model-pricing-rules-types'

const requestConfig = {
  skipBusinessError: true,
  skipErrorHandler: true,
}

export async function getModelPricingRules() {
  const res = await api.get<ModelPricingRulesResponse>(
    '/api/model-pricing-rules/',
    requestConfig
  )
  return res.data
}

export async function createModelPricingRule(payload: ModelPricingRulePayload) {
  const res = await api.post<ModelPricingRuleResponse>(
    '/api/model-pricing-rules/',
    payload,
    requestConfig
  )
  return res.data
}

export async function updateModelPricingRule(
  id: number,
  payload: ModelPricingRulePayload
) {
  const res = await api.put<ModelPricingRuleResponse>(
    `/api/model-pricing-rules/${id}`,
    payload,
    requestConfig
  )
  return res.data
}

export async function deleteModelPricingRule(id: number) {
  const res = await api.delete<ModelPricingRuleResponse>(
    `/api/model-pricing-rules/${id}`,
    requestConfig
  )
  return res.data
}
