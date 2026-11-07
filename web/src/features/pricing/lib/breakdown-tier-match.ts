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
import {
  normalizeTierLabel,
  type ParsedTaskTier,
  type ParsedTier,
} from './billing-expr'

type BreakdownMatchTier = ParsedTier | ParsedTaskTier

function tierMatchesNormalizedLabel(
  tier: BreakdownMatchTier,
  normalizedMatchedTierLabel: string
): boolean {
  return (
    normalizedMatchedTierLabel !== '' &&
    normalizeTierLabel(tier.label) === normalizedMatchedTierLabel
  )
}

function tokenTierMatchesFacts(
  tier: ParsedTier,
  usageFacts: Record<string, string | number>
): boolean {
  return (
    tier.conditions.length > 0 &&
    tier.conditions.every((condition) => {
      const rawValue = usageFacts[condition.var]
      const value = Number(rawValue)
      if (!Number.isFinite(value)) return false
      switch (condition.op) {
        case '<':
          return value < condition.value
        case '<=':
          return value <= condition.value
        case '>':
          return value > condition.value
        case '>=':
          return value >= condition.value
        default:
          return false
      }
    })
  )
}

/**
 * Decide whether a price-table row is the settlement hit.
 * Label equality (after normalizeTierLabel) wins; usage-facts matching is
 * only a fallback when no display row still carries the engine label.
 */
export function isBreakdownTierMatched(
  tier: BreakdownMatchTier,
  tiers: readonly BreakdownMatchTier[],
  matchedTierLabel?: string | null,
  usageFacts?: Record<string, string | number>,
  billingUnit?: 'token' | 'request',
  fixedPrice?: number
): boolean {
  if (!('unitPrices' in tier) && billingUnit) {
    if ((tier.billingUnit ?? 'token') !== billingUnit) return false
    if (
      billingUnit === 'request' &&
      fixedPrice !== undefined &&
      tier.fixedPrice !== fixedPrice
    ) {
      return false
    }
  }
  const normalizedMatchedTierLabel = normalizeTierLabel(
    matchedTierLabel ?? undefined
  )
  if (
    !normalizedMatchedTierLabel &&
    usageFacts &&
    !('unitPrices' in tier) &&
    tiers.every(
      (candidate) =>
        !('unitPrices' in candidate) && candidate.conditions.length > 0
    )
  ) {
    const matchedTiers = tiers.filter(
      (candidate) =>
        !('unitPrices' in candidate) &&
        tokenTierMatchesFacts(candidate, usageFacts)
    )
    return matchedTiers.length === 1 && matchedTiers[0] === tier
  }
  if (tierMatchesNormalizedLabel(tier, normalizedMatchedTierLabel)) {
    const sameLabelTiers = tiers.filter((candidate) =>
      tierMatchesNormalizedLabel(candidate, normalizedMatchedTierLabel) &&
      ('unitPrices' in candidate ||
        !billingUnit ||
        ((candidate.billingUnit ?? 'token') === billingUnit &&
          (billingUnit !== 'request' ||
            fixedPrice === undefined ||
            candidate.fixedPrice === fixedPrice)))
    )
    if (sameLabelTiers.length > 1) {
      // Expanded rows can share one engine label. Time-dependent rows cannot
      // be resolved from usage alone; never substitute the current wall clock.
      if (!usageFacts || tier.conditionText) return false
      if ('unitPrices' in tier) {
        return tier.conditions.length > 0 && tier.conditions.every(
          (condition) =>
            'field' in condition &&
            String(usageFacts[condition.field]) === condition.value
        )
      }
      return tokenTierMatchesFacts(tier, usageFacts)
    }
    return true
  }
  if (
    tiers.some((candidate) =>
      tierMatchesNormalizedLabel(candidate, normalizedMatchedTierLabel)
    )
  ) {
    return false
  }
  if (
    !usageFacts ||
    tier.conditions.length === 0 ||
    ('unitPrices' in tier && tier.conditionText)
  ) {
    return false
  }
  return tier.conditions.every((condition) => {
    if (!('field' in condition)) {
      return false
    }
    const fact = usageFacts[condition.field]
    if (fact === undefined) {
      return false
    }
    return String(fact) === condition.value
  })
}
