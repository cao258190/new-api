/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { Upload, X } from 'lucide-react'
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'

import type { ImageStudioParams } from '../types'

type ParamChange = <K extends keyof ImageStudioParams>(
  key: K,
  value: ImageStudioParams[K]
) => void

type ImageUploadProps = {
  accept: string
  label: string
  preview: string
  previewAlt: string
  removeLabel: string
  onChange: (file?: File) => void
  onRemove: () => void
}

/** Render an accessible image picker with its optional local preview. */
export function ImageUpload(props: ImageUploadProps) {
  const { t } = useTranslation()
  const inputRef = useRef<HTMLInputElement>(null)

  return (
    <div className='flex items-center gap-2'>
      <input
        ref={inputRef}
        aria-hidden='true'
        accept={props.accept}
        className='sr-only'
        onChange={(event) => {
          const file = event.currentTarget.files?.[0]
          event.currentTarget.value = ''
          props.onChange(file)
        }}
        tabIndex={-1}
        type='file'
      />
      <Button
        aria-label={props.label || t('Upload image')}
        onClick={() => inputRef.current?.click()}
        size='icon'
        type='button'
        variant='outline'
      >
        <Upload />
      </Button>
      {props.preview && (
        <div className='relative'>
          <img
            alt={props.previewAlt}
            className='size-10 rounded-md object-cover'
            src={props.preview}
          />
          <Button
            aria-label={props.removeLabel}
            className='absolute -top-2 -right-2'
            onClick={props.onRemove}
            size='icon-sm'
            type='button'
            variant='secondary'
          >
            <X />
          </Button>
        </div>
      )}
    </div>
  )
}

type AdvancedImageOptionsProps = {
  params: ImageStudioParams
  onParamChange: ParamChange
}

/** Render the optional image output and moderation controls. */
export function AdvancedImageOptions(props: AdvancedImageOptionsProps) {
  const { t } = useTranslation()
  return (
    <Collapsible>
      <CollapsibleTrigger className='text-muted-foreground text-sm'>
        {t('Advanced settings')}
      </CollapsibleTrigger>
      <CollapsibleContent className='grid gap-3 pt-3 sm:grid-cols-2'>
        <SelectField
          id='image-background'
          label={t('Background')}
          onChange={(value) => props.onParamChange('background', value)}
          options={[
            ['', t('Default')],
            ['opaque', t('Opaque')],
          ]}
          value={props.params.background}
        />
        <SelectField
          id='image-format'
          label={t('Output format')}
          onChange={(value) => props.onParamChange('output_format', value)}
          options={[
            ['png', 'PNG'],
            ['jpeg', 'JPEG'],
            ['webp', 'WebP'],
          ]}
          value={props.params.output_format}
        />
        <div className='space-y-1'>
          <Label htmlFor='image-compression'>{t('Output compression')}</Label>
          <Input
            id='image-compression'
            max={100}
            min={0}
            onChange={(event) =>
              props.onParamChange(
                'output_compression',
                event.target.value
                  ? String(
                      Math.min(100, Math.max(0, Number(event.target.value)))
                    )
                  : ''
              )
            }
            placeholder={t('Default')}
            type='number'
            value={props.params.output_compression}
          />
        </div>
        <SelectField
          id='image-moderation'
          label={t('Moderation')}
          onChange={(value) => props.onParamChange('moderation', value)}
          options={[
            ['', t('Default')],
            ['auto', t('Auto')],
            ['low', t('Low')],
          ]}
          value={props.params.moderation}
        />
        <SelectField
          id='image-input-fidelity'
          label={t('Input fidelity')}
          onChange={(value) => props.onParamChange('input_fidelity', value)}
          options={[
            ['', t('Default')],
            ['low', t('Low')],
            ['high', t('High')],
          ]}
          value={props.params.input_fidelity}
        />
        <div className='flex items-center gap-2'>
          <Switch
            checked={props.params.watermark}
            id='image-watermark'
            onCheckedChange={(value) => props.onParamChange('watermark', value)}
          />
          <Label htmlFor='image-watermark'>{t('Watermark')}</Label>
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

type SelectFieldProps = {
  id: string
  label: string
  value: string
  options: Array<[string, string]>
  onChange: (value: string) => void
}

function SelectField(props: SelectFieldProps) {
  return (
    <div>
      <Label htmlFor={props.id}>{props.label}</Label>
      <NativeSelect
        id={props.id}
        onChange={(event) => props.onChange(event.target.value)}
        value={props.value}
      >
        {props.options.map(([value, label]) => (
          <NativeSelectOption key={value || 'default'} value={value}>
            {label}
          </NativeSelectOption>
        ))}
      </NativeSelect>
    </div>
  )
}
