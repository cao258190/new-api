/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/

/** The two OpenAI image operations supported by the first studio version. */
export type ImageStudioMode = 'generate' | 'edit'

/** Match the gateway's OpenAI image request count limit. */
export const IMAGE_STUDIO_MAX_IMAGES = 128

/** A model enabled for the current user's selected group. */
export type ImageStudioModelOption = { label: string; value: string }

/** A billing/access group available to the current user. */
export type ImageStudioGroupOption = {
  label: string
  value: string
  ratio: number | string
  desc?: string
}

/** Parameters sent to the OpenAI-compatible image endpoints. */
export type ImageStudioParams = {
  model: string
  group: string
  prompt: string
  n: number
  size: string
  /** Whether custom dimensions should be rounded up to 16-pixel steps. */
  align16?: boolean
  quality: string
  background: string
  output_format: string
  output_compression: string
  moderation: string
  input_fidelity: string
  watermark: boolean
}

/** A browser-persisted image or an image URL that could not be cached. */
export type ImageStudioAsset = {
  id: string
  name: string
  mimeType: string
  blob?: Blob
  url?: string
}

/** One generation attempt and its output images. */
export type ImageStudioMessage = {
  id: string
  prompt: string
  mode: ImageStudioMode
  params: ImageStudioParams
  createdAt: number
  status: 'complete' | 'error'
  error?: string
  revisedPrompt?: string
  inputAssetId?: string
  maskAssetId?: string
  results: Array<{ assetId?: string; url: string; name: string }>
}

/** A named local conversation containing image generation attempts. */
export type ImageStudioSession = {
  id: string
  title: string
  createdAt: number
  updatedAt: number
  draft?: {
    mode: ImageStudioMode
    params: ImageStudioParams
    inputAssetId?: string
    maskAssetId?: string
  }
  messages: ImageStudioMessage[]
}
