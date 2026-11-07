/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { getImageGroups, getImageModels, requestImages } from '../api'
import { ImageStudio } from '../index'
import {
  imageUrlToBlob,
  loadImageAsset,
  loadImageSessions,
  saveImageAsset,
  saveImageSession,
} from '../storage'
import type { ImageStudioParams, ImageStudioSession } from '../types'

vi.mock('../api', () => ({
  getImageGroups: vi.fn(),
  getImageModels: vi.fn(),
  requestImages: vi.fn(),
}))

vi.mock('../storage', () => ({
  deleteImageAsset: vi.fn(),
  deleteImageSession: vi.fn(),
  deleteSessionAssets: vi.fn(),
  imageUrlToBlob: vi.fn(),
  loadImageAsset: vi.fn(),
  loadImageSessions: vi.fn(),
  saveImageAsset: vi.fn(),
  saveImageSession: vi.fn(),
}))

const currentParams: ImageStudioParams = {
  model: 'gpt-image-1',
  group: 'default',
  prompt: 'Current draft prompt',
  n: 1,
  size: '1024x1024',
  quality: 'auto',
  background: '',
  output_format: 'png',
  output_compression: '',
  moderation: '',
  input_fidelity: '',
  watermark: false,
}

const retryParams: ImageStudioParams = {
  ...currentParams,
  prompt: 'Previous failed prompt',
}

const currentAsset: Blob = new Blob(['current image'], { type: 'image/png' })
const retryAsset: Blob = new Blob(['previous image'], { type: 'image/png' })
const currentAssetId = 'current-draft-image'
const retryAssetId = 'previous-attempt-image'

const session: ImageStudioSession = {
  id: 'session-1',
  title: 'Retry behavior',
  createdAt: 1,
  updatedAt: 1,
  draft: {
    mode: 'edit',
    params: currentParams,
    inputAssetId: currentAssetId,
  },
  messages: [
    {
      id: 'failed-attempt',
      prompt: retryParams.prompt,
      mode: 'edit',
      params: retryParams,
      createdAt: 2,
      status: 'error',
      error: 'The previous request failed',
      inputAssetId: retryAssetId,
      results: [],
    },
  ],
}

describe('image studio retry', () => {
  let client: QueryClient

  beforeEach(() => {
    client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    useAuthStore.getState().auth.setUser({
      id: 42,
      username: 'image-studio-user',
      role: 1,
    })
    vi.mocked(getImageGroups).mockResolvedValue([
      { label: 'default', value: 'default', ratio: 1 },
    ])
    vi.mocked(getImageModels).mockResolvedValue([
      { label: 'gpt-image-1', value: 'gpt-image-1' },
    ])
    vi.mocked(loadImageSessions).mockResolvedValue([session])
    vi.mocked(loadImageAsset).mockImplementation(async (_userId, id) => {
      const blob = id === currentAssetId ? currentAsset : retryAsset
      return {
        id,
        name: id === currentAssetId ? 'current.png' : 'retry.png',
        mimeType: 'image/png',
        blob,
      }
    })
    vi.mocked(saveImageAsset).mockResolvedValue('saved-asset')
    vi.mocked(saveImageSession).mockResolvedValue('saved-session')
    vi.mocked(imageUrlToBlob).mockResolvedValue(
      new Blob(['generated image'], { type: 'image/png' })
    )
    vi.mocked(requestImages)
      .mockRejectedValueOnce(new Error('Retry failed'))
      .mockResolvedValueOnce({
        revisedPrompt: undefined,
        items: [
          {
            url: 'https://example.test/generated.png',
            name: 'generated.png',
            revisedPrompt: undefined,
          },
        ],
      })
    vi.spyOn(URL, 'createObjectURL').mockImplementation(
      () => `blob:image-studio-${crypto.randomUUID()}`
    )
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)
  })

  afterEach(() => {
    cleanup()
    client.clear()
    useAuthStore.setState(useAuthStore.getInitialState(), true)
    vi.restoreAllMocks()
  })

  it('keeps the current draft image for a new edit after retrying a historical message', async () => {
    const user = userEvent.setup()
    render(
      <QueryClientProvider client={client}>
        <ImageStudio />
      </QueryClientProvider>
    )

    const referenceImage = await screen.findByRole('img', {
      name: 'Reference image',
    })
    const currentPreview = referenceImage.getAttribute('src')
    await user.click(screen.getByRole('button', { name: 'Retry' }))
    await waitFor(() => expect(requestImages).toHaveBeenCalledTimes(1))
    expect(requestImages).toHaveBeenNthCalledWith(
      1,
      retryParams,
      'edit',
      expect.objectContaining({ name: 'retry.png' }),
      undefined
    )

    await user.click(await screen.findByRole('button', { name: 'Generate' }))
    await waitFor(() => expect(requestImages).toHaveBeenCalledTimes(2))
    expect(requestImages).toHaveBeenNthCalledWith(
      2,
      currentParams,
      'edit',
      expect.objectContaining({ name: 'current.png' }),
      undefined
    )
    expect(
      screen.getByRole('img', { name: 'Reference image' })
    ).toHaveAttribute('src', currentPreview)
  })
})
