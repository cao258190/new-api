/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { describe, expect, test } from 'vitest'

import { getImageSessionTitle } from '../session-title'

describe('image session title', () => {
  test('collapses prompt whitespace and adds an ellipsis within 32 characters', () => {
    const prompt = `${'A'.repeat(31)}  final detail`

    expect(getImageSessionTitle(prompt)).toBe(`${'A'.repeat(31)}…`)
  })

  test('keeps Unicode code points intact when shortening a prompt', () => {
    const prompt = `${'猫'.repeat(30)}😀ending`

    expect(getImageSessionTitle(prompt)).toBe(`${'猫'.repeat(30)}😀…`)
  })

  test('uses the full trimmed prompt when it fits', () => {
    expect(getImageSessionTitle('  A red\nkite  ')).toBe('A red kite')
  })
})
