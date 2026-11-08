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

import { Loader } from '@/components/ai-elements/loader'
import { Message, MessageContent } from '@/components/ai-elements/message'
import { Shimmer } from '@/components/ai-elements/shimmer'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'

import type { ImageStudioMessage } from '../types'

type ImageHistoryProps = {
  messages: ImageStudioMessage[]
  pendingMessage?: {
    id: string
    prompt: string
    inputPreview?: string
    maskPreview?: string
  }
  assetUrls: Record<string, string>
  isGenerating: boolean
  onDownload: (url: string, name: string) => void
  onRetry: (message: ImageStudioMessage) => void
}

/** Show prompts on the right and generated images on the left. */
export function ImageHistory(props: ImageHistoryProps) {
  const { t } = useTranslation()
  const [preview, setPreview] = useState<{ url: string; alt: string }>()

  return (
    <div className='space-y-6'>
      {props.messages.map((message) => (
        <div className='space-y-3' key={message.id}>
          <Message aria-label={t('Prompt')} from='user' role='group'>
            <MessageContent className='max-w-[85%] break-words whitespace-pre-wrap'>
              {message.prompt}
              {(message.inputAssetId || message.maskAssetId) && (
                <div className='mt-2 flex flex-wrap gap-2'>
                  {[message.inputAssetId, message.maskAssetId].map(
                    (assetId, index) => {
                      if (!assetId) return null
                      const src = props.assetUrls[assetId]
                      if (!src) return null
                      return (
                        <img
                          alt={
                            index === 0 ? t('Reference image') : t('Mask image')
                          }
                          className='size-16 rounded-md object-cover'
                          key={assetId}
                          src={src}
                        />
                      )
                    }
                  )}
                </div>
              )}
            </MessageContent>
          </Message>
          <Message
            aria-label={t('Image preview')}
            from='assistant'
            role='group'
          >
            <div className='w-full min-w-0 space-y-3'>
              {message.status === 'error' ? (
                <div className='flex flex-wrap items-center justify-between gap-3'>
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
              {message.results.length > 1 && message.status === 'complete' && (
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
              {message.revisedPrompt && (
                <p className='text-muted-foreground text-xs'>
                  {t('Revised prompt')}: {message.revisedPrompt}
                </p>
              )}
            </div>
          </Message>
        </div>
      ))}
      {props.pendingMessage && (
        <div className='space-y-3' key={props.pendingMessage.id}>
          <Message aria-label={t('Prompt')} from='user' role='group'>
            <MessageContent className='max-w-[85%] break-words whitespace-pre-wrap'>
              {props.pendingMessage.prompt}
              {(props.pendingMessage.inputPreview ||
                props.pendingMessage.maskPreview) && (
                <div className='mt-2 flex flex-wrap gap-2'>
                  {props.pendingMessage.inputPreview && (
                    <img
                      alt={t('Reference image')}
                      className='size-16 rounded-md object-cover'
                      src={props.pendingMessage.inputPreview}
                    />
                  )}
                  {props.pendingMessage.maskPreview && (
                    <img
                      alt={t('Mask image')}
                      className='size-16 rounded-md object-cover'
                      src={props.pendingMessage.maskPreview}
                    />
                  )}
                </div>
              )}
            </MessageContent>
          </Message>
          <Message
            aria-label={t('Image preview')}
            from='assistant'
            role='group'
          >
            <div
              aria-label={t('Generating...')}
              aria-live='polite'
              className='w-full min-w-0 space-y-3'
              role='status'
            >
              <div className='text-muted-foreground flex items-center gap-2 text-sm'>
                <Loader />
                <Shimmer className='text-sm'>{t('Generating...')}</Shimmer>
              </div>
              <div className='bg-muted/60 aspect-square w-full max-w-xs animate-pulse rounded-xl' />
            </div>
          </Message>
        </div>
      )}
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
