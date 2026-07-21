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

export type ModelPricingRuleSubjectType = 'user' | 'user_group'

export type ModelPricingRule = {
  id: number
  subject_type: ModelPricingRuleSubjectType
  subject_value: string
  model: string
  using_group: string
  ratio: number
  enabled: boolean
  created_at: number
  updated_at: number
}

export type ModelPricingRulePayload = Omit<
  ModelPricingRule,
  'id' | 'created_at' | 'updated_at'
>

export type ModelPricingRulesResponse = {
  success: boolean
  message: string
  data: ModelPricingRule[]
}

export type ModelPricingRuleResponse = {
  success: boolean
  message: string
  data: ModelPricingRule
}
