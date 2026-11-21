/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, test, vi } from 'vitest'

import type { ImageStudioParams } from '../../types'
import { GenerationPanel } from '../generation-panel'
import { ImageSettingsDialog } from '../image-settings-dialog'

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
  test('reference upload is always available and reveals mask upload after a reference is selected', () => {
    const onInputImageChange = vi.fn()
    const reference = new File(['image'], 'reference.png', {
      type: 'image/png',
    })
    const { container, rerender } = render(
      <GenerationPanel
        groups={[]}
        inputPreview=''
        isGenerating={false}
        isOptionsLoading={false}
        maskPreview=''
        models={[]}
        onGenerate={vi.fn()}
        onInputImageChange={onInputImageChange}
        onMaskChange={vi.fn()}
        onParamChange={vi.fn()}
        onRemoveInput={vi.fn()}
        onRemoveMask={vi.fn()}
        params={params}
      />
    )

    expect(screen.queryByRole('button', { name: 'Text to image' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Image to image' })).toBeNull()
    expect(
      screen.getByRole('button', { name: 'Upload reference image' })
    ).toBeVisible()
    expect(screen.queryByRole('button', { name: 'Upload mask' })).toBeNull()
    const referenceInput =
      container.querySelector<HTMLInputElement>('input[type="file"]')
    if (!referenceInput) {
      throw new Error('Reference image input was not rendered')
    }
    expect(referenceInput).toHaveAttribute(
      'accept',
      'image/png,image/jpeg,image/webp'
    )
    fireEvent.change(referenceInput, {
      target: { files: [reference] },
    })
    expect(onInputImageChange).toHaveBeenCalledWith(reference)

    rerender(
      <GenerationPanel
        groups={[]}
        inputPreview='blob:reference'
        isGenerating={false}
        isOptionsLoading={false}
        maskPreview=''
        models={[]}
        onGenerate={vi.fn()}
        onInputImageChange={onInputImageChange}
        onMaskChange={vi.fn()}
        onParamChange={vi.fn()}
        onRemoveInput={vi.fn()}
        onRemoveMask={vi.fn()}
        params={params}
      />
    )
    expect(screen.getByRole('button', { name: 'Upload mask' })).toBeVisible()
    const fileInputs =
      container.querySelectorAll<HTMLInputElement>('input[type="file"]')
    expect(fileInputs).toHaveLength(2)
    expect(fileInputs[1]).toHaveAttribute('accept', 'image/png')
  })

  test('text-to-image generation does not require a mode selection or upload', () => {
    const onGenerate = vi.fn()
    render(
      <GenerationPanel
        groups={[]}
        inputPreview=''
        isGenerating={false}
        isOptionsLoading={false}
        maskPreview=''
        models={[{ label: 'gpt-image-1', value: 'gpt-image-1' }]}
        onGenerate={onGenerate}
        onInputImageChange={vi.fn()}
        onMaskChange={vi.fn()}
        onParamChange={vi.fn()}
        onRemoveInput={vi.fn()}
        onRemoveMask={vi.fn()}
        params={params}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: 'Generate' }))
    expect(onGenerate).toHaveBeenCalledOnce()
  })

  test('generation stays disabled when GPT Image models are unavailable', () => {
    render(
      <GenerationPanel
        groups={[]}
        inputPreview=''
        isGenerating={false}
        isOptionsLoading={false}
        maskPreview=''
        models={[]}
        onGenerate={vi.fn()}
        onInputImageChange={vi.fn()}
        onMaskChange={vi.fn()}
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
        models={[{ label: 'gpt-image-1', value: 'gpt-image-1' }]}
        onGenerate={vi.fn()}
        onInputImageChange={vi.fn()}
        onMaskChange={vi.fn()}
        onParamChange={vi.fn()}
        onRemoveInput={vi.fn()}
        onRemoveMask={vi.fn()}
        params={params}
      />
    )

    expect(screen.getByRole('button', { name: 'Generate' })).toBeDisabled()
  })

  test('generation parameters are opened in a settings dialog', () => {
    const onParamChange = vi.fn()
    render(
      <GenerationPanel
        groups={[]}
        inputPreview=''
        isGenerating={false}
        isOptionsLoading={false}
        maskPreview=''
        models={[{ label: 'gpt-image-1', value: 'gpt-image-1' }]}
        onGenerate={vi.fn()}
        onInputImageChange={vi.fn()}
        onMaskChange={vi.fn()}
        onParamChange={onParamChange}
        onRemoveInput={vi.fn()}
        onRemoveMask={vi.fn()}
        params={params}
      />
    )

    expect(
      screen.queryByLabelText('Custom image count')
    ).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Image settings' }))

    const dialog = screen.getByRole('dialog', { name: 'Image settings' })
    expect(within(dialog).getByLabelText('Width')).toHaveValue(1024)
    expect(within(dialog).getByLabelText('Height')).toHaveValue(1024)
    expect(within(dialog).getByRole('button', { name: '4k' })).toHaveAttribute(
      'aria-pressed',
      'false'
    )
    fireEvent.change(within(dialog).getByLabelText('Width'), {
      target: { value: '1031' },
    })
    fireEvent.blur(within(dialog).getByLabelText('Width'))
    expect(onParamChange).toHaveBeenCalledWith('size', '1040x1024')

    fireEvent.click(within(dialog).getByRole('button', { name: '2k' }))
    expect(onParamChange).toHaveBeenCalledWith('size', '2048x2048')

    fireEvent.change(within(dialog).getByLabelText('Custom image count'), {
      target: { value: '12' },
    })
    fireEvent.blur(within(dialog).getByLabelText('Custom image count'))
    expect(onParamChange).toHaveBeenCalledWith('n', 12)
  })

  test.each([
    {
      group: 'Quality',
      initial: 'Auto',
      next: 'High',
      key: 'quality',
      value: 'high',
    },
    { group: 'Images', initial: '1', next: '3', key: 'n', value: 3 },
  ])(
    'keeps only the new $group choice selected after closing and reopening settings',
    async ({ group, initial, next, key, value }) => {
      const user = userEvent.setup()
      const onParamChange = vi.fn()

      function Settings() {
        const [current, setCurrent] = useState(params)
        return (
          <ImageSettingsDialog
            disabled={false}
            onParamChange={(field, setting) => {
              onParamChange(field, setting)
              setCurrent((previous) => ({ ...previous, [field]: setting }))
            }}
            params={current}
          />
        )
      }

      render(<Settings />)
      const trigger = screen.getByRole('button', { name: 'Image settings' })
      await user.click(trigger)

      const options = within(screen.getByRole('group', { name: group }))
      expect(
        options.getByRole('button', { name: initial, pressed: true })
      ).toBeVisible()
      const option = options.getByRole('button', { name: next, pressed: false })
      option.focus()
      await user.keyboard(' ')

      expect(onParamChange).toHaveBeenCalledWith(key, value)
      expect(option).toHaveAttribute('aria-pressed', 'true')
      expect(options.getAllByRole('button', { pressed: true })).toEqual([
        option,
      ])
      await user.click(screen.getByRole('button', { name: 'Done' }))
      await user.click(trigger)
      expect(
        within(screen.getByRole('group', { name: group })).getByRole('button', {
          name: next,
          pressed: true,
        })
      ).toBeVisible()
    }
  )

  test('session controls stay disabled until local sessions finish loading', () => {
    render(
      <GenerationPanel
        groups={[]}
        inputPreview=''
        isGenerating={false}
        isOptionsLoading={false}
        isSessionReady={false}
        maskPreview=''
        models={[{ label: 'gpt-image-1', value: 'gpt-image-1' }]}
        onGenerate={vi.fn()}
        onInputImageChange={vi.fn()}
        onMaskChange={vi.fn()}
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
