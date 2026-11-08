/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { describe, expect, test } from 'vitest'

import {
  alignImageDimension,
  computeImageSize,
  inferImageAspectRatio,
  inferImageResolution,
  readImageDimensions,
} from '../image-size'

describe('image size controls', () => {
  test('selecting a resolution and aspect ratio maps to image dimensions', () => {
    expect(computeImageSize('2k', '16:9')).toBe('2048x1152')
  })

  test('automatic resolution preserves the selected aspect ratio', () => {
    expect(computeImageSize('auto', '16:9')).toBe('16:9')
    expect(readImageDimensions('16:9', 'auto', '16:9')).toEqual({
      width: 1536,
      height: 864,
    })
  })

  test('existing custom dimensions select the nearest presets', () => {
    expect(inferImageResolution('1600x900')).toBe('2k')
    expect(inferImageAspectRatio('1600x900')).toBe('16:9')
  })

  test('custom dimensions are bounded and optionally aligned to 16 pixels', () => {
    expect(alignImageDimension(1025, true)).toBe(1040)
    expect(alignImageDimension(1025, false)).toBe(1025)
    expect(alignImageDimension(5000, false)).toBe(4096)
  })
})
