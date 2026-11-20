/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { describe, expect, test, vi } from 'vitest'

import zh from '@/i18n/locales/zh.json'

import type { ImageStudioMessage } from '../../types'
import { ImageHistory } from '../image-history'

const failedMessage: ImageStudioMessage = {
  id: 'attempt-1',
  prompt: 'A red kite',
  mode: 'generate',
  params: {
    model: 'gpt-image-1',
    group: 'default',
    prompt: 'A red kite',
    n: 1,
    size: '1024x1024',
    quality: 'auto',
    background: '',
    output_format: 'png',
    output_compression: '',
    moderation: '',
    input_fidelity: '',
    watermark: false,
  },
  createdAt: 1,
  status: 'error',
  error: 'Request failed',
  results: [],
}

const completedMessage: ImageStudioMessage = {
  ...failedMessage,
  status: 'complete',
  error: undefined,
  results: [
    {
      assetId: 'generated-1',
      url: 'https://images.example/generated.png',
      name: 'generated.png',
    },
  ],
}

describe('image history', () => {
  test.each([
    ['Daily free image quota is insufficient', '今日免费生图额度不足'],
    [
      'Image Studio free quota is temporarily unavailable',
      '免费生图服务暂时不可用，请稍后重试',
    ],
  ])(
    'translates a saved %s error and its request ID when the language changes',
    async (error, translated) => {
      const requestId = 'studio-quota-test'
      const original = `${error} (request id: ${requestId})`
      i18next.addResourceBundle(
        'zhCN',
        'translation',
        zh.translation,
        true,
        true
      )
      try {
        render(
          <ImageHistory
            assetUrls={{}}
            isGenerating={false}
            messages={[{ ...failedMessage, error: original }]}
            onDownload={vi.fn()}
            onRetry={vi.fn()}
          />
        )
        expect(screen.getByText(original)).toBeVisible()

        await act(() => i18next.changeLanguage('zhCN'))
        expect(
          screen.getByText(`${translated} (请求 ID: ${requestId})`)
        ).toBeVisible()

        await act(() => i18next.changeLanguage('en'))
        expect(screen.getByText(original)).toBeVisible()
      } finally {
        await act(() => i18next.changeLanguage('en'))
        i18next.removeResourceBundle('zhCN', 'translation')
      }
    }
  )

  test('keeps the prompt, cached image, and revised prompt in the same batch', () => {
    render(
      <ImageHistory
        assetUrls={{ 'generated-1': 'blob:generated' }}
        isGenerating={false}
        messages={[
          { ...completedMessage, revisedPrompt: 'A red kite in a blue sky' },
        ]}
        onDownload={vi.fn()}
        onRetry={vi.fn()}
      />
    )

    const batch = within(screen.getByRole('article', { name: 'A red kite' }))
    expect(batch.getByText('Completed')).toBeVisible()
    expect(
      batch.getByText('Revised prompt: A red kite in a blue sky')
    ).toBeVisible()
    const image = within(
      batch.getByRole('button', { name: 'Preview image' })
    ).getByRole('img')
    expect(image).toHaveAttribute('src', 'blob:generated')
  })

  test('shows generation status next to the prompt while a batch is pending', () => {
    render(
      <ImageHistory
        assetUrls={{}}
        isGenerating
        messages={[]}
        pendingMessage={{ id: 'pending-1', prompt: 'A red kite', createdAt: 1 }}
        onDownload={vi.fn()}
        onRetry={vi.fn()}
      />
    )

    const batch = within(screen.getByRole('article', { name: 'A red kite' }))
    expect(batch.getByRole('status', { name: 'Generating...' })).toBeVisible()
    expect(
      batch.queryByRole('button', { name: 'Preview image' })
    ).not.toBeInTheDocument()
  })

  test('shows the reference image in the pending batch', () => {
    render(
      <ImageHistory
        assetUrls={{}}
        isGenerating
        messages={[]}
        pendingMessage={{
          id: 'pending-edit-1',
          prompt: 'Edit this photo',
          createdAt: 1,
          inputPreview: 'blob:reference',
        }}
        onDownload={vi.fn()}
        onRetry={vi.fn()}
      />
    )

    expect(
      within(
        screen.getByRole('article', { name: 'Edit this photo' })
      ).getByRole('img', {
        name: 'Reference image',
      })
    ).toHaveAttribute('src', 'blob:reference')
  })

  test('retry is disabled while another image request is running', () => {
    const onRetry = vi.fn()
    render(
      <ImageHistory
        assetUrls={{}}
        isGenerating
        messages={[failedMessage]}
        onDownload={vi.fn()}
        onRetry={onRetry}
      />
    )

    const retryButton = screen.getByRole('button', { name: 'Retry' })
    expect(retryButton).toBeDisabled()
    fireEvent.click(retryButton)
    expect(onRetry).not.toHaveBeenCalled()
  })

  test('opens a selected batch, changes images by keyboard and thumbnails, and restores focus', async () => {
    const user = userEvent.setup()
    render(
      <ImageHistory
        assetUrls={{ 'generated-1': 'blob:generated' }}
        isGenerating={false}
        messages={[
          {
            ...completedMessage,
            results: [
              ...completedMessage.results,
              { url: 'https://images.example/detail.png', name: 'detail.png' },
            ],
          },
        ]}
        onDownload={vi.fn()}
        onRetry={vi.fn()}
      />
    )

    const trigger = screen.getByRole('button', { name: 'Preview image 2' })
    await user.click(trigger)
    const dialog = within(screen.getByRole('dialog', { name: 'Image preview' }))
    expect(dialog.getByText('detail.png')).toBeVisible()
    expect(dialog.getByRole('button', { name: 'Next image' })).toHaveAttribute(
      'aria-disabled',
      'true'
    )

    await user.keyboard('{ArrowLeft}')
    expect(dialog.getByText('generated.png')).toBeVisible()
    expect(
      dialog.getByRole('button', { name: 'Preview image 1' })
    ).toHaveAttribute('aria-pressed', 'true')
    await user.click(dialog.getByRole('button', { name: 'Preview image 2' }))
    expect(dialog.getByText('detail.png')).toBeVisible()

    await user.keyboard('{Escape}')
    await waitFor(() => expect(trigger).toHaveFocus())
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  test('starts a single image fitted and lets loaded images zoom, reset, and download', async () => {
    const user = userEvent.setup()
    const onDownload = vi.fn()
    render(
      <ImageHistory
        assetUrls={{ 'generated-1': 'blob:generated' }}
        isGenerating={false}
        messages={[completedMessage]}
        onDownload={onDownload}
        onRetry={vi.fn()}
      />
    )

    await user.click(screen.getByRole('button', { name: 'Preview image' }))
    const dialog = within(screen.getByRole('dialog', { name: 'Image preview' }))
    const image = dialog.getByAltText('Generated image')
    Object.defineProperties(image, {
      naturalWidth: { value: 640 },
      naturalHeight: { value: 480 },
    })
    fireEvent.load(image)

    expect(dialog.getByText('640 × 480')).toBeVisible()
    expect(dialog.getByText('100%')).toBeVisible()
    expect(
      dialog.queryByRole('button', { name: 'Next image' })
    ).not.toBeInTheDocument()
    await user.click(dialog.getByRole('button', { name: 'Zoom in' }))
    expect(dialog.getByText('125%')).toBeVisible()
    await user.click(dialog.getByRole('button', { name: 'Fit to window' }))
    expect(dialog.getByText('100%')).toBeVisible()
    await user.click(dialog.getByRole('button', { name: 'Download' }))
    expect(onDownload).toHaveBeenCalledWith('blob:generated', 'generated.png')
  })

  test('reports an unavailable historical image and disables zoom', async () => {
    const user = userEvent.setup()
    render(
      <ImageHistory
        assetUrls={{}}
        isGenerating={false}
        messages={[completedMessage]}
        onDownload={vi.fn()}
        onRetry={vi.fn()}
      />
    )
    await user.click(screen.getByRole('button', { name: 'Preview image' }))
    const dialog = within(screen.getByRole('dialog', { name: 'Image preview' }))
    fireEvent.error(dialog.getByAltText('Generated image'))
    expect(dialog.getByText('Failed to load image')).toBeVisible()
    expect(dialog.getByRole('button', { name: 'Zoom in' })).toHaveAttribute(
      'aria-disabled',
      'true'
    )
  })
})
