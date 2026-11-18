/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  IMAGE_STUDIO_MAX_IMAGES,
  type ImageStudioGroupOption,
  type ImageStudioModelOption,
  type ImageStudioParams,
} from './types'

/** Return the models available in a group, limited to GPT Image models. */
export async function getImageModels(
  group: string
): Promise<ImageStudioModelOption[]> {
  const response = await api.get('/api/user/models', { params: { group } })
  requireServerSuccess(response.data)
  const models: unknown[] = Array.isArray(response.data?.data)
    ? response.data.data
    : []
  return models
    .filter((model): model is string => typeof model === 'string')
    .filter((model) => /^gpt-image(?:-|$)/i.test(model))
    .map((model) => ({ label: model, value: model }))
}

/** Load the group list used by the shared group selector. */
export async function getImageGroups(): Promise<ImageStudioGroupOption[]> {
  const response = await api.get('/api/user/self/groups')
  requireServerSuccess(response.data)
  const data = response.data?.data
  if (!data || typeof data !== 'object' || Array.isArray(data)) return []

  return Object.entries(
    data as Record<string, { desc?: string; ratio?: number | string }>
  ).map(([value, info]) => ({
    label: value,
    value,
    ratio: info?.ratio ?? 1,
    desc: info?.desc,
  }))
}

/**
 * Parse OpenAI image response data, accepting URL and base64 payloads.
 *
 * @param data The response `data` field, which may be an object or array.
 */
export function parseImageResponseData(data: unknown, outputFormat = 'png') {
  const entries = Array.isArray(data) ? data : [data]
  const mimeType =
    outputFormat === 'jpeg' ? 'image/jpeg' : `image/${outputFormat}`
  const extension = outputFormat === 'jpeg' ? 'jpg' : outputFormat
  const urlEntries = entries.flatMap((entry) => {
    if (!entry || typeof entry !== 'object') return []
    const value = entry as Record<string, unknown>
    const url = typeof value.url === 'string' ? value.url : ''
    return url ? [{ url, revisedPrompt: value.revised_prompt }] : []
  })
  const base64Entries = entries.flatMap((entry) => {
    if (!entry || typeof entry !== 'object') return []
    const value = entry as Record<string, unknown>
    const base64 = typeof value.b64_json === 'string' ? value.b64_json : ''
    return base64
      ? [
          {
            url: `data:${mimeType};base64,${base64}`,
            revisedPrompt: value.revised_prompt,
          },
        ]
      : []
  })
  const imageCount = Math.max(urlEntries.length, base64Entries.length)
  return Array.from({ length: imageCount }, (_, index) => {
    const item = base64Entries[index] ?? urlEntries[index]
    return {
      ...item,
      name: `image-${index + 1}.${extension}`,
      revisedPrompt:
        typeof item?.revisedPrompt === 'string'
          ? item.revisedPrompt
          : undefined,
    }
  })
}

/** Send a JSON generation or multipart edit request through the gateway. */
export async function requestImages(
  params: ImageStudioParams,
  mode: 'generate' | 'edit',
  inputImage?: File,
  mask?: File
) {
  const boundedCount = Number.isInteger(params.n)
    ? Math.min(IMAGE_STUDIO_MAX_IMAGES, Math.max(1, params.n))
    : 1
  const fields = Object.fromEntries(
    Object.entries({
      model: params.model,
      prompt: params.prompt,
      n: boundedCount,
      size: params.size,
      quality: params.quality,
      background: params.background,
      output_format: params.output_format,
      output_compression: params.output_compression
        ? Number(params.output_compression)
        : undefined,
      moderation: params.moderation,
      input_fidelity: params.input_fidelity,
      watermark: params.watermark,
    }).filter(([, value]) => value !== undefined && value !== '')
  )

  const response =
    mode === 'edit'
      ? await (() => {
          const form = new FormData()
          Object.entries(fields).forEach(([key, value]) =>
            form.append(key, String(value))
          )
          if (inputImage) form.append('image', inputImage, inputImage.name)
          if (mask) form.append('mask', mask, mask.name)
          return api.post('/pg/images/edits', form, {
            params: { group: params.group },
            skipErrorHandler: true,
          })
        })()
      : await api.post('/pg/images/generations', fields, {
          params: { group: params.group },
          skipErrorHandler: true,
        })

  requireServerSuccess(response.data)
  const body = response.data as { data?: unknown; revised_prompt?: unknown }
  const items = parseImageResponseData(body.data, params.output_format)
  return {
    items,
    revisedPrompt:
      typeof body.revised_prompt === 'string'
        ? body.revised_prompt
        : items[0]?.revisedPrompt,
  }
}
