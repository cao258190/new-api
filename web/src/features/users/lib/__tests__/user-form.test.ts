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
import { expect, it } from 'vitest'

import {
  transformUserToFormDefaults,
  USER_FORM_DEFAULT_VALUES,
  userFormSchema,
} from '../user-form'

it.each(['', '0', '12.5', '100'])(
  'accepts a personal rebate rate of %s',
  (rate) => {
    expect(
      userFormSchema.safeParse({
        ...USER_FORM_DEFAULT_VALUES,
        username: 'inviter',
        aff_reward_rate: rate,
      }).success
    ).toBe(true)
  }
)

it.each(['-1', '12.345', '100.01', 'abc'])(
  'rejects an invalid personal rebate rate of %s',
  (rate) => {
    expect(
      userFormSchema.safeParse({
        ...USER_FORM_DEFAULT_VALUES,
        username: 'inviter',
        aff_reward_rate: rate,
      }).success
    ).toBe(false)
  }
)

it('shows the global fallback as empty and converts basis points to percent', () => {
  const user = {
    id: 4,
    username: 'inviter',
    display_name: 'Inviter',
    role: 1,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    group: 'default',
  }
  expect(
    transformUserToFormDefaults({ ...user, aff_reward_rate: null })
      .aff_reward_rate
  ).toBe('')
  expect(
    transformUserToFormDefaults({ ...user, aff_reward_rate: 1250 })
      .aff_reward_rate
  ).toBe('12.5')
})
