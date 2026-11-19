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
import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'

import { getAnnouncementReminder } from '@/features/announcements/lib/get-announcement-reminder'
import type { ReminderAnnouncement } from '@/features/announcements/types'
import { useStatus } from '@/hooks/use-status'
import { getNotice } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'
import { useNotificationStore } from '@/stores/notification-store'

function hashString(input: string): string {
  let hash = 0
  if (!input) return '0'

  for (let i = 0; i < input.length; i += 1) {
    const chr = input.charCodeAt(i)
    hash = (hash << 5) - hash + chr
    hash |= 0
  }
  return hash.toString(36)
}

/** Identify a timeline entry, using a fingerprint for legacy entries without an ID. */
function getAnnouncementKey(item: Record<string, unknown>): string {
  if (item.id !== undefined && item.id !== null) {
    return `id:${item.id}`
  }

  const fingerprint = JSON.stringify({
    publishDate: item.publishDate || '',
    content: typeof item.content === 'string' ? item.content.trim() : '',
    extra: typeof item.extra === 'string' ? item.extra.trim() : '',
    type: item.type || '',
    title: typeof item.title === 'string' ? item.title.trim() : '',
    link: typeof item.link === 'string' ? item.link.trim() : '',
  })
  return `hash:${hashString(fingerprint)}`
}

type NotificationTab = 'notice' | 'announcements'
type ReminderBatch = { userId: number; announcements: ReminderAnnouncement[] }

/** Manage the notification center and prompt signed-in console users to confirm important announcements. */
export function useNotifications(options?: {
  showAnnouncementReminders?: boolean
}) {
  const userId = useAuthStore((s) =>
    options?.showAnnouncementReminders ? s.auth.user?.id : undefined
  )
  const [popoverOpen, setPopoverOpen] = useState(false)
  const [activeTab, setActiveTab] = useState<NotificationTab>('notice')
  const [batch, setBatch] = useState<ReminderBatch | null>(null)
  const reminderBatch =
    batch && batch.userId === userId ? batch.announcements : null

  const {
    data: noticeResponse,
    isLoading: noticeLoading,
    refetch: refetchNotice,
  } = useQuery({
    queryKey: ['notice'],
    queryFn: async () => requireServerSuccess(await getNotice()),
    staleTime: 1000 * 60 * 5,
  })

  const {
    status,
    loading: statusLoading,
    isFetching,
    isPlaceholderData,
    error,
  } = useStatus(userId === undefined ? undefined : { refetchOnMount: 'always' })
  const announcementsEnabled = status?.announcements_enabled ?? false
  const published = useMemo(() => {
    if (!announcementsEnabled || !Array.isArray(status?.announcements)) {
      return []
    }
    return (status.announcements as Record<string, unknown>[]).filter(
      (item) => item !== null && typeof item === 'object'
    )
  }, [announcementsEnabled, status?.announcements])

  const lastReadNotice = useNotificationStore((s) => s.lastReadNotice)
  const readAnnouncementKeys = useNotificationStore(
    (s) => s.readAnnouncementKeys
  )
  const markNoticeRead = useNotificationStore((s) => s.markNoticeRead)
  const markAnnouncementsRead = useNotificationStore(
    (s) => s.markAnnouncementsRead
  )
  const setAnnouncementReminders = useNotificationStore(
    (s) => s.setAnnouncementReminders
  )
  const reminderStates = useNotificationStore((s) =>
    userId === undefined ? undefined : s.announcementReminders[userId]
  )

  const reminderIndex = useMemo(() => {
    const byId = new Map<string, ReminderAnnouncement>()
    if (userId === undefined) return byId

    for (const announcement of published) {
      const reminder = getAnnouncementReminder(announcement.reminder)
      if (
        !reminder ||
        typeof announcement.content !== 'string' ||
        byId.has(reminder.id)
      ) {
        continue
      }

      byId.set(reminder.id, {
        ...reminder,
        announcementKey: getAnnouncementKey(announcement),
        content: announcement.content,
        extra:
          typeof announcement.extra === 'string'
            ? announcement.extra
            : undefined,
      })
    }
    return byId
  }, [published, userId])
  const strongKeys = new Set(
    [...reminderIndex.values()].map((item) => item.announcementKey)
  )
  const announcements = published.slice(0, 20)
  const regularKeys = announcements
    .map(getAnnouncementKey)
    .filter((key) => !strongKeys.has(key))
  const pendingReminders = useMemo(
    () =>
      [...reminderIndex.values()].filter(
        (item) => reminderStates?.[item.id] !== true
      ),
    [reminderIndex, reminderStates]
  )

  useEffect(() => {
    if (
      userId === undefined ||
      popoverOpen ||
      reminderBatch ||
      isFetching ||
      isPlaceholderData ||
      error
    ) {
      return
    }

    // Freeze the displayed batch so a later update cannot be acknowledged unseen.
    const today = new Date().toDateString()
    const due = pendingReminders.filter(
      (item) => reminderStates?.[item.id] !== today
    )
    if (due.length > 0) setBatch({ userId, announcements: due })
  }, [
    userId,
    popoverOpen,
    reminderBatch,
    isFetching,
    isPlaceholderData,
    error,
    pendingReminders,
    reminderStates,
  ])

  const noticeContent = noticeResponse?.success
    ? (noticeResponse.data || '').trim()
    : ''
  const unreadNoticeCount =
    noticeContent && noticeContent !== lastReadNotice ? 1 : 0
  const unreadAnnouncementsCount =
    regularKeys.filter((key) => !readAnnouncementKeys.includes(key)).length +
    pendingReminders.length

  const handleTabChange = (tab: NotificationTab) => {
    setActiveTab(tab)
    if (tab === 'announcements' && regularKeys.length > 0) {
      markAnnouncementsRead(regularKeys)
    }
  }

  const handleOpenPopover = (tab?: NotificationTab) => {
    const nextTab =
      tab ?? (pendingReminders.length > 0 ? 'announcements' : activeTab)

    if (noticeContent) markNoticeRead(noticeContent)
    handleTabChange(nextTab)
    setPopoverOpen(true)
  }

  const handlePopoverOpenChange = (open: boolean) => {
    if (open) {
      handleOpenPopover()
      return
    }

    setPopoverOpen(false)
  }

  const openAnnouncementReminders = () => {
    if (
      userId === undefined ||
      pendingReminders.length === 0 ||
      isPlaceholderData
    ) {
      return
    }

    setPopoverOpen(false)
    setBatch({ userId, announcements: pendingReminders })
  }

  const finishAnnouncementReminders = (acknowledged: boolean) => {
    if (userId === undefined || !reminderBatch) return

    setAnnouncementReminders(
      userId,
      reminderBatch.map((item) => item.id),
      acknowledged ? true : new Date().toDateString()
    )
    setBatch(null)
  }

  return {
    notice: noticeContent,
    announcements,
    loading: noticeLoading || statusLoading,
    unreadCount: unreadNoticeCount + unreadAnnouncementsCount,
    unreadNoticeCount,
    unreadAnnouncementsCount,
    popoverOpen,
    setPopoverOpen: handlePopoverOpenChange,
    activeTab,
    setActiveTab: handleTabChange,
    openPopover: handleOpenPopover,
    closePopover: () => setPopoverOpen(false),
    refetchNotice,
    pendingReminderCount: pendingReminders.length,
    reminderBatch,
    openAnnouncementReminders,
    acknowledgeAnnouncementReminders: () => finishAnnouncementReminders(true),
    snoozeAnnouncementReminders: () => finishAnnouncementReminders(false),
  }
}
