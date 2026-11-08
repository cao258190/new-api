/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { useQuery } from '@tanstack/react-query'
import { Sparkles } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { EmptyState } from '@/components/empty-state'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import { getImageGroups, getImageModels, requestImages } from './api'
import { GenerationPanel } from './components/generation-panel'
import { ImageHistory } from './components/image-history'
import { ImageStudioSessionSidebar } from './components/session-sidebar'
import {
  deleteImageAsset,
  deleteImageSession,
  imageUrlToBlob,
  loadImageAsset,
  loadImageSessions,
  saveImageAsset,
  saveImageSession,
} from './storage'
import type {
  ImageStudioGroupOption,
  ImageStudioMessage,
  ImageStudioMode,
  ImageStudioModelOption,
  ImageStudioParams,
  ImageStudioSession,
} from './types'

const DEFAULT_PARAMS: ImageStudioParams = {
  model: 'gpt-image-1',
  group: 'default',
  prompt: '',
  n: 1,
  size: '1024x1024',
  align16: true,
  quality: 'auto',
  background: '',
  output_format: 'png',
  output_compression: '',
  moderation: '',
  input_fidelity: '',
  watermark: false,
}

const EMPTY_MODELS: ImageStudioModelOption[] = []
const EMPTY_GROUPS: ImageStudioGroupOption[] = []

function createSession(): ImageStudioSession {
  const now = Date.now()
  return {
    id: crypto.randomUUID(),
    title: 'Untitled',
    createdAt: now,
    updatedAt: now,
    messages: [],
  }
}

/** Render the local multi-session GPT Image workspace. */
export function ImageStudio() {
  const { t } = useTranslation()
  const translateRef = useRef(t)
  translateRef.current = t
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [sessions, setSessions] = useState<ImageStudioSession[]>([])
  const [sessionsOwnerId, setSessionsOwnerId] = useState<number>()
  const [selectedId, setSelectedId] = useState('')
  const [params, setParams] = useState<ImageStudioParams>(DEFAULT_PARAMS)
  const [mode, setMode] = useState<ImageStudioMode>('generate')
  const [inputImage, setInputImage] = useState<File>()
  const [mask, setMask] = useState<File>()
  const [inputAssetId, setInputAssetId] = useState<string>()
  const [maskAssetId, setMaskAssetId] = useState<string>()
  const [inputPreview, setInputPreview] = useState('')
  const [maskPreview, setMaskPreview] = useState('')
  const [isGenerating, setIsGenerating] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string>()
  const [assetUrls, setAssetUrls] = useState<Record<string, string>>({})
  const draftTimer = useRef<number | null>(null)
  const inputPreviewRef = useRef(inputPreview)
  const maskPreviewRef = useRef(maskPreview)
  const assetUrlsRef = useRef(assetUrls)
  inputPreviewRef.current = inputPreview
  maskPreviewRef.current = maskPreview
  assetUrlsRef.current = assetUrls

  /** Release browser object URLs and input files before changing local sessions. */
  const clearSessionImages = useCallback(() => {
    if (inputPreviewRef.current) URL.revokeObjectURL(inputPreviewRef.current)
    if (maskPreviewRef.current) URL.revokeObjectURL(maskPreviewRef.current)
    Object.values(assetUrlsRef.current).forEach((url) =>
      URL.revokeObjectURL(url)
    )
    inputPreviewRef.current = ''
    maskPreviewRef.current = ''
    assetUrlsRef.current = {}
    setInputImage(undefined)
    setMask(undefined)
    setInputAssetId(undefined)
    setMaskAssetId(undefined)
    setInputPreview('')
    setMaskPreview('')
    setAssetUrls({})
  }, [])

  const visibleSessions = sessionsOwnerId === userId ? sessions : []
  const selectedSession = visibleSessions.find(
    (session) => session.id === selectedId
  )
  const selectedSessionRef = useRef(selectedSession)
  selectedSessionRef.current = selectedSession
  const groupsQuery = useQuery({
    queryKey: ['image-studio-groups', userId],
    queryFn: getImageGroups,
    enabled: Boolean(userId),
  })
  const modelsQuery = useQuery({
    queryKey: ['image-studio-models', userId, params.group],
    queryFn: () => getImageModels(params.group),
    enabled: Boolean(userId && params.group),
  })
  const models = modelsQuery.data ?? EMPTY_MODELS
  const groups = groupsQuery.data ?? EMPTY_GROUPS

  useEffect(() => {
    clearSessionImages()
    setSessions([])
    setSelectedId('')
    setMode('generate')
    setParams(DEFAULT_PARAMS)
    setSessionsOwnerId(undefined)
    if (!userId) return
    let cancelled = false
    loadImageSessions(userId)
      .then((loaded) => {
        if (cancelled) return
        const next = loaded.sort((a, b) => b.updatedAt - a.updatedAt)
        const sessionsToUse = next.length ? next : [createSession()]
        const firstSession = sessionsToUse[0]
        if (!firstSession) return
        setSessions(sessionsToUse)
        setSessionsOwnerId(userId)
        setSelectedId(firstSession.id)
        if (!next.length) {
          void saveImageSession(userId, firstSession).catch(() =>
            toast.error(
              translateRef.current('Image history could not be saved locally')
            )
          )
        }
      })
      .catch(() => {
        if (cancelled) return
        const next = createSession()
        setSessions([next])
        setSessionsOwnerId(userId)
        setSelectedId(next.id)
      })
    return () => {
      cancelled = true
    }
  }, [clearSessionImages, userId])

  useEffect(() => {
    const preferred =
      models.find((model) => model.value === 'gpt-image-1') ?? models[0]
    if (preferred && !models.some((model) => model.value === params.model)) {
      setParams((previous) => ({ ...previous, model: preferred.value }))
    }
  }, [models, params.model])

  useEffect(() => {
    if (
      !groups.length ||
      groups.some((group) => group.value === params.group)
    ) {
      return
    }
    const fallback =
      groups.find((group) => group.value === 'default')?.value ??
      groups[0]?.value ??
      ''
    setParams((previous) => ({ ...previous, group: fallback }))
  }, [groups, params.group])

  useEffect(() => {
    if (!userId) return
    let cancelled = false
    const createdUrls: string[] = []
    const sessionToRestore = selectedSessionRef.current
    if (!sessionToRestore) return
    clearSessionImages()
    const assetIds = sessionToRestore.messages.flatMap((message) =>
      message.results.flatMap((result) =>
        result.assetId ? [result.assetId] : []
      )
    )
    Promise.all(
      assetIds.map(async (assetId) => {
        const asset = await loadImageAsset(userId, assetId)
        if (!asset?.blob) return null
        const url = URL.createObjectURL(asset.blob)
        createdUrls.push(url)
        return [assetId, url] as const
      })
    )
      .then((entries) => {
        if (cancelled) {
          createdUrls.forEach((url) => URL.revokeObjectURL(url))
          return
        }
        setAssetUrls((previous) => {
          const next = { ...previous }
          entries.forEach((entry) => {
            if (entry) next[entry[0]] = entry[1]
          })
          return next
        })
      })
      .catch(() =>
        toast.warning(
          translateRef.current('Some local images could not be restored')
        )
      )

    const latestMessage = sessionToRestore.messages.at(-1)
    const draft = sessionToRestore.draft
    const restoredMode = draft?.mode ?? latestMessage?.mode ?? 'generate'
    const restoredParams = draft?.params ?? latestMessage?.params
    const restoredInputId = draft?.inputAssetId ?? latestMessage?.inputAssetId
    const restoredMaskId = draft?.maskAssetId ?? latestMessage?.maskAssetId
    if (restoredParams) {
      setMode(restoredMode)
      setParams(restoredParams)
      setInputAssetId(restoredInputId)
      setMaskAssetId(restoredMaskId)
      setInputImage(undefined)
      setMask(undefined)
      setInputPreview('')
      setMaskPreview('')
      const restoreInput = async (
        assetId: string | undefined,
        setFile: (file?: File) => void,
        setPreview: (preview: string) => void
      ) => {
        if (!assetId) return
        const asset = await loadImageAsset(userId, assetId)
        if (cancelled || !asset?.blob) return
        const url = URL.createObjectURL(asset.blob)
        createdUrls.push(url)
        setFile(new File([asset.blob], asset.name, { type: asset.mimeType }))
        setPreview(url)
      }
      void Promise.all([
        restoreInput(restoredInputId, setInputImage, setInputPreview),
        restoreInput(restoredMaskId, setMask, setMaskPreview),
      ]).catch(() =>
        toast.warning(
          translateRef.current('Some local images could not be restored')
        )
      )
    } else {
      setMode('generate')
      setParams((previous) => ({
        ...DEFAULT_PARAMS,
        group: previous.group,
        model: previous.model,
      }))
      setInputImage(undefined)
      setMask(undefined)
      setInputAssetId(undefined)
      setMaskAssetId(undefined)
      setInputPreview('')
      setMaskPreview('')
    }

    return () => {
      cancelled = true
      createdUrls.forEach((url) => URL.revokeObjectURL(url))
    }
  }, [clearSessionImages, selectedId, userId])

  useEffect(
    () => () => {
      if (inputPreviewRef.current) URL.revokeObjectURL(inputPreviewRef.current)
      if (maskPreviewRef.current) URL.revokeObjectURL(maskPreviewRef.current)
      Object.values(assetUrlsRef.current).forEach((url) =>
        URL.revokeObjectURL(url)
      )
    },
    []
  )

  const updateParams = <K extends keyof ImageStudioParams>(
    key: K,
    value: ImageStudioParams[K]
  ) => {
    setParams((previous) => ({ ...previous, [key]: value }))
  }

  const persistSession = async (session: ImageStudioSession) => {
    if (!userId) return
    try {
      await saveImageSession(userId, session)
    } catch {
      toast.error(t('Image history could not be saved locally'))
    }
  }

  useEffect(() => {
    if (!userId || !selectedSession) return
    if (draftTimer.current !== null) window.clearTimeout(draftTimer.current)
    const updated = {
      ...selectedSession,
      draft: { mode, params, inputAssetId, maskAssetId },
    }
    const draftIsCurrent =
      selectedSession.draft?.mode === mode &&
      selectedSession.draft.params === params &&
      selectedSession.draft.inputAssetId === inputAssetId &&
      selectedSession.draft.maskAssetId === maskAssetId
    if (!draftIsCurrent) {
      setSessions((previous) =>
        previous.map((session) =>
          session.id === updated.id ? updated : session
        )
      )
    }
    draftTimer.current = window.setTimeout(() => {
      draftTimer.current = null
      void saveImageSession(userId, updated).catch(() =>
        toast.error(t('Image history could not be saved locally'))
      )
    }, 300)
    return () => {
      if (draftTimer.current !== null) {
        window.clearTimeout(draftTimer.current)
        draftTimer.current = null
      }
    }
  }, [
    inputAssetId,
    maskAssetId,
    mode,
    params,
    selectedId,
    selectedSession,
    t,
    userId,
  ])

  const handleNewSession = () => {
    clearSessionImages()
    const next = createSession()
    setSessions((previous) => [next, ...previous])
    void persistSession(next)
    setSelectedId(next.id)
    setParams((previous) => ({
      ...DEFAULT_PARAMS,
      group: previous.group,
      model: previous.model,
    }))
    setMode('generate')
  }

  const handleRename = (session: ImageStudioSession) => {
    const title = window.prompt(t('Rename session'), session.title)?.trim()
    if (!title) return
    const updated = { ...session, title, updatedAt: Date.now() }
    setSessions((previous) =>
      previous.map((item) => (item.id === session.id ? updated : item))
    )
    void persistSession(updated)
  }

  const handleDelete = async () => {
    if (!userId || !deleteTarget) return
    try {
      await deleteImageSession(userId, deleteTarget)
    } catch {
      toast.error(t('Image history could not be deleted locally'))
      return
    }
    if (selectedId === deleteTarget) clearSessionImages()
    const remaining = sessions.filter((session) => session.id !== deleteTarget)
    const next = remaining.length ? remaining : [createSession()]
    setSessions(next)
    setSelectedId((current) =>
      current === deleteTarget ? (next[0]?.id ?? '') : current
    )
    setDeleteTarget(undefined)
  }

  const setUploadedFile = async (
    file: File | undefined,
    kind: 'input' | 'mask'
  ) => {
    if (!file) return
    if (!userId || !selectedSession) return
    if (file.size > 50 * 1024 * 1024) {
      toast.error(t('Images must be smaller than 50 MB'))
      return
    }
    if (
      !['image/png', 'image/jpeg', 'image/webp'].includes(file.type) ||
      (kind === 'mask' && file.type !== 'image/png')
    ) {
      toast.error(
        kind === 'mask'
          ? t('Mask images must be PNG files')
          : t('Images must be PNG, JPEG, or WebP files')
      )
      return
    }
    const preview = URL.createObjectURL(file)
    const previousAssetId = kind === 'input' ? inputAssetId : maskAssetId
    const assetId = crypto.randomUUID()
    if (kind === 'input') {
      if (inputPreviewRef.current) URL.revokeObjectURL(inputPreviewRef.current)
      inputPreviewRef.current = preview
      setInputImage(file)
      setInputPreview(preview)
      setInputAssetId(assetId)
    } else {
      if (maskPreviewRef.current) URL.revokeObjectURL(maskPreviewRef.current)
      maskPreviewRef.current = preview
      setMask(file)
      setMaskPreview(preview)
      setMaskAssetId(assetId)
    }
    try {
      await saveImageAsset(userId, {
        id: assetId,
        name: file.name,
        mimeType: file.type,
        blob: file,
      })
    } catch {
      toast.warning(t('This image could not be saved for offline use'))
      URL.revokeObjectURL(preview)
      if (kind === 'input') {
        inputPreviewRef.current = ''
        setInputImage(undefined)
        setInputPreview('')
        setInputAssetId(undefined)
      } else {
        maskPreviewRef.current = ''
        setMask(undefined)
        setMaskPreview('')
        setMaskAssetId(undefined)
      }
      return
    }
    const updated: ImageStudioSession = {
      ...selectedSession,
      draft: {
        mode,
        params,
        inputAssetId: kind === 'input' ? assetId : inputAssetId,
        maskAssetId: kind === 'mask' ? assetId : maskAssetId,
      },
    }
    setSessions((previous) =>
      previous.map((session) => (session.id === updated.id ? updated : session))
    )
    try {
      await saveImageSession(userId, updated)
    } catch {
      toast.error(t('Image history could not be saved locally'))
      return
    }
    if (previousAssetId) {
      const isUsedByHistory = selectedSession.messages.some(
        (message) =>
          message.inputAssetId === previousAssetId ||
          message.maskAssetId === previousAssetId
      )
      if (!isUsedByHistory) {
        await deleteImageAsset(userId, previousAssetId).catch(() => undefined)
      }
    }
  }

  const removeUploadedFile = (kind: 'input' | 'mask') => {
    if (!selectedSession || !userId) return
    const assetId = kind === 'input' ? inputAssetId : maskAssetId
    const preview = kind === 'input' ? inputPreview : maskPreview
    if (preview) URL.revokeObjectURL(preview)
    if (kind === 'input') {
      inputPreviewRef.current = ''
      setInputImage(undefined)
      setInputPreview('')
      setInputAssetId(undefined)
    } else {
      maskPreviewRef.current = ''
      setMask(undefined)
      setMaskPreview('')
      setMaskAssetId(undefined)
    }
    const nextInputAssetId = kind === 'input' ? undefined : inputAssetId
    const nextMaskAssetId = kind === 'mask' ? undefined : maskAssetId
    const updated: ImageStudioSession = {
      ...selectedSession,
      draft: {
        mode,
        params,
        inputAssetId: nextInputAssetId,
        maskAssetId: nextMaskAssetId,
      },
    }
    setSessions((previous) =>
      previous.map((session) => (session.id === updated.id ? updated : session))
    )
    void saveImageSession(userId, updated)
      .then(async () => {
        if (!assetId) return
        const usedByHistory = selectedSession.messages.some(
          (message) =>
            message.inputAssetId === assetId || message.maskAssetId === assetId
        )
        if (!usedByHistory) await deleteImageAsset(userId, assetId)
      })
      .catch(() => toast.error(t('Image history could not be saved locally')))
  }

  const handleGenerate = async (
    retryMessage?: ImageStudioMessage,
    retryInput?: File,
    retryMask?: File
  ) => {
    if (!userId || !selectedSession) return
    const requestParams = retryMessage?.params ?? params
    const requestMode = retryMessage?.mode ?? mode
    const requestInput = retryMessage ? retryInput : inputImage
    const requestMask = retryMessage ? retryMask : mask
    const prompt = requestParams.prompt.trim()
    if (!requestParams.model) {
      toast.error(t('No GPT Image models are available for this group'))
      return
    }
    if (!prompt) {
      toast.error(t('Enter a prompt before generating'))
      return
    }
    if (requestMode === 'edit' && !requestInput) {
      toast.error(t('Upload a reference image for image-to-image mode'))
      return
    }

    setIsGenerating(true)
    const baseMessage: ImageStudioMessage = {
      id: crypto.randomUUID(),
      prompt,
      mode: requestMode,
      params: requestParams,
      createdAt: Date.now(),
      status: 'complete',
      inputAssetId: retryMessage?.inputAssetId ?? inputAssetId,
      maskAssetId: retryMessage?.maskAssetId ?? maskAssetId,
      results: [],
    }
    try {
      const response = await requestImages(
        requestParams,
        requestMode,
        requestInput,
        requestMask
      )
      if (!response.items.length) {
        throw new Error(t('The image service returned no images'))
      }
      const results: ImageStudioMessage['results'] = []
      for (const item of response.items) {
        const assetId = crypto.randomUUID()
        try {
          const blob = await imageUrlToBlob(item.url)
          await saveImageAsset(userId, {
            id: assetId,
            name: item.name,
            mimeType: blob.type,
            blob,
          })
          const objectUrl = URL.createObjectURL(blob)
          setAssetUrls((previous) => ({ ...previous, [assetId]: objectUrl }))
          results.push({ assetId, url: item.url, name: item.name })
        } catch {
          results.push({ url: item.url, name: item.name })
          toast.warning(t('This image could not be saved for offline use'))
        }
      }
      const updated: ImageStudioSession = {
        ...selectedSession,
        title: selectedSession.messages.length
          ? selectedSession.title
          : prompt.slice(0, 32),
        updatedAt: Date.now(),
        draft: {
          mode: requestMode,
          params: requestParams,
          inputAssetId: baseMessage.inputAssetId,
          maskAssetId: baseMessage.maskAssetId,
        },
        messages: [
          ...selectedSession.messages,
          {
            ...baseMessage,
            revisedPrompt: response.revisedPrompt,
            results,
          },
        ],
      }
      setSessions((previous) =>
        previous.map((item) => (item.id === updated.id ? updated : item))
      )
      await persistSession(updated)
    } catch (error) {
      const updated: ImageStudioSession = {
        ...selectedSession,
        updatedAt: Date.now(),
        draft: {
          mode: requestMode,
          params: requestParams,
          inputAssetId: baseMessage.inputAssetId,
          maskAssetId: baseMessage.maskAssetId,
        },
        messages: [
          ...selectedSession.messages,
          {
            ...baseMessage,
            status: 'error',
            error:
              error instanceof Error ? error.message : t('Generation failed'),
          },
        ],
      }
      setSessions((previous) =>
        previous.map((item) => (item.id === updated.id ? updated : item))
      )
      await persistSession(updated)
      handleServerError(error, t('Generation failed'))
    } finally {
      setIsGenerating(false)
    }
  }

  const download = async (url: string, name: string) => {
    try {
      const blob = await imageUrlToBlob(url)
      const objectUrl = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = objectUrl
      anchor.download = name
      anchor.click()
      window.setTimeout(() => URL.revokeObjectURL(objectUrl), 1000)
    } catch {
      window.open(url, '_blank', 'noopener,noreferrer')
    }
  }

  const retry = async (message: ImageStudioMessage) => {
    if (!userId) return
    try {
      const input = message.inputAssetId
        ? await loadImageAsset(userId, message.inputAssetId)
        : undefined
      const maskAsset = message.maskAssetId
        ? await loadImageAsset(userId, message.maskAssetId)
        : undefined
      if (message.mode === 'edit' && !input?.blob) {
        toast.error(t('Upload a reference image for image-to-image mode'))
        return
      }
      if (message.maskAssetId && !maskAsset?.blob) {
        toast.error(t('Some local images could not be restored'))
        return
      }
      const inputFile = input?.blob
        ? new File([input.blob], input.name, { type: input.mimeType })
        : undefined
      const maskFile = maskAsset?.blob
        ? new File([maskAsset.blob], maskAsset.name, {
            type: maskAsset.mimeType,
          })
        : undefined
      await handleGenerate(message, inputFile, maskFile)
    } catch {
      toast.error(t('Some local images could not be restored'))
    }
  }

  if (!userId) {
    return <EmptyState title={t('Please sign in to use Image Studio')} />
  }

  return (
    <div className='flex min-h-0 flex-1 overflow-hidden'>
      <ImageStudioSessionSidebar
        disabled={isGenerating || !selectedSession}
        onCreate={handleNewSession}
        onDelete={setDeleteTarget}
        onRename={handleRename}
        onSelect={(session) => {
          if (session.id === selectedId) return
          clearSessionImages()
          setSelectedId(session.id)
        }}
        selectedId={selectedId}
        sessions={visibleSessions}
      />
      <section className='flex min-w-0 flex-1 flex-col overflow-y-auto'>
        <div className='mx-auto flex w-full max-w-6xl flex-1 flex-col gap-5 p-4 md:p-6'>
          <div>
            <h1 className='text-xl font-semibold'>{t('Image Studio')}</h1>
            <p className='text-muted-foreground text-sm'>
              {t('Create and edit images with GPT Image models')}
            </p>
          </div>
          {selectedSession?.messages.length ? (
            <ImageHistory
              assetUrls={assetUrls}
              isGenerating={isGenerating}
              messages={selectedSession.messages}
              onDownload={(url, name) => void download(url, name)}
              onRetry={retry}
            />
          ) : (
            <EmptyState
              className='min-h-32'
              description={t(
                'Describe an image or upload a reference to begin.'
              )}
              icon={Sparkles}
              title={t('Start a new image')}
            />
          )}
          <GenerationPanel
            groups={groups}
            inputPreview={inputPreview}
            isGenerating={isGenerating}
            isOptionsLoading={modelsQuery.isLoading || groupsQuery.isLoading}
            isSessionReady={Boolean(selectedSession)}
            maskPreview={maskPreview}
            mode={mode}
            models={models}
            onGenerate={() => void handleGenerate()}
            onInputImageChange={(file) => setUploadedFile(file, 'input')}
            onMaskChange={(file) => setUploadedFile(file, 'mask')}
            onModeChange={setMode}
            onParamChange={updateParams}
            onRemoveInput={() => removeUploadedFile('input')}
            onRemoveMask={() => removeUploadedFile('mask')}
            params={params}
          />
        </div>
      </section>
      <ConfirmDialog
        confirmText={t('Delete')}
        destructive
        desc={t('Delete this local session and its history?')}
        handleConfirm={() => void handleDelete()}
        onOpenChange={(open) => !open && setDeleteTarget(undefined)}
        open={Boolean(deleteTarget)}
        title={t('Delete session')}
      />
    </div>
  )
}
