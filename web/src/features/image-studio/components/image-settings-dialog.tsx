/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { SlidersHorizontal } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import {
  alignImageDimension,
  computeImageSize,
  IMAGE_ASPECT_RATIOS,
  IMAGE_RESOLUTIONS,
  inferImageAspectRatio,
  inferImageResolution,
  readImageDimensions,
  type ImageAspectRatio,
  type ImageResolution,
} from '../lib/image-size'
import { IMAGE_STUDIO_MAX_IMAGES, type ImageStudioParams } from '../types'
import { AdvancedImageOptions } from './generation-panel-controls'

const qualityOptions = [
  { value: 'auto', label: 'Auto' },
  { value: 'high', label: 'High' },
  { value: 'medium', label: 'Medium' },
  { value: 'low', label: 'Low' },
] as const

type ImageSettingsDialogProps = {
  params: ImageStudioParams
  disabled: boolean
  onParamChange: <K extends keyof ImageStudioParams>(
    key: K,
    value: ImageStudioParams[K]
  ) => void
}

/** Render the image parameter editor in a compact, persistent modal. */
export function ImageSettingsDialog(props: ImageSettingsDialogProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [customCount, setCustomCount] = useState(
    props.params.n > 10 ? String(props.params.n) : ''
  )
  const resolution = inferImageResolution(props.params.size)
  const aspectRatio = inferImageAspectRatio(props.params.size)
  const dimensions = readImageDimensions(
    props.params.size,
    resolution,
    aspectRatio
  )
  const alignTo16 = props.params.align16 ?? true

  useEffect(() => {
    setCustomCount(props.params.n > 10 ? String(props.params.n) : '')
  }, [props.params.n])

  const applySizePreset = (
    nextResolution: ImageResolution,
    nextAspectRatio: ImageAspectRatio
  ) => {
    props.onParamChange(
      'size',
      computeImageSize(nextResolution, nextAspectRatio)
    )
  }

  const updateDimension = (key: 'width' | 'height', value: string) => {
    const parsed = Number(value)
    if (!Number.isFinite(parsed) || parsed <= 0) return
    const updated = alignImageDimension(parsed, alignTo16)
    const width = key === 'width' ? updated : dimensions.width
    const height = key === 'height' ? updated : dimensions.height
    if (!width || !height) return
    props.onParamChange('size', `${width}x${height}`)
  }

  return (
    <Dialog
      description={t(
        'Custom dimensions and some options depend on the selected model and provider.'
      )}
      footer={
        <Button onClick={() => setOpen(false)} variant='outline'>
          {t('Done')}
        </Button>
      }
      open={open}
      onOpenChange={setOpen}
      title={t('Image settings')}
      trigger={
        <Button disabled={props.disabled} type='button' variant='outline'>
          <SlidersHorizontal aria-hidden='true' />
          {t('Image settings')}
        </Button>
      }
      contentClassName='sm:max-w-2xl'
      bodyClassName='space-y-6'
    >
      <fieldset className='min-w-0 space-y-6' disabled={props.disabled}>
        <section className='space-y-2.5'>
          <p className='text-sm font-medium'>{t('Quality')}</p>
          <div
            aria-label={t('Quality')}
            className='grid grid-cols-4 gap-2'
            role='group'
          >
            {qualityOptions.map((option) => (
              <Button
                aria-pressed={props.params.quality === option.value}
                className='aria-pressed:border-foreground w-full rounded-full'
                key={option.value}
                onClick={() => props.onParamChange('quality', option.value)}
                type='button'
                variant='outline'
              >
                {t(option.label)}
              </Button>
            ))}
          </div>
        </section>

        <section className='space-y-2.5'>
          <div className='flex items-center justify-between gap-3'>
            <p className='text-sm font-medium'>{t('Size')}</p>
            <div className='flex items-center gap-2'>
              <Label htmlFor='image-align-16' className='text-xs'>
                {t('Align to 16')}
              </Label>
              <Switch
                checked={alignTo16}
                id='image-align-16'
                onCheckedChange={(value) =>
                  props.onParamChange('align16', value)
                }
              />
            </div>
          </div>
          <div className='grid grid-cols-[1fr_auto_1fr] items-center gap-2'>
            <DimensionInput
              disabled={aspectRatio === 'auto'}
              id='image-width'
              label={t('Width')}
              prefix='W'
              step={alignTo16 ? 16 : 1}
              value={dimensions.width}
              onCommit={(value) => updateDimension('width', value)}
            />
            <span aria-hidden='true' className='text-muted-foreground px-1'>
              ↔
            </span>
            <DimensionInput
              disabled={aspectRatio === 'auto'}
              id='image-height'
              label={t('Height')}
              prefix='H'
              step={alignTo16 ? 16 : 1}
              value={dimensions.height}
              onCommit={(value) => updateDimension('height', value)}
            />
          </div>
        </section>

        <section className='space-y-2.5'>
          <p className='text-sm font-medium'>{t('Resolution')}</p>
          <div className='grid grid-cols-4 gap-2'>
            {IMAGE_RESOLUTIONS.map((value) => (
              <Button
                aria-pressed={resolution === value}
                className='aria-pressed:border-foreground w-full rounded-full'
                key={value}
                onClick={() =>
                  applySizePreset(
                    value,
                    aspectRatio === 'auto' ? '1:1' : aspectRatio
                  )
                }
                type='button'
                variant='outline'
              >
                {value === 'auto' ? t('Auto') : value}
              </Button>
            ))}
          </div>
        </section>

        <section className='space-y-2.5'>
          <p className='text-sm font-medium'>{t('Aspect ratio')}</p>
          <div
            aria-label={t('Aspect ratio')}
            className='grid grid-cols-3 gap-2 sm:grid-cols-5'
            role='group'
          >
            {IMAGE_ASPECT_RATIOS.map((option) => (
              <button
                aria-pressed={aspectRatio === option.value}
                className='border-input aria-pressed:border-foreground hover:bg-accent focus-visible:ring-ring/50 flex min-h-20 flex-col items-center justify-center gap-1.5 rounded-xl border px-2 py-2 text-sm transition-colors focus-visible:ring-3'
                key={option.value}
                onClick={() =>
                  applySizePreset(resolution, option.value as ImageAspectRatio)
                }
                type='button'
              >
                <span
                  aria-hidden='true'
                  className='grid h-7 w-9 place-items-center'
                >
                  {option.width === 0 ? (
                    <span className='text-[10px] font-medium'>AUTO</span>
                  ) : (
                    <span
                      className='border-2 border-current'
                      style={{
                        width:
                          option.width >= option.height
                            ? 24
                            : Math.max(10, 24 * (option.width / option.height)),
                        height:
                          option.height >= option.width
                            ? 24
                            : Math.max(10, 24 * (option.height / option.width)),
                      }}
                    />
                  )}
                </span>
                <span>
                  {option.value === 'auto' ? t('Auto') : option.value}
                </span>
              </button>
            ))}
          </div>
        </section>

        <section className='flex items-center justify-between gap-3'>
          <div className='space-y-0.5'>
            <Label htmlFor='image-transparent'>
              {t('Transparent background')}
            </Label>
            <p className='text-muted-foreground text-xs'>
              {t('Transparent backgrounds are supported only by some models.')}
            </p>
          </div>
          <Switch
            checked={props.params.background === 'transparent'}
            id='image-transparent'
            onCheckedChange={(value) =>
              props.onParamChange('background', value ? 'transparent' : '')
            }
          />
        </section>

        <section className='space-y-2.5'>
          <p className='text-sm font-medium'>{t('Images')}</p>
          <div
            aria-label={t('Images')}
            className='grid grid-cols-4 gap-2'
            role='group'
          >
            {Array.from({ length: 10 }, (_, index) => index + 1).map(
              (value) => (
                <Button
                  aria-pressed={props.params.n === value}
                  className='aria-pressed:border-foreground w-full rounded-full'
                  key={value}
                  onClick={() => props.onParamChange('n', value)}
                  type='button'
                  variant='outline'
                >
                  {value} {t(value === 1 ? 'Image' : 'Images')}
                </Button>
              )
            )}
            <Input
              aria-label={t('Custom image count')}
              className='col-span-2 rounded-full text-center'
              max={IMAGE_STUDIO_MAX_IMAGES}
              min={1}
              onBlur={() => {
                const value = Number(customCount)
                if (Number.isInteger(value) && value >= 1) {
                  props.onParamChange(
                    'n',
                    Math.min(IMAGE_STUDIO_MAX_IMAGES, value)
                  )
                } else {
                  setCustomCount(
                    props.params.n > 10 ? String(props.params.n) : ''
                  )
                }
              }}
              onChange={(event) => setCustomCount(event.target.value)}
              placeholder={t('Custom')}
              step={1}
              type='number'
              value={customCount}
            />
          </div>
        </section>

        <AdvancedImageOptions
          params={props.params}
          onParamChange={props.onParamChange}
        />
      </fieldset>
    </Dialog>
  )
}

type DimensionInputProps = {
  id: string
  label: string
  prefix: string
  value: number
  step: number
  disabled: boolean
  onCommit: (value: string) => void
}

/** Commit a custom image dimension when the field loses focus. */
function DimensionInput(props: DimensionInputProps) {
  return (
    <label className='bg-muted flex h-12 min-w-0 items-center overflow-hidden rounded-xl px-3'>
      <span className='text-muted-foreground w-8 shrink-0 text-sm'>
        {props.prefix}
      </span>
      <Input
        aria-label={props.label}
        className='h-10 min-w-0 border-0 bg-transparent px-1 shadow-none focus-visible:ring-0'
        defaultValue={props.value || ''}
        disabled={props.disabled}
        id={props.id}
        key={`${props.id}-${props.value}`}
        max={4096}
        min={16}
        onBlur={(event) => props.onCommit(event.currentTarget.value)}
        onKeyDown={(event) => {
          if (event.key === 'Enter') event.currentTarget.blur()
        }}
        step={props.step}
        type='number'
      />
    </label>
  )
}
