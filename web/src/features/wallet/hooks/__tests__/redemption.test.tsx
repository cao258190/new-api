/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { act, renderHook } from '@testing-library/react'
import i18next from 'i18next'
import { toast } from 'sonner'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { useRedemption } from '../use-redemption'

afterEach(() => {
  vi.restoreAllMocks()
})

test.each([
  {
    name: 'quota',
    data: 500000,
    expectedMessage: 'Redemption successful! Added:',
  },
  {
    name: 'subscription',
    data: { type: 'subscription', plan_id: 7, plan_title: 'Pro plan' },
    expectedMessage: 'Subscription redeemed successfully: Pro plan',
  },
])(
  'redeeming a $name code reports the entitlement and refreshes the user',
  async ({ data, expectedMessage }) => {
    await i18next.init({ lng: 'en', resources: { en: { translation: {} } } })
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ data: { success: true, data } })
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ data: { success: true, data: {} } })
    const successToast = vi
      .spyOn(toast, 'success')
      .mockImplementation(() => 'toast-id')
    const { result } = renderHook(() => useRedemption())

    let redeemed = false
    await act(async () => {
      redeemed = await result.current.redeemCode('sample-code')
    })

    expect(redeemed).toBe(true)
    expect(post).toHaveBeenCalledWith('/api/user/topup', { key: 'sample-code' })
    expect(get).toHaveBeenCalledWith('/api/user/self', {
      skipErrorHandler: true,
    })
    expect(successToast).toHaveBeenCalledWith(
      expect.stringContaining(expectedMessage)
    )
  }
)
