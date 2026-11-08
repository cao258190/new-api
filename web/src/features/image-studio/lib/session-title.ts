/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/

const MAX_TITLE_CODE_POINTS = 32

/** Build a compact local session title from its first prompt. */
export function getImageSessionTitle(prompt: string): string {
  const normalizedPrompt = prompt.replaceAll(/\s+/gu, ' ').trim()
  if (!normalizedPrompt) return 'Untitled'

  const codePoints = [...normalizedPrompt]
  if (codePoints.length <= MAX_TITLE_CODE_POINTS) return normalizedPrompt

  return `${codePoints.slice(0, MAX_TITLE_CODE_POINTS - 1).join('')}…`
}
