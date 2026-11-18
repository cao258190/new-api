/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { getImageModels, parseImageResponseData, requestImages } from '../api'
import type { ImageStudioParams } from '../types'

const params: ImageStudioParams = {
  model: 'gpt-image-1',
  group: 'default',
  prompt: 'A small red kite',
  n: 2,
  size: '1024x1024',
  quality: 'high',
  background: 'transparent',
  output_format: 'png',
  output_compression: '',
  moderation: 'auto',
  input_fidelity: 'high',
  watermark: false,
}

afterEach(() => vi.restoreAllMocks())

describe('image studio API', () => {
  test('filters group models to GPT Image names', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: ['gpt-image-1', 'dall-e-3', 'gpt-4o'] },
    } as never)

    await expect(getImageModels('premium')).resolves.toEqual([
      { label: 'gpt-image-1', value: 'gpt-image-1' },
    ])
    expect(api.get).toHaveBeenCalledWith('/api/user/models', {
      params: { group: 'premium' },
    })
  })

  test('parses a base64 image when the server returns an object', () => {
    expect(
      parseImageResponseData({ b64_json: 'aGVsbG8=', revised_prompt: 'A kite' })
    ).toEqual([
      {
        url: 'data:image/png;base64,aGVsbG8=',
        name: 'image-1.png',
        revisedPrompt: 'A kite',
      },
    ])
  })

  test('counts a split URL and base64 pair as one image', () => {
    expect(
      parseImageResponseData([
        { url: 'https://images.example/one.png' },
        { b64_json: 'aGVsbG8=' },
        { revised_prompt: 'No payload' },
      ])
    ).toHaveLength(1)
  })

  test('sends JSON generation fields and respects the gateway image count limit', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: { data: [{ url: 'https://images.example/result.png' }] },
    } as never)

    await requestImages({ ...params, n: 129 }, 'generate')

    expect(post).toHaveBeenCalledWith(
      '/pg/images/generations',
      expect.objectContaining({
        model: 'gpt-image-1',
        prompt: 'A small red kite',
        n: 128,
        quality: 'high',
        watermark: false,
      }),
      expect.objectContaining({
        params: { group: 'default' },
        skipErrorHandler: true,
      })
    )
  })

  test('sends edit inputs as multipart fields and files', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ data: { data: [] } } as never)
    const image = new File(['image'], 'reference.png', { type: 'image/png' })
    const mask = new File(['mask'], 'mask.png', { type: 'image/png' })

    await requestImages(params, 'edit', image, mask)

    const body = post.mock.calls[0]?.[1]
    expect(post).toHaveBeenCalledWith(
      '/pg/images/edits',
      expect.any(FormData),
      expect.objectContaining({
        params: { group: 'default' },
        skipErrorHandler: true,
      })
    )
    expect(body).toBeInstanceOf(FormData)
    expect((body as FormData).get('model')).toBe('gpt-image-1')
    expect((body as FormData).has('group')).toBe(false)
    expect((body as FormData).get('image')).toMatchObject({
      name: 'reference.png',
      type: 'image/png',
    })
    expect((body as FormData).get('mask')).toMatchObject({
      name: 'mask.png',
      type: 'image/png',
    })
  })
})
