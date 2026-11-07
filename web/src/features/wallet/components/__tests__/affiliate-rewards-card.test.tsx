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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'

import { AffiliateRewardsCard } from '../affiliate-rewards-card'

it('opens rebate details at the effective rate even when no pending quota exists', async () => {
  const onViewRewards = vi.fn()
  render(
    <AffiliateRewardsCard
      user={{
        id: 3,
        username: 'inviter',
        quota: 0,
        used_quota: 0,
        request_count: 0,
        aff_quota: 0,
        aff_history_quota: 100,
        aff_count: 1,
        group: 'default',
      }}
      affiliateLink='https://example.com/register?aff=test'
      rewardRate={1250}
      onTransfer={vi.fn()}
      onViewRewards={onViewRewards}
    />
  )

  expect(screen.getByText(/12\.5% back/)).toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Transfer to Balance' })
  ).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Rebate details' }))
  expect(onViewRewards).toHaveBeenCalledOnce()
})
