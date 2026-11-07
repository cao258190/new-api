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
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import {
  Pagination,
  PaginationContent,
  PaginationItem,
} from '@/components/ui/pagination'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatQuota, formatTimestampToDate } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'

import { getAffiliateRewards } from '../../api'
import type { AffiliateReward } from '../../types'

type AffiliateRewardsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

const PAGE_SIZE = 10

/** Display settled rebates for the current inviter with source filtering. */
export function AffiliateRewardsDialog(props: AffiliateRewardsDialogProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [page, setPage] = useState(1)
  const [sourceType, setSourceType] = useState<'' | 'topup' | 'redemption'>('')
  const rewards = useQuery({
    queryKey: ['affiliate-rewards', page, sourceType],
    queryFn: async () =>
      requireServerSuccess(
        await getAffiliateRewards(page, PAGE_SIZE, sourceType)
      ).data,
    enabled: props.open,
  })
  const items = rewards.data?.items ?? []
  const total = rewards.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Rebate details')}
      description={t(
        'Each rebate is calculated when the invited user receives wallet quota. Historical rates and amounts do not change.'
      )}
      contentClassName='flex max-h-(--dialog-available-height) flex-col max-sm:w-screen max-sm:max-w-none max-sm:rounded-none sm:max-w-3xl'
    >
      <div className='space-y-4'>
        <Select
          items={[
            { value: 'all', label: t('All sources') },
            { value: 'topup', label: t('Online top-up') },
            { value: 'redemption', label: t('Quota code redemption') },
          ]}
          value={sourceType || 'all'}
          onValueChange={(value) => {
            if (
              value === 'all' ||
              value === 'topup' ||
              value === 'redemption'
            ) {
              setSourceType(value === 'all' ? '' : value)
              setPage(1)
            }
          }}
        >
          <SelectTrigger aria-label={t('Source')} className='w-full sm:w-56'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              <SelectItem value='all'>{t('All sources')}</SelectItem>
              <SelectItem value='topup'>{t('Online top-up')}</SelectItem>
              <SelectItem value='redemption'>
                {t('Quota code redemption')}
              </SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>

        {rewards.isPending && (
          <LoadingState message={t('Loading rebate details...')} />
        )}
        {rewards.isError && (
          <ErrorState
            title={t('Failed to load rebate details')}
            onRetry={() => void rewards.refetch()}
          />
        )}
        {rewards.isSuccess && items.length === 0 && (
          <EmptyState
            title={t('No rebate details yet')}
            description={t(
              'Rebates from eligible wallet top-ups and quota codes will appear here.'
            )}
            className='min-h-40'
          />
        )}
        {rewards.isSuccess && items.length > 0 && (
          <div className='space-y-3'>
            {items.map((reward: AffiliateReward) => (
              <article key={reward.id} className='rounded-lg border p-3 sm:p-4'>
                <div className='flex flex-wrap items-start justify-between gap-2'>
                  <div>
                    <h3 className='text-sm font-semibold'>
                      {reward.source_type === 'topup'
                        ? t('Online top-up')
                        : t('Quota code redemption')}
                    </h3>
                    <p className='text-muted-foreground text-xs'>
                      {reward.invitee_name
                        ? `${reward.invitee_name} · #${formatNumber(reward.invitee_id, locale)}`
                        : t('User #{{id}}', { id: reward.invitee_id })}
                    </p>
                  </div>
                  <time
                    className='text-muted-foreground text-xs'
                    dateTime={new Date(reward.created_at * 1000).toISOString()}
                  >
                    {formatTimestampToDate(reward.created_at)}
                  </time>
                </div>
                <dl className='mt-3 grid grid-cols-2 gap-3 text-sm sm:grid-cols-5'>
                  {[
                    [t('Received quota'), formatQuota(reward.base_quota)],
                    [
                      t('Applied rate'),
                      `${formatNumber(reward.rate_bps / 100, locale)}%`,
                    ],
                    [t('Total rebate'), formatQuota(reward.reward_quota)],
                    [
                      t('Credited to balance'),
                      formatQuota(reward.credited_quota),
                    ],
                    [t('Pending transfer'), formatQuota(reward.pending_quota)],
                  ].map(([label, value]) => (
                    <div key={label}>
                      <dt className='text-muted-foreground text-xs'>{label}</dt>
                      <dd className='mt-1 font-medium tabular-nums'>{value}</dd>
                    </div>
                  ))}
                </dl>
              </article>
            ))}
          </div>
        )}

        {!rewards.isPending && !rewards.isError && total > 0 && (
          <Pagination className='items-center justify-between gap-3'>
            <span className='text-muted-foreground text-xs'>
              {t('Showing')} {formatNumber((page - 1) * PAGE_SIZE + 1, locale)}–
              {formatNumber(Math.min(page * PAGE_SIZE, total), locale)}{' '}
              {t('of')} {formatNumber(total, locale)}
            </span>
            <PaginationContent>
              <PaginationItem>
                <Button
                  variant='outline'
                  size='icon'
                  aria-label={t('Go to previous page')}
                  disabled={page <= 1}
                  onClick={() => setPage((value) => value - 1)}
                >
                  <ChevronLeft aria-hidden='true' />
                </Button>
              </PaginationItem>
              <PaginationItem>
                <span className='px-2 text-sm tabular-nums' aria-live='polite'>
                  {formatNumber(page, locale)} /{' '}
                  {formatNumber(pageCount, locale)}
                </span>
              </PaginationItem>
              <PaginationItem>
                <Button
                  variant='outline'
                  size='icon'
                  aria-label={t('Go to next page')}
                  disabled={page >= pageCount}
                  onClick={() => setPage((value) => value + 1)}
                >
                  <ChevronRight aria-hidden='true' />
                </Button>
              </PaginationItem>
            </PaginationContent>
          </Pagination>
        )}
      </div>
    </Dialog>
  )
}
