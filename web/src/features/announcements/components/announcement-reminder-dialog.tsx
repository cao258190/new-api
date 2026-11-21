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
import { CircleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { RichContent } from '@/components/rich-content'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { ReminderAnnouncement } from '../types'

type AnnouncementReminderDialogProps = {
  announcements: ReminderAnnouncement[]
  onAcknowledge: () => void
  onSnooze: () => void
}

/** Show the current batch of reminders with a single acknowledgment action. */
export function AnnouncementReminderDialog(
  props: AnnouncementReminderDialogProps
) {
  const { t, i18n } = useTranslation()
  if (props.announcements.length === 0) return null

  const multiple = props.announcements.length > 1
  const first = props.announcements[0]
  const maintenance = !multiple && Boolean(first.maintenance)
  const displayCount = formatNumber(
    props.announcements.length,
    toIntlLocale(i18n.resolvedLanguage || i18n.language)
  )

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onSnooze()
      }}
      disablePointerDismissal
      showCloseButton={false}
      contentClassName='sm:max-w-lg'
      headerClassName={cn(
        maintenance &&
          'bg-warning/12 -mx-4 -mt-4 px-4 py-4 sm:-mx-6 sm:-mt-6 sm:px-6 sm:py-5'
      )}
      title={
        <span className='flex flex-col gap-2 text-xl break-words'>
          <span className='text-muted-foreground flex items-center gap-2 text-sm font-semibold'>
            <span
              className={cn(
                'bg-primary/12 text-primary grid size-7 place-items-center rounded-lg',
                maintenance && 'bg-warning/20 text-warning'
              )}
            >
              <CircleAlert aria-hidden='true' className='size-4' />
            </span>
            {maintenance
              ? t('Planned maintenance')
              : t('Important announcements')}
          </span>
          {multiple
            ? t('{{displayCount}} announcements to confirm', { displayCount })
            : first.title}
        </span>
      }
      bodyClassName='space-y-3'
      footerClassName='border-border/70 border-t pt-4'
      footer={
        <>
          <Button variant='outline' onClick={props.onSnooze}>
            {t('Remind me later')}
          </Button>
          <Button onClick={props.onAcknowledge}>
            {multiple
              ? t('Confirm {{displayCount}} announcements', { displayCount })
              : t('Got it')}
          </Button>
        </>
      }
    >
      {props.announcements.map((announcement) => (
        <article
          key={announcement.id}
          className={cn(
            'flex min-w-0 flex-col gap-2.5 text-sm break-words',
            multiple && 'bg-muted/50 rounded-lg border p-3'
          )}
        >
          {multiple ? (
            <h3 className='text-foreground font-semibold'>
              {announcement.title}
            </h3>
          ) : null}
          {announcement.maintenance ? (
            <dl className='divide-border/70 grid divide-y'>
              <div className='grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)] gap-2 py-2'>
                <dt className='text-muted-foreground'>
                  {t('Maintenance window')}
                </dt>
                <dd className='font-semibold'>
                  {announcement.maintenance.window}
                </dd>
              </div>
              <div className='grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)] gap-2 py-2'>
                <dt className='text-muted-foreground'>{t('Impact scope')}</dt>
                <dd className='font-semibold'>
                  {announcement.maintenance.impact}
                </dd>
              </div>
            </dl>
          ) : null}
          <RichContent breaks content={announcement.content} />
          {announcement.extra ? (
            <RichContent
              breaks
              content={announcement.extra}
              className='text-muted-foreground'
            />
          ) : null}
        </article>
      ))}
    </Dialog>
  )
}
