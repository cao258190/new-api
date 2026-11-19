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
import type { AnnouncementReminderConfig } from '../types'

/** Read optional reminder metadata from a published or legacy announcement. */
export function getAnnouncementReminder(
  value: unknown
): AnnouncementReminderConfig | undefined {
  if (!value || typeof value !== 'object') return undefined

  const reminder = value as Record<string, unknown>
  if (
    typeof reminder.id !== 'string' ||
    !reminder.id.trim() ||
    typeof reminder.title !== 'string' ||
    !reminder.title.trim()
  ) {
    return undefined
  }

  const maintenance = reminder.maintenance
  if (maintenance && typeof maintenance === 'object') {
    const fields = maintenance as Record<string, unknown>
    if (
      typeof fields.window === 'string' &&
      fields.window.trim() &&
      typeof fields.impact === 'string' &&
      fields.impact.trim()
    ) {
      return {
        id: reminder.id,
        title: reminder.title,
        maintenance: { window: fields.window, impact: fields.impact },
      }
    }
  }

  return { id: reminder.id, title: reminder.title }
}
