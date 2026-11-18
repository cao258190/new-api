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
import { Textarea } from '@/components/ui/textarea'

import type {
  ImageStudioGroupOption,
  ImageStudioModelOption,
  ImageStudioParams,
} from '../types'
import { ImageUpload } from './generation-panel-controls'
import { ImageSettingsDialog } from './image-settings-dialog'

type GenerationPanelProps = {
  params: ImageStudioParams
  groups: ImageStudioGroupOption[]
  models: ImageStudioModelOption[]
  isOptionsLoading: boolean
  isGenerating: boolean
  isSessionReady?: boolean
  inputPreview: string
  maskPreview: string
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
      <CardContent className='p-3'>
        <fieldset
          className='min-w-0 space-y-3'
          disabled={!isSessionReady || props.isGenerating}
        >
          <Textarea
            aria-label={t('Prompt')}
            className='min-h-28 resize-y border-0 bg-transparent shadow-none focus-visible:ring-0'
            onChange={(event) =>
              props.onParamChange('prompt', event.target.value)
            }
            placeholder={t('Describe the image you want to create...')}
            value={props.params.prompt}
          />
          <div className='flex flex-wrap items-center justify-between gap-3 border-t pt-3'>
            <div className='flex min-w-0 flex-wrap items-center gap-2'>
              <ImageUpload
                accept='image/png,image/jpeg,image/webp'
                label={t('Upload reference image')}
                preview={props.inputPreview}
                onChange={props.onInputImageChange}
                onRemove={props.onRemoveInput}
                previewAlt={t('Reference image')}
                removeLabel={t('Remove reference image')}
              />
              {props.inputPreview && (
                <ImageUpload
                  accept='image/png'
                  label={t('Upload mask')}
                  preview={props.maskPreview}
                  onChange={props.onMaskChange}
                  onRemove={props.onRemoveMask}
                  previewAlt={t('Mask image')}
                  removeLabel={t('Remove mask')}
                />
              )}
              <ImageSettingsDialog
                disabled={!isSessionReady || props.isGenerating}
                params={props.params}
                onParamChange={props.onParamChange}
              />
            </div>
            <div className='flex min-w-0 flex-wrap items-center justify-end gap-2'>
              <ModelGroupSelector
                disabled={props.isOptionsLoading || !isSessionReady}
                groups={props.groups}
                models={props.models}
                onGroupChange={(value) => props.onParamChange('group', value)}
                onModelChange={(value) => props.onParamChange('model', value)}
                selectedGroup={props.params.group}
                selectedModel={props.params.model}
              />
              <Button
                disabled={
                  props.isGenerating ||
                  !isSessionReady ||
                  props.isOptionsLoading ||
                  !props.params.model
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
          </div>
          {!props.isOptionsLoading && props.models.length === 0 && (
            <p className='text-muted-foreground text-sm'>
              {t('No GPT Image models are available for this group')}
            </p>
          )}
        </fieldset>
      </CardContent>
    </Card>
  )
}
