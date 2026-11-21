/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createRef } from 'react'
import { expect, test, vi } from 'vitest'

import { ImageStudioSessionMenu } from '../session-menu'

const sessions = [
  { id: 'a', title: 'Red kite', createdAt: 1, updatedAt: 1, messages: [] },
  { id: 'b', title: 'Blue sea', createdAt: 2, updatedAt: 2, messages: [] },
]

/** Render the menu with its active session without opening it. */
function renderSessionMenu(disabled = false) {
  const onSelect = vi.fn()
  const onRename = vi.fn()
  render(
    <ImageStudioSessionMenu
      disabled={disabled}
      onCreate={vi.fn()}
      onDelete={vi.fn()}
      onRename={onRename}
      onSelect={onSelect}
      selectedId='a'
      sessions={sessions}
      triggerRef={createRef<HTMLButtonElement>()}
    />
  )
  return {
    onSelect,
    onRename,
    trigger: screen.getByRole('button', { name: 'Red kite' }),
  }
}

test('the session list opens from the current title and closes after switching', async () => {
  const user = userEvent.setup()
  const { onSelect, trigger } = renderSessionMenu()

  expect(
    screen.queryByRole('navigation', { name: 'Image sessions' })
  ).not.toBeInTheDocument()
  await user.tab()
  expect(trigger).toHaveFocus()
  await user.keyboard('{Enter}')
  expect(trigger).toHaveAttribute('aria-expanded', 'true')
  const list = within(
    screen.getByRole('navigation', { name: 'Image sessions' })
  )
  expect(list.getByRole('button', { name: 'Red kite' })).toHaveAttribute(
    'aria-current',
    'true'
  )
  await user.click(list.getByRole('button', { name: 'Blue sea' }))
  expect(onSelect).toHaveBeenCalledWith(sessions[1])
  expect(trigger).toHaveAttribute('aria-expanded', 'false')
})

test('renaming rejects a blank title and returns focus to the menu after saving', async () => {
  const user = userEvent.setup()
  const { onRename, trigger } = renderSessionMenu()

  await user.click(trigger)
  const list = within(
    screen.getByRole('navigation', { name: 'Image sessions' })
  )
  await user.click(list.getAllByRole('button', { name: 'Rename session' })[0])
  const dialog = within(screen.getByRole('dialog', { name: 'Rename session' }))
  const field = dialog.getByRole('textbox', { name: 'Name' })
  await waitFor(() => expect(field).toHaveFocus())
  await user.clear(field)
  expect(dialog.getByRole('button', { name: 'Save' })).toBeDisabled()
  await user.type(field, '  Evening flight  {Enter}')
  expect(onRename).toHaveBeenCalledWith('a', 'Evening flight')
  await waitFor(() => expect(trigger).toHaveFocus())
})

test('session actions cannot be opened while generation is in progress', async () => {
  const user = userEvent.setup()
  const { trigger } = renderSessionMenu(true)

  expect(trigger).toBeDisabled()
  await user.click(trigger)
  expect(
    screen.queryByRole('navigation', { name: 'Image sessions' })
  ).not.toBeInTheDocument()
})
