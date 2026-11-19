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
import type { RequestPluginTokenUsage } from '../types'

/** Draft strings preserve empty and invalid fields until the administrator runs. */
export type RequestPluginUsageDraft = Record<
  keyof RequestPluginTokenUsage,
  string
>

/** The counters accepted by response usage hooks, in input/output display order. */
export const REQUEST_PLUGIN_USAGE_FIELDS = [
  { key: 'inputTokens', label: 'Total input tokens' },
  { key: 'outputTokens', label: 'Output Tokens' },
  { key: 'cacheReadTokens', label: 'Cache read tokens' },
  { key: 'cacheWriteTokens', label: 'Cache write tokens' },
  { key: 'cacheWrite1hTokens', label: 'Cache write tokens (1h)' },
  { key: 'cacheWrite12hTokens', label: 'Cache write tokens (12h)' },
] as const

/** A small sample with cached input and an uncached remainder to redistribute. */
export const REQUEST_PLUGIN_USAGE_SAMPLE: RequestPluginUsageDraft = {
  inputTokens: '100',
  outputTokens: '2000',
  cacheReadTokens: '89',
  cacheWriteTokens: '0',
  cacheWrite1hTokens: '0',
  cacheWrite12hTokens: '0',
}

/** Validates a dry-run sample against the host's counter and cache partition limits. */
export function parseRequestPluginUsage(
  draft: RequestPluginUsageDraft
):
  | { usage: RequestPluginTokenUsage; error?: never }
  | { usage?: never; error: string } {
  const usage: RequestPluginTokenUsage = {
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheWriteTokens: 0,
    cacheWrite1hTokens: 0,
    cacheWrite12hTokens: 0,
  }
  for (const { key } of REQUEST_PLUGIN_USAGE_FIELDS) {
    const value = Number(draft[key])
    if (
      !draft[key].trim() ||
      !Number.isInteger(value) ||
      value < 0 ||
      value > 2_147_483_647
    ) {
      return {
        error: 'Token counts must be whole numbers from 0 to 2147483647.',
      }
    }
    usage[key] = value
  }
  if (
    usage.cacheReadTokens + usage.cacheWriteTokens > usage.inputTokens ||
    usage.cacheWrite1hTokens + usage.cacheWrite12hTokens >
      usage.cacheWriteTokens
  ) {
    return {
      error:
        'Cache reads and writes must fit within total input; cache lifetime counts must fit within total writes.',
    }
  }
  return { usage }
}
