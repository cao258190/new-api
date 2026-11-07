/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { Download, RotateCcw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

import type { ImageStudioMessage } from '../types'

type ImageHistoryProps = {
  messages: ImageStudioMessage[]
  assetUrls: Record<string, string>
  isGenerating: boolean
  onDownload: (url: string, name: string) => void
  onRetry: (message: ImageStudioMessage) => void
}

/** Show each prompt and the generated or edited images in the active session. */
export function ImageHistory(props: ImageHistoryProps) {
  const { t } = useTranslation()
  const [preview, setPreview] = useState<{ url: string; alt: string }>()

  return (
    <div className='grid gap-5'>
      {props.messages.map((message) => (
        <Card key={message.id}>
          <CardHeader>
            <CardTitle className='flex items-center justify-between gap-3'>
              <span className='truncate'>{message.prompt}</span>
              <div className='flex shrink-0 items-center gap-2'>
                {message.results.length > 1 &&
                  message.status === 'complete' && (
                    <Button
                      onClick={() =>
                        message.results.forEach((result) => {
                          const src = result.assetId
                            ? (props.assetUrls[result.assetId] ?? result.url)
                            : result.url
                          props.onDownload(src, result.name)
                        })
                      }
                      size='sm'
                      variant='outline'
                    >
                      <Download />
                      {t('Download all images')}
                    </Button>
                  )}
                <span className='text-muted-foreground text-xs'>
                  {message.mode === 'edit'
                    ? t('Image to image')
                    : t('Text to image')}
                </span>
              </div>
            </CardTitle>
          </CardHeader>
          <CardContent>
            {message.status === 'error' ? (
              <div className='flex items-center justify-between gap-3'>
                <p className='text-destructive text-sm'>{message.error}</p>
                <Button
                  disabled={props.isGenerating}
                  onClick={() => props.onRetry(message)}
                  variant='outline'
                >
                  <RotateCcw />
                  {t('Retry')}
                </Button>
              </div>
            ) : (
              <div className='grid grid-cols-2 gap-3 md:grid-cols-4'>
                {message.results.map((result) => {
                  const src = result.assetId
                    ? (props.assetUrls[result.assetId] ?? result.url)
                    : result.url
                  return (
                    <div
                      className='group relative overflow-hidden rounded-lg border'
                      key={result.assetId ?? result.url}
                    >
                      <button
                        aria-label={t('Preview image')}
                        className='block w-full cursor-zoom-in'
                        onClick={() =>
                          setPreview({ url: src, alt: message.prompt })
                        }
                        type='button'
                      >
                        <img
                          alt={message.prompt}
                          className='aspect-square w-full object-cover'
                          loading='lazy'
                          src={src}
                        />
                      </button>
                      <Button
                        aria-label={t('Download image')}
                        className='absolute right-2 bottom-2 opacity-100 focus-visible:opacity-100 md:opacity-0 md:group-hover:opacity-100'
                        onClick={() => props.onDownload(src, result.name)}
                        size='icon-sm'
                      >
                        <Download />
                      </Button>
                    </div>
                  )
                })}
              </div>
            )}
            {message.revisedPrompt && (
              <p className='text-muted-foreground mt-3 text-xs'>
                {t('Revised prompt')}: {message.revisedPrompt}
              </p>
            )}
          </CardContent>
        </Card>
      ))}
      <Dialog
        contentClassName='sm:max-w-5xl'
        onOpenChange={(open) => !open && setPreview(undefined)}
        open={Boolean(preview)}
        showCloseButton
        title={t('Image preview')}
      >
        {preview && (
          <img
            alt={preview.alt}
            className='mx-auto max-h-[75dvh] max-w-full object-contain'
            src={preview.url}
          />
        )}
      </Dialog>
    </div>
  )
}
