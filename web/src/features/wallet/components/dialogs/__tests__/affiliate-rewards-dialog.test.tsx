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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import type { AffiliateReward } from '../../../types'
import { AffiliateRewardsDialog } from '../affiliate-rewards-dialog'

const settledReward: AffiliateReward = {
  id: 1,
  inviter_id: 3,
  invitee_id: 7,
  invitee_name: 'A***',
  source_type: 'topup',
  source_id: 20,
  rate_bps: 1250,
  base_quota: 1000,
  reward_quota: 125,
  credited_quota: 100,
  pending_quota: 25,
  created_at: 1793960000,
}

function renderRewards() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <AffiliateRewardsDialog open onOpenChange={() => undefined} />
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.restoreAllMocks()
})

it('shows the saved rate and wallet split, then loads the next page', async () => {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    const page = new URL(url, 'http://localhost').searchParams.get('p')
    return {
      data: {
        success: true,
        data: {
          items:
            page === '1'
              ? Array.from({ length: 10 }, (_, index) => ({
                  ...settledReward,
                  id: index + 1,
                }))
              : [{ ...settledReward, id: 11, source_type: 'redemption' }],
          total: 11,
          page: Number(page),
          page_size: 10,
        },
      },
    }
  })

  renderRewards()
  expect(await screen.findAllByText('12.5%')).toHaveLength(10)
  expect(screen.getAllByText('A*** · #7')).toHaveLength(10)
  expect(screen.getAllByText('Credited to balance')).toHaveLength(10)
  expect(screen.getAllByText('Pending transfer')).toHaveLength(10)

  await userEvent.click(screen.getByRole('button', { name: 'Go to next page' }))
  await waitFor(() => {
    expect(get).toHaveBeenCalledWith('/api/user/aff/rewards?p=2&page_size=10')
  })
  expect(
    await screen.findByRole('heading', { name: 'Quota code redemption' })
  ).toBeInTheDocument()
  expect(screen.getAllByText('A*** · #7')).toHaveLength(1)
})

it('shows a retry action when loading fails and an empty state after retry', async () => {
  const get = vi
    .spyOn(api, 'get')
    .mockResolvedValueOnce({ data: { success: false, message: 'failed' } })
    .mockResolvedValue({
      data: {
        success: true,
        data: { items: [], total: 0, page: 1, page_size: 10 },
      },
    })

  renderRewards()
  expect(
    await screen.findByText('Failed to load rebate details')
  ).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
  expect(await screen.findByText('No rebate details yet')).toBeInTheDocument()
  expect(get).toHaveBeenCalledTimes(2)
})
