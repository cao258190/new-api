/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { Check, ChevronDown, Pencil, Plus, Trash2 } from 'lucide-react'
import { useId, useState, type RefObject } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
} from '@/components/ui/popover'
import { cn } from '@/lib/utils'

import type { ImageStudioSession } from '../types'

type ImageStudioSessionMenuProps = {
  sessions: ImageStudioSession[]
  selectedId: string
  disabled: boolean
  triggerRef: RefObject<HTMLButtonElement | null>
  onCreate: () => void
  onSelect: (session: ImageStudioSession) => void
  onRename: (id: string, title: string) => void
  onDelete: (sessionId: string) => void
}

/** Keep conversation history and management within the active session title. */
export function ImageStudioSessionMenu(props: ImageStudioSessionMenuProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [rename, setRename] = useState<{ id: string; title: string }>()
  const formId = useId()
  const title = props.sessions.find(
    (session) => session.id === props.selectedId
  )?.title

  return (
    <>
      <Popover open={open} onOpenChange={setOpen}>
        <h1 className='w-full min-w-0'>
          <PopoverTrigger
            disabled={props.disabled}
            ref={props.triggerRef}
            render={
              <Button
                className='-ms-2 max-w-full px-2 font-semibold'
                variant='ghost'
              />
            }
            title={t('Image sessions')}
          >
            <span className='truncate'>
              {!title || title === 'Untitled' ? t('New session') : title}
            </span>
            <ChevronDown
              aria-hidden='true'
              className='text-muted-foreground size-3.5 transition-transform in-aria-expanded:rotate-180 motion-reduce:transition-none'
            />
          </PopoverTrigger>
        </h1>
        <PopoverContent
          align='start'
          className='w-[min(19rem,calc(100vw-1.5rem))] gap-0 overflow-hidden p-0 motion-reduce:animate-none'
          collisionPadding={8}
          sideOffset={6}
        >
          <div className='flex min-h-11 items-center justify-between gap-2 border-b py-1 ps-3 pe-2'>
            <PopoverTitle>{t('Image sessions')}</PopoverTitle>
            <Button
              disabled={props.disabled}
              onClick={() => {
                setOpen(false)
                props.onCreate()
              }}
              size='sm'
              variant='outline'
            >
              <Plus aria-hidden='true' />
              {t('New session')}
            </Button>
          </div>
          <nav
            aria-label={t('Image sessions')}
            className='max-h-[min(44dvh,18rem)] space-y-1 overflow-y-auto overscroll-contain p-2'
          >
            {props.sessions.map((session) => {
              const active = session.id === props.selectedId
              const name =
                session.title === 'Untitled' ? t('Untitled') : session.title
              return (
                <div
                  className={cn(
                    'flex min-w-0 items-center gap-0.5 rounded-md',
                    active && 'bg-accent'
                  )}
                  key={session.id}
                >
                  <Button
                    aria-current={active || undefined}
                    className='min-w-0 flex-1 justify-start text-start font-normal'
                    disabled={props.disabled}
                    onClick={() => {
                      setOpen(false)
                      props.onSelect(session)
                    }}
                    title={name}
                    variant='ghost'
                  >
                    <span className='flex-1 truncate'>{name}</span>
                    {active && (
                      <Check
                        aria-hidden='true'
                        className='text-primary size-3.5'
                      />
                    )}
                  </Button>
                  <Button
                    aria-label={t('Rename session')}
                    disabled={props.disabled}
                    onClick={() => {
                      setOpen(false)
                      setRename({
                        id: session.id,
                        title:
                          session.title === 'Untitled' ? '' : session.title,
                      })
                    }}
                    size='icon-sm'
                    title={t('Rename session')}
                    variant='ghost'
                  >
                    <Pencil aria-hidden='true' />
                  </Button>
                  <Button
                    aria-label={t('Delete session')}
                    className='hover:text-destructive'
                    disabled={props.disabled}
                    onClick={() => {
                      setOpen(false)
                      props.onDelete(session.id)
                    }}
                    size='icon-sm'
                    title={t('Delete session')}
                    variant='ghost'
                  >
                    <Trash2 aria-hidden='true' />
                  </Button>
                </div>
              )
            })}
          </nav>
        </PopoverContent>
      </Popover>
      <Dialog
        contentClassName='sm:max-w-sm'
        finalFocus={props.triggerRef}
        footer={
          <Button
            disabled={props.disabled || !rename?.title.trim()}
            form={formId}
            type='submit'
          >
            {t('Save')}
          </Button>
        }
        onOpenChange={(nextOpen) => !nextOpen && setRename(undefined)}
        open={Boolean(rename)}
        showCloseButton
        title={t('Rename session')}
      >
        <form
          id={formId}
          onSubmit={(event) => {
            event.preventDefault()
            if (!rename || props.disabled) return
            const title = rename.title.trim()
            if (!title) return
            props.onRename(rename.id, title)
            setRename(undefined)
          }}
        >
          <Input
            aria-label={t('Name')}
            maxLength={64}
            onChange={(event) => {
              const title = event.target.value
              setRename((current) => current && { ...current, title })
            }}
            value={rename?.title ?? ''}
          />
        </form>
      </Dialog>
    </>
  )
}
