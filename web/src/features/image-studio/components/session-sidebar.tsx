/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import type { ImageStudioSession } from '../types'

type ImageStudioSessionSidebarProps = {
  sessions: ImageStudioSession[]
  selectedId: string
  disabled: boolean
  onCreate: () => void
  onSelect: (session: ImageStudioSession) => void
  onRename: (session: ImageStudioSession) => void
  onDelete: (sessionId: string) => void
}

/** Render the user's locally stored image generation conversations. */
export function ImageStudioSessionSidebar(
  props: ImageStudioSessionSidebarProps
) {
  const { t } = useTranslation()

  return (
    <aside className='bg-muted/20 flex w-36 shrink-0 flex-col border-r sm:w-56 md:w-64'>
      <div className='flex items-center justify-between border-b p-3'>
        <h2 className='text-sm font-semibold'>{t('Image Studio')}</h2>
        <Button
          aria-label={t('New session')}
          disabled={props.disabled}
          onClick={props.onCreate}
          size='icon-sm'
          variant='ghost'
        >
          <Plus />
        </Button>
      </div>
      <nav
        aria-label={t('Image sessions')}
        className='flex-1 space-y-1 overflow-y-auto p-2'
      >
        {props.sessions.map((session) => (
          <div
            className={`group flex items-center gap-1 rounded-lg px-2 py-1 ${session.id === props.selectedId ? 'bg-accent' : 'hover:bg-accent/60'}`}
            key={session.id}
          >
            <button
              aria-current={
                session.id === props.selectedId ? 'page' : undefined
              }
              className='min-w-0 flex-1 truncate py-1 text-left text-sm'
              disabled={props.disabled}
              onClick={() => props.onSelect(session)}
              type='button'
            >
              {session.title === 'Untitled' ? t('Untitled') : session.title}
            </button>
            <Button
              aria-label={t('Rename session')}
              className='opacity-100 focus-visible:opacity-100 sm:opacity-0 sm:group-hover:opacity-100'
              disabled={props.disabled}
              onClick={() => props.onRename(session)}
              size='icon-sm'
              variant='ghost'
            >
              <Pencil />
            </Button>
            <Button
              aria-label={t('Delete session')}
              className='opacity-100 focus-visible:opacity-100 sm:opacity-0 sm:group-hover:opacity-100'
              disabled={props.disabled}
              onClick={() => props.onDelete(session.id)}
              size='icon-sm'
              variant='ghost'
            >
              <Trash2 />
            </Button>
          </div>
        ))}
      </nav>
    </aside>
  )
}
