/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/

/** Preset output resolutions available in the image settings dialog. */
export const IMAGE_RESOLUTIONS = ['1k', '2k', '4k', 'auto'] as const

/** Common image aspect ratios supported by the workspace controls. */
export const IMAGE_ASPECT_RATIOS = [
  { value: '1:1', width: 1, height: 1 },
  { value: '2:3', width: 2, height: 3 },
  { value: '3:2', width: 3, height: 2 },
  { value: '4:3', width: 4, height: 3 },
  { value: '3:4', width: 3, height: 4 },
  { value: '16:9', width: 16, height: 9 },
  { value: '9:16', width: 9, height: 16 },
  { value: '21:9', width: 21, height: 9 },
  { value: '9:21', width: 9, height: 21 },
  { value: 'auto', width: 0, height: 0 },
] as const

/** A supported resolution tier or provider-selected automatic resolution. */
export type ImageResolution = (typeof IMAGE_RESOLUTIONS)[number]

/** A supported aspect ratio or a provider-selected automatic ratio. */
export type ImageAspectRatio = (typeof IMAGE_ASPECT_RATIOS)[number]['value']

const IMAGE_SIZE_PRESETS: Record<
  Exclude<ImageResolution, 'auto'>,
  Record<Exclude<ImageAspectRatio, 'auto'>, string>
> = {
  '1k': {
    '1:1': '1024x1024',
    '2:3': '1024x1536',
    '3:2': '1536x1024',
    '4:3': '1024x768',
    '3:4': '768x1024',
    '16:9': '1536x864',
    '9:16': '864x1536',
    '21:9': '2016x864',
    '9:21': '864x2016',
  },
  '2k': {
    '1:1': '2048x2048',
    '2:3': '1360x2048',
    '3:2': '2048x1360',
    '4:3': '2048x1536',
    '3:4': '1536x2048',
    '16:9': '2048x1152',
    '9:16': '1152x2048',
    '21:9': '2688x1152',
    '9:21': '1152x2688',
  },
  '4k': {
    '1:1': '2880x2880',
    '2:3': '2336x3520',
    '3:2': '3520x2336',
    '4:3': '3312x2480',
    '3:4': '2480x3312',
    '16:9': '3840x2160',
    '9:16': '2160x3840',
    '21:9': '3840x1648',
    '9:21': '1648x3840',
  },
}

/**
 * Return dimensions represented by an API size or the selected preset.
 *
 * @param size The currently stored OpenAI-compatible size value.
 * @param resolution The selected resolution, used for aspect-ratio-only values.
 * @param aspectRatio The selected ratio used when size is automatic.
 */
export function readImageDimensions(
  size: string,
  resolution: ImageResolution,
  aspectRatio: ImageAspectRatio
) {
  const pixels = parsePixelSize(size)
  if (pixels) return pixels
  if (size === 'auto' || aspectRatio === 'auto') {
    return { width: 0, height: 0 }
  }
  const presetResolution = resolution === 'auto' ? '1k' : resolution
  const dimensions = parsePixelSize(
    IMAGE_SIZE_PRESETS[presetResolution][aspectRatio]
  )
  return dimensions ?? { width: 0, height: 0 }
}

/** Infer the closest resolution preset from an existing size value. */
export function inferImageResolution(size: string): ImageResolution {
  const pixels = parsePixelSize(size)
  if (!pixels) return 'auto'

  for (const resolution of ['1k', '2k', '4k'] as const) {
    if (
      Object.values(IMAGE_SIZE_PRESETS[resolution]).includes(
        `${pixels.width}x${pixels.height}`
      )
    ) {
      return resolution
    }
  }

  const longSide = Math.max(pixels.width, pixels.height)
  if (longSide >= 3072) return '4k'
  if (longSide >= 1536) return '2k'
  return '1k'
}

/** Infer the nearest supported aspect ratio from an existing size value. */
export function inferImageAspectRatio(size: string): ImageAspectRatio {
  if (!size || size === 'auto') return 'auto'
  const ratio = parseAspectRatio(size) ?? parsePixelSize(size)
  if (!ratio) return '1:1'

  const targetRatio = ratio.width / ratio.height
  return IMAGE_ASPECT_RATIOS.filter((option) => option.value !== 'auto').reduce(
    (best, option) => {
      const bestOption = IMAGE_ASPECT_RATIOS.find(
        (candidate) => candidate.value === best
      )
      const bestRatio = (bestOption?.width ?? 1) / (bestOption?.height ?? 1)
      const currentRatio = option.width / option.height
      return Math.abs(currentRatio - targetRatio) <
        Math.abs(bestRatio - targetRatio)
        ? option.value
        : best
    },
    '1:1' as ImageAspectRatio
  )
}

/** Map a resolution and aspect ratio to an API-compatible size value. */
export function computeImageSize(
  resolution: ImageResolution,
  aspectRatio: ImageAspectRatio
) {
  if (aspectRatio === 'auto') return 'auto'
  if (resolution === 'auto') return aspectRatio
  return IMAGE_SIZE_PRESETS[resolution][aspectRatio]
}

/** Round a dimension up to the selected alignment step. */
export function alignImageDimension(value: number, alignTo16: boolean) {
  const bounded = Math.max(16, Math.min(4096, Math.floor(value)))
  return alignTo16 ? Math.ceil(bounded / 16) * 16 : bounded
}

function parsePixelSize(value: string) {
  const match = /^(\d+)x(\d+)$/i.exec(value)
  if (!match) return undefined
  return { width: Number(match[1]), height: Number(match[2]) }
}

function parseAspectRatio(value: string) {
  const match = /^(\d+(?:\.\d+)?):(\d+(?:\.\d+)?)$/.exec(value)
  if (!match) return undefined
  const width = Number(match[1])
  const height = Number(match[2])
  return width > 0 && height > 0 ? { width, height } : undefined
}
