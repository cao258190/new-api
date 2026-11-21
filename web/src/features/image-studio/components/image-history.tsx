/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import {
  CircleAlert,
  CircleCheck,
  Download,
  Loader2,
  Maximize2,
  RotateCcw,
} from 'lucide-react'
import { useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { translateServerText } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import type { ImageStudioMessage, ImageStudioPendingMessage } from '../types'
import { ImageViewer } from './image-viewer'

type ImageHistoryProps = {
  messages: ImageStudioMessage[]
  pendingMessage?: ImageStudioPendingMessage
  assetUrls: Record<string, string>
  isGenerating: boolean
  onDownload: (url: string, name: string) => void
  onRetry: (message: ImageStudioMessage) => void
}

/** Group each prompt, its attachments, and its results or failure in one batch. */
export function ImageHistory(props: ImageHistoryProps) {
  const { t, i18n } = useTranslation()
  const [preview, setPreview] = useState<{ messageId: string; index: number }>()
  const imageTrigger = useRef<HTMLButtonElement>(null)
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const clock = useMemo(
    () =>
      new Intl.DateTimeFormat(locale, { hour: 'numeric', minute: '2-digit' }),
    [locale]
  )
  const attempts = props.pendingMessage
    ? [...props.messages, props.pendingMessage]
    : props.messages
  const getImageUrl = (result: ImageStudioMessage['results'][number]): string =>
    (result.assetId && props.assetUrls[result.assetId]) || result.url
  const previewBatch = preview
    ? props.messages.find((message) => message.id === preview.messageId)
    : undefined

  return (
    <div className='divide-y'>
      {attempts.map((attempt) => {
        const message = 'results' in attempt ? attempt : undefined
        const pending = !message
        const failed = message?.status === 'error'
        let StatusIcon = CircleCheck
        let status = t('Completed')
        if (pending) {
          StatusIcon = Loader2
          status = t('Generating...')
        } else if (failed) {
          StatusIcon = CircleAlert
          status = t('Generation failed')
        }

        const date = new Date(attempt.createdAt)
        const hasDate = Number.isFinite(date.getTime())
        const photos = message?.results ?? []
        const attachments =
          'results' in attempt
            ? [
                attempt.inputAssetId && props.assetUrls[attempt.inputAssetId],
                attempt.maskAssetId && props.assetUrls[attempt.maskAssetId],
              ]
            : [attempt.inputPreview, attempt.maskPreview]

        return (
          <article
            aria-label={attempt.prompt}
            className='min-w-0 space-y-2 py-3 first:pt-0 last:pb-0'
            key={attempt.id}
          >
            <div className='flex items-start justify-between gap-2'>
              <div className='flex min-w-0 flex-wrap items-center gap-2'>
                <Badge
                  aria-label={pending ? status : undefined}
                  className={cn(
                    !pending && !failed && 'bg-success/10 text-success'
                  )}
                  role={pending ? 'status' : undefined}
                  variant={failed ? 'destructive' : 'secondary'}
                >
                  <StatusIcon
                    aria-hidden='true'
                    className={pending ? 'motion-safe:animate-spin' : undefined}
                  />
                  {status}
                </Badge>
                {hasDate && (
                  <time
                    className='text-muted-foreground text-xs tabular-nums'
                    dateTime={date.toISOString()}
                  >
                    {clock.format(date)}
                  </time>
                )}
              </div>
              {failed ? (
                <Button
                  disabled={props.isGenerating}
                  onClick={() => props.onRetry(message)}
                  size='sm'
                  variant='outline'
                >
                  <RotateCcw aria-hidden='true' />
                  {t('Retry')}
                </Button>
              ) : (
                <CopyButton
                  className='text-muted-foreground size-7'
                  tooltip={t('Copy prompt')}
                  value={attempt.prompt}
                />
              )}
            </div>
            <p className='text-sm leading-6 [overflow-wrap:anywhere] whitespace-pre-wrap'>
              {attempt.prompt}
            </p>
            {attachments.some(Boolean) && (
              <div className='flex flex-wrap gap-2'>
                {attachments[0] && (
                  <img
                    alt={t('Reference image')}
                    className='size-16 rounded-md object-cover'
                    loading='lazy'
                    src={attachments[0]}
                  />
                )}
                {attachments[1] && (
                  <img
                    alt={t('Mask image')}
                    className='size-16 rounded-md object-cover'
                    loading='lazy'
                    src={attachments[1]}
                  />
                )}
              </div>
            )}
            {failed && (
              <p className='text-destructive text-sm [overflow-wrap:anywhere]'>
                {translateServerText(t, message.error ?? 'Generation failed')}
              </p>
            )}
            {!failed && photos.length > 0 && (
              <>
                <div className='flex flex-wrap gap-2.5'>
                  {photos.map((photo, index) => (
                    <Button
                      aria-label={
                        photos.length === 1
                          ? t('Preview image')
                          : t('Preview image {{number}}', {
                              number: formatNumber(index + 1, locale),
                            })
                      }
                      className={cn(
                        'group relative block h-auto max-w-full cursor-zoom-in overflow-hidden border-0 p-0 hover:bg-transparent',
                        photos.length > 1 && 'max-w-[calc(50%-0.3125rem)]'
                      )}
                      key={photo.assetId ?? index}
                      onClick={(event) => {
                        imageTrigger.current = event.currentTarget
                        setPreview({ messageId: attempt.id, index })
                      }}
                      size='sm'
                      title={t('Preview image')}
                      variant='ghost'
                    >
                      <img
                        alt={t('Generated image')}
                        className={cn(
                          'h-auto w-auto max-w-full object-contain',
                          photos.length === 1
                            ? 'max-h-[clamp(12rem,45svh,28rem)]'
                            : 'max-h-[clamp(8rem,29svh,17.5rem)]'
                        )}
                        loading='lazy'
                        src={getImageUrl(photo)}
                      />
                      <span
                        aria-hidden='true'
                        className='text-foreground bg-background/90 absolute end-2 top-2 grid size-7 place-items-center rounded-md opacity-90 group-hover:opacity-100'
                      >
                        <Maximize2 className='size-3.5' />
                      </span>
                    </Button>
                  ))}
                </div>
                <Button
                  onClick={() =>
                    photos.forEach((photo) =>
                      props.onDownload(getImageUrl(photo), photo.name)
                    )
                  }
                  size='sm'
                  variant='outline'
                >
                  <Download aria-hidden='true' />
                  {photos.length === 1
                    ? t('Download')
                    : t('Download all images')}
                </Button>
              </>
            )}
            {message?.revisedPrompt && (
              <p className='text-muted-foreground text-xs [overflow-wrap:anywhere]'>
                {t('Revised prompt')}: {message.revisedPrompt}
              </p>
            )}
          </article>
        )
      })}
      <ImageViewer
        index={preview?.index ?? 0}
        onClose={() => setPreview(undefined)}
        onDownload={props.onDownload}
        onSelect={(index) =>
          setPreview((current) => current && { ...current, index })
        }
        photos={previewBatch?.results.map((photo) => ({
          ...photo,
          url: getImageUrl(photo),
        }))}
        triggerRef={imageTrigger}
      />
    </div>
  )
}
