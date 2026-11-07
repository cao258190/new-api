/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { Image as ImageIcon, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { ModelGroupSelector } from '@/components/model-group-selector'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'

import {
  IMAGE_STUDIO_MAX_IMAGES,
  type ImageStudioGroupOption,
  type ImageStudioMode,
  type ImageStudioModelOption,
  type ImageStudioParams,
} from '../types'
import { AdvancedImageOptions, ImageUpload } from './generation-panel-controls'

type GenerationPanelProps = {
  params: ImageStudioParams
  mode: ImageStudioMode
  groups: ImageStudioGroupOption[]
  models: ImageStudioModelOption[]
  isOptionsLoading: boolean
  isGenerating: boolean
  isSessionReady?: boolean
  inputPreview: string
  maskPreview: string
  onModeChange: (mode: ImageStudioMode) => void
  onParamChange: <K extends keyof ImageStudioParams>(
    key: K,
    value: ImageStudioParams[K]
  ) => void
  onInputImageChange: (file?: File) => void
  onMaskChange: (file?: File) => void
  onRemoveInput: () => void
  onRemoveMask: () => void
  onGenerate: () => void
}

/** Render the model controls, prompt, image inputs, and generation options. */
export function GenerationPanel(props: GenerationPanelProps) {
  const { t } = useTranslation()
  const isSessionReady = props.isSessionReady ?? true

  return (
    <Card className='mt-auto'>
      <CardContent className='pt-4'>
        <fieldset className='min-w-0 space-y-4' disabled={!isSessionReady}>
          <div className='flex flex-wrap gap-2'>
            <Button
              aria-pressed={props.mode === 'generate'}
              onClick={() => props.onModeChange('generate')}
              variant={props.mode === 'generate' ? 'default' : 'outline'}
            >
              {t('Text to image')}
            </Button>
            <Button
              aria-pressed={props.mode === 'edit'}
              onClick={() => props.onModeChange('edit')}
              variant={props.mode === 'edit' ? 'default' : 'outline'}
            >
              {t('Image to image')}
            </Button>
            <div className='flex min-w-0 basis-full justify-start sm:ml-auto sm:min-w-[260px] sm:flex-1 sm:basis-auto sm:justify-end'>
              <ModelGroupSelector
                disabled={props.isOptionsLoading || !isSessionReady}
                groups={props.groups}
                models={props.models}
                onGroupChange={(value) => props.onParamChange('group', value)}
                onModelChange={(value) => props.onParamChange('model', value)}
                selectedGroup={props.params.group}
                selectedModel={props.params.model}
              />
            </div>
          </div>
          <Textarea
            aria-label={t('Prompt')}
            className='min-h-24'
            onChange={(event) =>
              props.onParamChange('prompt', event.target.value)
            }
            placeholder={t('Describe the image you want to create...')}
            value={props.params.prompt}
          />
          {props.mode === 'edit' && (
            <div className='flex flex-wrap gap-3'>
              <ImageUpload
                accept='image/png,image/jpeg,image/webp'
                label={t('Upload reference image')}
                preview={props.inputPreview}
                onChange={props.onInputImageChange}
                onRemove={props.onRemoveInput}
                previewAlt={t('Reference image')}
                removeLabel={t('Remove reference image')}
              />
              <ImageUpload
                accept='image/png'
                label={t('Upload mask')}
                preview={props.maskPreview}
                onChange={props.onMaskChange}
                onRemove={props.onRemoveMask}
                previewAlt={t('Mask image')}
                removeLabel={t('Remove mask')}
              />
            </div>
          )}
          <div className='grid gap-3 sm:grid-cols-3'>
            <div>
              <Label htmlFor='image-n'>{t('Images')}</Label>
              <Input
                id='image-n'
                max={IMAGE_STUDIO_MAX_IMAGES}
                min={1}
                onChange={(event) =>
                  props.onParamChange(
                    'n',
                    Math.min(
                      IMAGE_STUDIO_MAX_IMAGES,
                      Math.max(1, Math.trunc(Number(event.target.value) || 1))
                    )
                  )
                }
                step={1}
                type='number'
                value={props.params.n}
              />
            </div>
            <div>
              <Label htmlFor='image-size'>{t('Size')}</Label>
              <NativeSelect
                id='image-size'
                onChange={(event) =>
                  props.onParamChange('size', event.target.value)
                }
                value={props.params.size}
              >
                <NativeSelectOption value='1024x1024'>
                  1024×1024
                </NativeSelectOption>
                <NativeSelectOption value='1536x1024'>
                  1536×1024
                </NativeSelectOption>
                <NativeSelectOption value='1024x1536'>
                  1024×1536
                </NativeSelectOption>
                <NativeSelectOption value='auto'>
                  {t('Auto')}
                </NativeSelectOption>
              </NativeSelect>
            </div>
            <div>
              <Label htmlFor='image-quality'>{t('Quality')}</Label>
              <NativeSelect
                id='image-quality'
                onChange={(event) =>
                  props.onParamChange('quality', event.target.value)
                }
                value={props.params.quality}
              >
                <NativeSelectOption value='auto'>
                  {t('Auto')}
                </NativeSelectOption>
                <NativeSelectOption value='low'>{t('Low')}</NativeSelectOption>
                <NativeSelectOption value='medium'>
                  {t('Medium')}
                </NativeSelectOption>
                <NativeSelectOption value='high'>
                  {t('High')}
                </NativeSelectOption>
              </NativeSelect>
            </div>
          </div>
          <AdvancedImageOptions
            params={props.params}
            onParamChange={props.onParamChange}
          />
          {!props.isOptionsLoading && props.models.length === 0 && (
            <p className='text-muted-foreground text-sm'>
              {t('No GPT Image models are available for this group')}
            </p>
          )}
          <div className='flex justify-end'>
            <Button
              disabled={
                props.isGenerating ||
                !isSessionReady ||
                props.isOptionsLoading ||
                !props.params.model ||
                (props.mode === 'edit' && !props.inputPreview)
              }
              onClick={props.onGenerate}
              size='lg'
            >
              {props.isGenerating ? (
                <>
                  <Loader2 className='animate-spin' />
                  {t('Generating...')}
                </>
              ) : (
                <>
                  <ImageIcon />
                  {t('Generate')}
                </>
              )}
            </Button>
          </div>
        </fieldset>
      </CardContent>
    </Card>
  )
}
