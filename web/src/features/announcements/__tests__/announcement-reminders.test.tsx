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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { NotificationPopover } from '@/components/notification-popover'
import { updateSystemOption } from '@/features/system-settings/api'
import { AnnouncementsSection } from '@/features/system-settings/content/announcements-section'
import { useNotifications } from '@/hooks/use-notifications'
import { getNotice, getStatus } from '@/lib/api'
import { STATUS_QUERY_KEY } from '@/lib/status-query'
import { useAuthStore } from '@/stores/auth-store'
import { useNotificationStore } from '@/stores/notification-store'

import { AnnouncementReminderDialog } from '../components/announcement-reminder-dialog'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/api')>()),
  getNotice: vi.fn(),
  getStatus: vi.fn(),
}))

vi.mock('@/features/system-settings/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/system-settings/api')>()),
  updateSystemOption: vi.fn(),
}))

const routine = {
  content: 'A regular update',
  publishDate: '2026-01-01T00:00:00Z',
}
const maintenance = {
  id: 2,
  content: 'Save your work before we start.',
  publishDate: '2026-01-02T00:00:00Z',
  reminder: {
    id: 'maintenance',
    title: 'Console maintenance tonight',
    maintenance: { window: '23:00 to 00:30', impact: 'API requests continue' },
  },
}
const billingNotice = {
  id: 3,
  content: 'The next invoice lists usage by model.',
  publishDate: '2026-01-03T00:00:00Z',
  reminder: { id: 'invoice', title: 'Invoices now include model usage' },
}
const releaseNotice = {
  id: 4,
  content: 'The export is now ready.',
  publishDate: '2026-01-04T00:00:00Z',
  reminder: { id: 'export', title: 'Usage export is ready' },
}

/** Mount the notification center with the same actions as the signed-in header. */
function ConsoleNotifications() {
  const notifications = useNotifications({ showAnnouncementReminders: true })

  return (
    <>
      <NotificationPopover
        open={notifications.popoverOpen}
        onOpenChange={notifications.setPopoverOpen}
        unreadCount={notifications.unreadCount}
        activeTab={notifications.activeTab}
        onTabChange={notifications.setActiveTab}
        notice={notifications.notice}
        announcements={notifications.announcements}
        loading={notifications.loading}
        onShowImportantAnnouncements={
          notifications.pendingReminderCount > 0
            ? notifications.openAnnouncementReminders
            : undefined
        }
      />
      {notifications.reminderBatch ? (
        <AnnouncementReminderDialog
          announcements={notifications.reminderBatch}
          onAcknowledge={notifications.acknowledgeAnnouncementReminders}
          onSnooze={notifications.snoozeAnnouncementReminders}
        />
      ) : null}
    </>
  )
}

function signInAs(id: number) {
  useAuthStore.setState((state) => ({
    auth: { ...state.auth, user: { id, username: 'tester', role: 1 } },
  }))
}

function renderConsole() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  const view = render(
    <QueryClientProvider client={client}>
      <ConsoleNotifications />
    </QueryClientProvider>
  )
  return { ...view, client }
}

describe('strong announcement reminders', () => {
  const getAnimations = Object.getOwnPropertyDescriptor(
    Element.prototype,
    'getAnimations'
  )
  beforeEach(() => {
    // jsdom does not provide the Web Animations API used by the scroll area.
    if (!getAnimations) {
      Object.defineProperty(Element.prototype, 'getAnimations', {
        configurable: true,
        value: () => [],
      })
    }
    localStorage.clear()
    useNotificationStore.setState({
      lastReadNotice: '',
      readAnnouncementKeys: [],
      announcementReminders: {},
    })
    signInAs(1)
    vi.mocked(getNotice).mockResolvedValue({ success: true, data: '' })
  })

  afterEach(() => {
    useAuthStore.getState().auth.reset()
    vi.useRealTimers()
    if (!getAnimations) {
      Reflect.deleteProperty(Element.prototype, 'getAnimations')
    }
    localStorage.clear()
  })

  it('saves a maintenance reminder only after its required fields are filled in', async () => {
    vi.mocked(updateSystemOption).mockResolvedValue({
      success: true,
      message: '',
    })
    const user = userEvent.setup()
    const client = new QueryClient()
    render(
      <QueryClientProvider client={client}>
        <AnnouncementsSection enabled data='[]' />
      </QueryClientProvider>
    )

    await user.click(screen.getByRole('button', { name: 'Add Announcement' }))
    const form = within(
      await screen.findByRole('dialog', { name: 'Add Announcement' })
    )
    const reminder = form.getByRole('switch', { name: 'Strong reminder' })
    expect(reminder).not.toBeChecked()
    await user.click(reminder)
    await user.click(form.getByRole('switch', { name: 'Planned maintenance' }))
    await user.type(
      form.getByRole('textbox', { name: 'Content' }),
      maintenance.content
    )
    await user.click(form.getByRole('button', { name: 'Add' }))
    expect(
      form.getByRole('textbox', { name: 'Announcement title' })
    ).toBeInvalid()

    await user.type(
      form.getByRole('textbox', { name: 'Announcement title' }),
      maintenance.reminder.title
    )
    await user.type(
      form.getByRole('textbox', { name: 'Maintenance window' }),
      maintenance.reminder.maintenance.window
    )
    await user.type(
      form.getByRole('textbox', { name: 'Impact scope' }),
      maintenance.reminder.maintenance.impact
    )
    await user.click(form.getByRole('button', { name: 'Add' }))
    await user.click(
      await screen.findByRole('button', { name: 'Save Settings' })
    )

    await waitFor(() => expect(updateSystemOption).toHaveBeenCalledOnce())
    const request = vi.mocked(updateSystemOption).mock.calls[0][0]
    expect(request.key).toBe('console_setting.announcements')
    expect(JSON.parse(String(request.value))).toMatchObject([
      {
        content: maintenance.content,
        reminder: { ...maintenance.reminder, id: expect.any(String) },
      },
    ])
  })

  it('keeps an older announcement in the timeline without opening a reminder', async () => {
    vi.mocked(getStatus).mockResolvedValue({
      announcements_enabled: true,
      announcements: [routine],
    })
    const user = userEvent.setup()
    renderConsole()
    const bell = screen.getByRole('button', { name: 'Notifications' })

    await waitFor(() => expect(bell).toHaveTextContent('1'))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    await user.click(bell)
    await user.click(screen.getByRole('tab', { name: 'Timeline' }))
    expect(await screen.findByText(routine.content)).toBeVisible()
    expect(bell).not.toHaveTextContent('1')
  })

  it('keeps a snoozed maintenance reminder unread and offers it again after the local day changes', async () => {
    vi.setSystemTime(new Date(2026, 10, 20, 12))
    vi.mocked(getStatus).mockResolvedValue({
      announcements_enabled: true,
      announcements: [maintenance],
    })
    const user = userEvent.setup()
    const firstVisit = renderConsole()
    let dialog = await screen.findByRole('dialog', {
      name: /Console maintenance tonight/,
    })
    expect(within(dialog).getByText('23:00 to 00:30')).toBeVisible()
    expect(within(dialog).getByText('API requests continue')).toBeVisible()

    await user.click(
      within(dialog).getByRole('button', { name: 'Remind me later' })
    )
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    firstVisit.unmount()

    const revisit = renderConsole()
    const bell = screen.getByRole('button', { name: 'Notifications' })
    await waitFor(() => expect(bell).toHaveTextContent('1'))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    await user.click(bell)
    expect(bell).toHaveTextContent('1')
    await user.click(
      screen.getByRole('button', { name: 'View important announcements' })
    )
    dialog = await screen.findByRole('dialog', {
      name: /Console maintenance tonight/,
    })
    expect(within(dialog).getByRole('button', { name: 'Got it' })).toBeVisible()
    await user.keyboard('{Escape}')
    expect(
      screen.queryByRole('dialog', { name: /Console maintenance tonight/ })
    ).not.toBeInTheDocument()
    expect(bell).toHaveTextContent('1')
    revisit.unmount()

    vi.setSystemTime(new Date(2026, 10, 21, 12))
    renderConsole()
    expect(
      await screen.findByRole('dialog', { name: /Console maintenance tonight/ })
    ).toBeVisible()
  })

  it('confirms only the displayed batch for the current account', async () => {
    vi.mocked(getStatus).mockResolvedValue({
      announcements_enabled: true,
      announcements: [billingNotice, maintenance],
    })
    const user = userEvent.setup()
    const { client } = renderConsole()
    const batch = await screen.findByRole('dialog', {
      name: /2 announcements to confirm/,
    })
    expect(within(batch).getByText(billingNotice.reminder.title)).toBeVisible()
    expect(within(batch).getByText(maintenance.reminder.title)).toBeVisible()

    act(() => {
      client.setQueryData(STATUS_QUERY_KEY, {
        announcements_enabled: true,
        announcements: [releaseNotice, billingNotice, maintenance],
      })
    })
    expect(
      within(batch).queryByText(releaseNotice.reminder.title)
    ).not.toBeInTheDocument()
    await user.click(
      within(batch).getByRole('button', { name: 'Confirm 2 announcements' })
    )

    const next = await screen.findByRole('dialog', {
      name: /Usage export is ready/,
    })
    await user.click(within(next).getByRole('button', { name: 'Got it' }))
    const bell = screen.getByRole('button', { name: 'Notifications' })
    expect(bell).not.toHaveTextContent(/\d/)

    act(() => signInAs(2))
    expect(
      await screen.findByRole('dialog', { name: /3 announcements to confirm/ })
    ).toBeVisible()
  })
})
