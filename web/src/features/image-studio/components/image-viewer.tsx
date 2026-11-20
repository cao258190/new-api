/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import {
  ChevronLeft,
  ChevronRight,
  Download,
  Image as ImageIcon,
  Loader2,
  Maximize,
  Minus,
  Plus,
  X,
} from 'lucide-react'
import {
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type RefObject,
} from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import '@/styles/image-studio.css'

import type { ImageStudioMessage } from '../types'

type ImageViewerProps = {
  photos?: ImageStudioMessage['results']
  index: number
  triggerRef: RefObject<HTMLButtonElement | null>
  onSelect: (index: number) => void
  onDownload: (url: string, name: string) => void
  onClose: () => void
}

type ImageMeasurement =
  | { url: string; failed: true }
  | { url: string; width: number; height: number; failed?: false }

const CENTER = { x: 0.5, y: 0.5 }
const MAX_ZOOM = 4
const ZOOM_STEP = 1.25

/** Inspect a generation batch at its native aspect ratio, with zoom relative to the fitted image. */
export function ImageViewer(props: ImageViewerProps) {
  const { t, i18n } = useTranslation()
  const viewport = useRef<HTMLDivElement>(null)
  const closeButton = useRef<HTMLButtonElement>(null)
  const activeThumb = useRef<HTMLButtonElement>(null)
  const center = useRef(CENTER)
  const drag = useRef<{
    x: number
    y: number
    left: number
    top: number
  } | null>(null)
  const [measurement, setMeasurement] = useState<ImageMeasurement>()
  const [zoom, setZoom] = useState(1)
  const { index, photos } = props
  const photo = photos?.[index]
  const current = measurement?.url === photo?.url ? measurement : undefined
  const failed = current?.failed
  const size = current && !current.failed ? current : undefined
  const multiple = Boolean(photos && photos.length > 1)
  const panning = Boolean(size && zoom > 1)
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const number = (value: number) => formatNumber(value, locale)

  useLayoutEffect(() => {
    const view = viewport.current
    if (!view || !size) return
    view.scrollLeft = view.scrollWidth * center.current.x - view.clientWidth / 2
    view.scrollTop =
      view.scrollHeight * center.current.y - view.clientHeight / 2
  }, [index, size, zoom])

  useLayoutEffect(() => {
    activeThumb.current?.scrollIntoView?.({
      block: 'nearest',
      inline: 'nearest',
    })
  }, [index, photo?.url])

  const changeZoom = (next: number): void => {
    if (!size) return
    const bounded = Math.max(1, Math.min(MAX_ZOOM, next))
    const view = viewport.current
    center.current =
      bounded > 1 && view
        ? {
            x:
              (view.scrollLeft + view.clientWidth / 2) /
              (view.scrollWidth || 1),
            y:
              (view.scrollTop + view.clientHeight / 2) /
              (view.scrollHeight || 1),
          }
        : CENTER
    setZoom(bounded)
  }

  const select = (next: number): void => {
    if (!photos || next < 0 || next >= photos.length || next === index) return
    center.current = CENTER
    setZoom(1)
    props.onSelect(next)
  }

  const controls = [
    {
      label: t('Zoom out'),
      Icon: Minus,
      next: zoom / ZOOM_STEP,
      disabled: !size || zoom <= 1,
    },
    {
      label: t('Zoom in'),
      Icon: Plus,
      next: zoom * ZOOM_STEP,
      disabled: !size || zoom >= MAX_ZOOM,
    },
    { label: t('Fit to window'), Icon: Maximize, next: 1, disabled: !size },
  ]

  return (
    <Dialog
      open={Boolean(photo)}
      onOpenChange={(open) => {
        if (open) return
        center.current = CENTER
        setZoom(1)
        setMeasurement(undefined)
        props.onClose()
      }}
    >
      <DialogContent
        className='image-studio-viewer motion-reduce:animate-none'
        data-batch={multiple}
        finalFocus={props.triggerRef}
        initialFocus={closeButton}
        onKeyDown={(event) => {
          if (
            !multiple ||
            event.altKey ||
            event.ctrlKey ||
            event.metaKey ||
            event.shiftKey
          ) {
            return
          }
          if (event.target === viewport.current && panning) return
          if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
            event.preventDefault()
            select(index + (event.key === 'ArrowLeft' ? -1 : 1))
          }
        }}
        showCloseButton={false}
        style={
          {
            '--viewer-aspect': size ? size.width / size.height : 1.5,
            '--viewer-zoom': zoom,
          } as CSSProperties
        }
      >
        {photo && photos && (
          <>
            <header className='image-studio-viewer-header'>
              <span
                aria-hidden='true'
                className='bg-muted grid size-8 place-items-center rounded-md'
              >
                <ImageIcon className='size-4' />
              </span>
              <div className='min-w-0'>
                <DialogTitle className='text-sm leading-5 font-semibold'>
                  {t('Image preview')}
                </DialogTitle>
                <div className='text-muted-foreground flex flex-wrap items-center gap-x-3 text-xs'>
                  <span className='truncate' title={photo.name}>
                    {photo.name}
                  </span>
                  {size && (
                    <span className='shrink-0 tabular-nums'>
                      {number(size.width)} × {number(size.height)}
                    </span>
                  )}
                </div>
              </div>
              <Button
                aria-label={t('Download')}
                onClick={() => props.onDownload(photo.url, photo.name)}
                title={t('Download')}
                variant='outline'
              >
                <Download aria-hidden='true' />
                <span className='hidden sm:inline'>{t('Download')}</span>
              </Button>
              <DialogClose
                aria-label={t('Close')}
                render={
                  <Button ref={closeButton} size='icon' variant='ghost' />
                }
                title={t('Close')}
              >
                <X aria-hidden='true' />
              </DialogClose>
            </header>

            <div className='image-studio-viewer-stage'>
              {multiple && (
                <Button
                  aria-label={t('Previous image')}
                  className='justify-self-center rounded-full'
                  disabled={index === 0}
                  focusableWhenDisabled
                  onClick={() => select(index - 1)}
                  size='icon'
                  title={t('Previous image')}
                  variant='outline'
                >
                  <ChevronLeft aria-hidden='true' />
                </Button>
              )}
              <div
                aria-label={t('Generated image')}
                className='image-studio-viewer-viewport'
                data-panning={panning}
                data-ready={Boolean(size)}
                onDoubleClick={() => changeZoom(zoom === 1 ? 2 : 1)}
                onLostPointerCapture={() => {
                  drag.current = null
                }}
                onPointerDown={(event) => {
                  if (
                    !panning ||
                    event.pointerType !== 'mouse' ||
                    event.button !== 0
                  ) {
                    return
                  }
                  const view = event.currentTarget
                  view.setPointerCapture(event.pointerId)
                  drag.current = {
                    x: event.clientX,
                    y: event.clientY,
                    left: view.scrollLeft,
                    top: view.scrollTop,
                  }
                }}
                onPointerMove={(event) => {
                  const view = event.currentTarget
                  if (
                    !drag.current ||
                    !view.hasPointerCapture(event.pointerId)
                  ) {
                    return
                  }
                  view.scrollLeft =
                    drag.current.left + drag.current.x - event.clientX
                  view.scrollTop =
                    drag.current.top + drag.current.y - event.clientY
                }}
                ref={viewport}
                role='region'
                tabIndex={panning ? 0 : undefined}
              >
                <div className='image-studio-viewer-plane'>
                  {!size && (
                    <p
                      className='text-muted-foreground flex items-center gap-2 px-3 text-center text-sm'
                      role='status'
                    >
                      {!failed && (
                        <Loader2
                          aria-hidden='true'
                          className='size-4 motion-safe:animate-spin'
                        />
                      )}
                      {failed ? t('Failed to load image') : t('Loading...')}
                    </p>
                  )}
                  <img
                    alt={t('Generated image')}
                    className='image-studio-viewer-image'
                    draggable={false}
                    key={photo.url}
                    onError={() =>
                      setMeasurement({ url: photo.url, failed: true })
                    }
                    onLoad={(event) => {
                      const { naturalWidth, naturalHeight } =
                        event.currentTarget
                      if (naturalWidth && naturalHeight) {
                        setMeasurement({
                          url: photo.url,
                          width: naturalWidth,
                          height: naturalHeight,
                        })
                      } else {
                        setMeasurement({ url: photo.url, failed: true })
                      }
                    }}
                    src={photo.url}
                  />
                </div>
              </div>
              {multiple && (
                <Button
                  aria-label={t('Next image')}
                  className='justify-self-center rounded-full'
                  disabled={index === photos.length - 1}
                  focusableWhenDisabled
                  onClick={() => select(index + 1)}
                  size='icon'
                  title={t('Next image')}
                  variant='outline'
                >
                  <ChevronRight aria-hidden='true' />
                </Button>
              )}
            </div>

            <footer className='image-studio-viewer-footer'>
              {multiple && (
                <>
                  <output
                    aria-live='polite'
                    className='text-muted-foreground text-xs whitespace-nowrap tabular-nums'
                  >
                    {number(index + 1)} / {number(photos.length)}
                  </output>
                  <div
                    aria-label={t('Image preview')}
                    className='image-studio-viewer-thumbs'
                    role='group'
                  >
                    {photos.map((item, itemIndex) => (
                      <Button
                        aria-label={t('Preview image {{number}}', {
                          number: number(itemIndex + 1),
                        })}
                        aria-pressed={itemIndex === index}
                        className='border-border aria-pressed:border-primary aria-pressed:ring-primary h-10 w-11 border p-0.5 aria-pressed:ring-1'
                        key={item.assetId ?? itemIndex}
                        onClick={() => select(itemIndex)}
                        ref={itemIndex === index ? activeThumb : undefined}
                        size='icon'
                        variant='ghost'
                      >
                        <img
                          alt=''
                          className='pointer-events-none size-full rounded-sm object-contain'
                          draggable={false}
                          loading='lazy'
                          src={item.url}
                        />
                      </Button>
                    ))}
                  </div>
                </>
              )}
              <div
                aria-label={t('Zoom')}
                className='bg-muted/40 flex items-center justify-self-end rounded-lg border px-0.5'
                role='group'
              >
                {controls.map((control, controlIndex) => (
                  <div
                    className='flex items-center last:ms-0.5 last:border-s last:ps-0.5'
                    key={control.label}
                  >
                    {controlIndex === 1 && (
                      <output className='min-w-11 text-center text-xs tabular-nums'>
                        {new Intl.NumberFormat(locale, {
                          style: 'percent',
                        }).format(zoom)}
                      </output>
                    )}
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <Button
                            aria-label={control.label}
                            className='pointer-coarse:size-9'
                            disabled={control.disabled}
                            focusableWhenDisabled
                            onClick={() => changeZoom(control.next)}
                            size='icon-sm'
                            variant='ghost'
                          />
                        }
                      >
                        <control.Icon aria-hidden='true' />
                      </TooltipTrigger>
                      <TooltipContent>{control.label}</TooltipContent>
                    </Tooltip>
                  </div>
                ))}
              </div>
            </footer>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
