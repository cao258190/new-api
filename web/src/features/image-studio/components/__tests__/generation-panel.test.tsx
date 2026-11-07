/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import type { ImageStudioParams } from '../../types'
import { GenerationPanel } from '../generation-panel'

const params: ImageStudioParams = {
  model: 'gpt-image-1',
  group: 'default',
  prompt: '',
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

describe('image studio generation panel', () => {
  test('switching to edit mode exposes reference and PNG mask uploads', () => {
    const onModeChange = vi.fn()
    const { rerender } = render(
      <GenerationPanel
        groups={[]}
        inputPreview=''
        isGenerating={false}
        isOptionsLoading={false}
        maskPreview=''
        mode='generate'
        models={[]}
        onGenerate={vi.fn()}
        onInputImageChange={vi.fn()}
        onMaskChange={vi.fn()}
        onModeChange={onModeChange}
        onParamChange={vi.fn()}
        onRemoveInput={vi.fn()}
        onRemoveMask={vi.fn()}
        params={params}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: 'Image to image' }))
    expect(
      screen.getByRole('button', { name: 'Image to image' })
    ).toHaveAttribute('aria-pressed', 'false')
    rerender(
      <GenerationPanel
        groups={[]}
        inputPreview=''
        isGenerating={false}
        isOptionsLoading={false}
        maskPreview=''
        mode='edit'
        models={[]}
        onGenerate={vi.fn()}
        onInputImageChange={vi.fn()}
        onMaskChange={vi.fn()}
        onModeChange={onModeChange}
        onParamChange={vi.fn()}
        onRemoveInput={vi.fn()}
        onRemoveMask={vi.fn()}
        params={params}
      />
    )
    expect(
      screen.getByRole('button', { name: 'Image to image' })
    ).toHaveAttribute('aria-pressed', 'true')

    expect(onModeChange).toHaveBeenCalledWith('edit')
    expect(screen.getByLabelText('Upload reference image')).toHaveAttribute(
      'accept',
      'image/png,image/jpeg,image/webp'
    )
    expect(screen.getByLabelText('Upload mask')).toHaveAttribute(
      'accept',
      'image/png'
    )
  })

  test('generation stays disabled when GPT Image models are unavailable', () => {
    render(
      <GenerationPanel
        groups={[]}
        inputPreview=''
        isGenerating={false}
        isOptionsLoading={false}
        maskPreview=''
        mode='generate'
        models={[]}
        onGenerate={vi.fn()}
        onInputImageChange={vi.fn()}
        onMaskChange={vi.fn()}
        onModeChange={vi.fn()}
        onParamChange={vi.fn()}
        onRemoveInput={vi.fn()}
        onRemoveMask={vi.fn()}
        params={{ ...params, model: '' }}
      />
    )

    expect(
      screen.getByText('No GPT Image models are available for this group')
    ).toBeVisible()
    expect(screen.getByRole('button', { name: 'Generate' })).toBeDisabled()
  })

  test('generation stays disabled while group models are loading', () => {
    render(
      <GenerationPanel
        groups={[]}
        inputPreview=''
        isGenerating={false}
        isOptionsLoading
        maskPreview=''
        mode='generate'
        models={[{ label: 'gpt-image-1', value: 'gpt-image-1' }]}
        onGenerate={vi.fn()}
        onInputImageChange={vi.fn()}
        onMaskChange={vi.fn()}
        onModeChange={vi.fn()}
        onParamChange={vi.fn()}
        onRemoveInput={vi.fn()}
        onRemoveMask={vi.fn()}
        params={params}
      />
    )

    expect(screen.getByRole('button', { name: 'Generate' })).toBeDisabled()
  })

  test('session controls stay disabled until local sessions finish loading', () => {
    render(
      <GenerationPanel
        groups={[]}
        inputPreview=''
        isGenerating={false}
        isOptionsLoading={false}
        isSessionReady={false}
        maskPreview=''
        mode='generate'
        models={[{ label: 'gpt-image-1', value: 'gpt-image-1' }]}
        onGenerate={vi.fn()}
        onInputImageChange={vi.fn()}
        onMaskChange={vi.fn()}
        onModeChange={vi.fn()}
        onParamChange={vi.fn()}
        onRemoveInput={vi.fn()}
        onRemoveMask={vi.fn()}
        params={params}
      />
    )

    expect(screen.getByRole('textbox', { name: 'Prompt' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Generate' })).toBeDisabled()
  })
})
