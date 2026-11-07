/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { fireEvent, render, screen } from '@testing-library/react'
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

describe('image history', () => {
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
