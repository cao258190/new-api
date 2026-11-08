/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

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
  test('renders the prompt and image result as separate chat messages', () => {
    render(
      <ImageHistory
        assetUrls={{ 'generated-1': 'blob:generated' }}
        isGenerating={false}
        messages={[completedMessage]}
        onDownload={vi.fn()}
        onRetry={vi.fn()}
      />
    )

    expect(
      within(screen.getByRole('group', { name: 'Prompt' })).getByText(
        'A red kite'
      )
    ).toBeVisible()
    expect(
      within(screen.getByRole('group', { name: 'Image preview' })).getByRole(
        'button',
        { name: 'Preview image' }
      )
    ).toBeVisible()
  })

  test('shows an animated assistant status while generation is pending', () => {
    render(
      <ImageHistory
        assetUrls={{}}
        isGenerating
        messages={[]}
        pendingMessage={{ id: 'pending-1', prompt: 'A red kite' }}
        onDownload={vi.fn()}
        onRetry={vi.fn()}
      />
    )

    expect(screen.getByRole('status', { name: 'Generating...' })).toBeVisible()
    expect(
      within(screen.getByRole('group', { name: 'Prompt' })).getByText(
        'A red kite'
      )
    ).toBeVisible()
  })

  test('shows the reference image in the pending user message', () => {
    render(
      <ImageHistory
        assetUrls={{}}
        isGenerating
        messages={[]}
        pendingMessage={{
          id: 'pending-edit-1',
          prompt: 'Edit this photo',
          inputPreview: 'blob:reference',
        }}
        onDownload={vi.fn()}
        onRetry={vi.fn()}
      />
    )

    expect(
      within(screen.getByRole('group', { name: 'Prompt' })).getByRole('img', {
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
})
