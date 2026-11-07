/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  deleteImageAsset,
  deleteImageSession,
  deleteSessionAssets,
  loadImageAsset,
  loadImageSessions,
  saveImageAsset,
  saveImageSession,
} from '../storage'
import type {
  ImageStudioAsset,
  ImageStudioParams,
  ImageStudioSession,
} from '../types'

type StoreData = Map<string, unknown>
const emptyParams = {} as ImageStudioParams

class MemoryRequest<T> extends EventTarget {
  error: DOMException | null = null
  result!: T

  constructor(
    private readonly operation: () => T,
    private readonly transaction?: MemoryTransaction,
    private readonly shouldFail = false
  ) {
    super()
    queueMicrotask(() => {
      if (this.transaction?.aborted) return
      try {
        if (this.shouldFail) throw new DOMException('Request failed')
        this.result = this.operation()
        this.dispatchEvent(new Event('success'))
      } catch (error) {
        this.error =
          error instanceof DOMException
            ? error
            : new DOMException(String(error))
        this.dispatchEvent(new Event('error'))
        this.transaction?.abort(this.error)
      } finally {
        this.transaction?.finishRequest()
      }
    })
  }
}

class MemoryTransaction extends EventTarget {
  error: DOMException | null = null
  aborted = false
  private pendingRequests = 0
  private completionQueued = false
  private failNextRequest: boolean

  constructor(
    private readonly stores: Map<string, StoreData>,
    readonly mode: IDBTransactionMode,
    failNextRequest: boolean
  ) {
    super()
    this.failNextRequest = failNextRequest
    queueMicrotask(() => this.completeIfIdle())
  }

  objectStore(name: string): IDBObjectStore {
    const data = this.stores.get(name)
    if (!data) throw new DOMException(`Unknown object store: ${name}`)
    return {
      get: (key: IDBValidKey) => this.request(() => data.get(String(key))),
      getAll: () => this.request(() => [...data.values()]),
      put: (value: { id: string }) =>
        this.request(() => {
          if (this.mode !== 'readwrite') {
            throw new DOMException('Read-only transaction')
          }
          data.set(value.id, value)
          return value.id
        }),
      delete: (key: IDBValidKey) =>
        this.request(() => {
          if (this.mode !== 'readwrite') {
            throw new DOMException('Read-only transaction')
          }
          data.delete(String(key))
          return undefined
        }),
    } as unknown as IDBObjectStore
  }

  abort(reason = new DOMException('Transaction aborted')): void {
    if (this.aborted) return
    this.aborted = true
    this.error = reason
    queueMicrotask(() => this.dispatchEvent(new Event('abort')))
  }

  finishRequest(): void {
    this.pendingRequests -= 1
    this.completeIfIdle()
  }

  private request<T>(operation: () => T): IDBRequest<T> {
    this.pendingRequests += 1
    const shouldFail = this.failNextRequest
    this.failNextRequest = false
    return new MemoryRequest(
      operation,
      this,
      shouldFail
    ) as unknown as IDBRequest<T>
  }

  private completeIfIdle(): void {
    if (this.aborted || this.pendingRequests !== 0 || this.completionQueued) {
      return
    }
    this.completionQueued = true
    queueMicrotask(() => {
      this.completionQueued = false
      if (this.aborted || this.pendingRequests !== 0) return
      this.dispatchEvent(new Event('complete'))
    })
  }
}

class MemoryOpenRequest extends EventTarget {
  error: DOMException | null = null
  result!: IDBDatabase
  transaction: IDBTransaction | null = null
}

class MemoryIndexedDB {
  readonly connections: Array<{ closed: boolean }> = []
  failOpen = false
  failTransaction = false
  failRequest = false
  private readonly databases = new Map<string, Map<string, StoreData>>()

  open(name: string): IDBOpenDBRequest {
    const request = new MemoryOpenRequest()

    queueMicrotask(() => {
      if (this.failOpen) {
        request.error = new DOMException('Open failed')
        request.dispatchEvent(new Event('error'))
        return
      }

      let stores = this.databases.get(name)
      const isNew = !stores
      if (!stores) {
        stores = new Map()
        this.databases.set(name, stores)
      }

      const connection = { closed: false }
      this.connections.push(connection)
      const databaseStores = stores
      const database = {
        close: () => {
          connection.closed = true
        },
        createObjectStore: (storeName: string) => {
          databaseStores.set(storeName, new Map())
          return {} as IDBObjectStore
        },
        objectStoreNames: {
          contains: (storeName: string) => databaseStores.has(storeName),
        },
        transaction: (
          storeNames: string | string[],
          mode: IDBTransactionMode
        ) => {
          if (this.failTransaction) throw new DOMException('Transaction failed')
          const names = Array.isArray(storeNames) ? storeNames : [storeNames]
          if (names.some((storeName) => !databaseStores.has(storeName))) {
            throw new DOMException('Unknown object store')
          }
          const shouldFailRequest = this.failRequest
          this.failRequest = false
          return new MemoryTransaction(
            databaseStores,
            mode,
            shouldFailRequest
          ) as unknown as IDBTransaction
        },
      }
      request.result = database as unknown as IDBDatabase
      if (isNew) request.dispatchEvent(new Event('upgradeneeded'))
      request.dispatchEvent(new Event('success'))
    })

    return request as unknown as IDBOpenDBRequest
  }
}

function makeSession(id: string): ImageStudioSession {
  return {
    id,
    title: id,
    createdAt: 1,
    updatedAt: 1,
    messages: [],
  }
}

function makeAsset(id: string, blob?: Blob): ImageStudioAsset {
  return { id, name: `${id}.png`, mimeType: blob?.type ?? 'image/png', blob }
}

let indexedDb: MemoryIndexedDB

beforeEach(() => {
  indexedDb = new MemoryIndexedDB()
  vi.stubGlobal('indexedDB', indexedDb as unknown as IDBFactory)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('image studio storage', () => {
  it('saves, loads, updates, and deletes multiple sessions for one user', async () => {
    const first = makeSession('first')
    const second = makeSession('second')
    await saveImageSession(17, first)
    await saveImageSession(17, second)

    expect((await loadImageSessions(17)).map((session) => session.id)).toEqual([
      'first',
      'second',
    ])

    const updated = { ...first, title: 'Renamed' }
    await saveImageSession(17, updated)
    expect((await loadImageSessions(17))[0].title).toBe('Renamed')

    await deleteImageSession(17, 'second')
    expect(await loadImageSessions(17)).toEqual([updated])
    expect(indexedDb.connections.every((connection) => connection.closed)).toBe(
      true
    )
  })

  it('restores Blob assets and preserves URL-only fallbacks', async () => {
    const blob = new Blob(['image bytes'], { type: 'image/png' })
    await saveImageAsset(17, makeAsset('blob-image', blob))
    await saveImageAsset(17, {
      id: 'remote-image',
      name: 'remote.png',
      mimeType: 'image/png',
      url: 'https://images.example/remote.png',
    })

    const restoredBlob = await loadImageAsset(17, 'blob-image')
    expect(restoredBlob?.blob).toBeInstanceOf(Blob)
    expect(await restoredBlob?.blob?.text()).toBe('image bytes')
    const fallback = await loadImageAsset(17, 'remote-image')
    expect(fallback).toMatchObject({
      url: 'https://images.example/remote.png',
    })
    expect(fallback?.blob).toBeUndefined()

    await deleteImageAsset(17, 'remote-image')
    expect(await loadImageAsset(17, 'remote-image')).toBeUndefined()
  })

  it('isolates sessions and assets between users', async () => {
    await saveImageSession(17, makeSession('same-id'))
    await saveImageSession(18, {
      ...makeSession('same-id'),
      title: 'Other user',
    })
    await saveImageAsset(
      17,
      makeAsset('shared-asset-id', new Blob(['user 17']))
    )
    await saveImageAsset(
      18,
      makeAsset('shared-asset-id', new Blob(['user 18']))
    )

    expect((await loadImageSessions(17))[0].title).toBe('same-id')
    expect((await loadImageSessions(18))[0].title).toBe('Other user')
    expect(
      await (await loadImageAsset(17, 'shared-asset-id'))?.blob?.text()
    ).toBe('user 17')
    expect(
      await (await loadImageAsset(18, 'shared-asset-id'))?.blob?.text()
    ).toBe('user 18')
  })

  it('deletes assets referenced by a deleted session and retains unrelated assets', async () => {
    const session: ImageStudioSession = {
      ...makeSession('with-assets'),
      draft: {
        mode: 'edit',
        params: emptyParams,
        inputAssetId: 'draft-input',
        maskAssetId: 'draft-mask',
      },
      messages: [
        {
          id: 'message',
          prompt: 'edit',
          mode: 'edit',
          params: emptyParams,
          createdAt: 2,
          status: 'complete',
          inputAssetId: 'message-input',
          maskAssetId: 'message-mask',
          results: [
            {
              assetId: 'result-asset',
              url: 'blob:preview',
              name: 'result.png',
            },
          ],
        },
      ],
    }
    const assetIds = [
      'draft-input',
      'draft-mask',
      'message-input',
      'message-mask',
      'result-asset',
    ]
    for (const assetId of assetIds) {
      await saveImageAsset(17, makeAsset(assetId, new Blob([assetId])))
    }
    await saveImageAsset(17, makeAsset('unrelated', new Blob(['keep'])))
    await saveImageSession(17, session)
    await saveImageAsset(18, makeAsset('draft-input', new Blob(['other user'])))
    await saveImageSession(18, { ...session, id: 'other-user-session' })

    await deleteImageSession(17, session.id)

    expect(await loadImageSessions(17)).toEqual([])
    for (const assetId of assetIds) {
      expect(await loadImageAsset(17, assetId)).toBeUndefined()
    }
    expect(await loadImageAsset(17, 'unrelated')).toBeDefined()
    expect(await loadImageAsset(18, 'draft-input')).toBeDefined()
    expect((await loadImageSessions(18))[0].id).toBe('other-user-session')
  })

  it('deletes only a session’s assets when asked to keep the session', async () => {
    const session: ImageStudioSession = {
      ...makeSession('kept-session'),
      messages: [
        {
          id: 'message',
          prompt: 'draw',
          mode: 'generate',
          params: emptyParams,
          createdAt: 2,
          status: 'complete',
          results: [
            { assetId: 'asset', url: 'blob:preview', name: 'image.png' },
          ],
        },
      ],
    }
    await saveImageSession(17, session)
    await saveImageAsset(17, makeAsset('asset', new Blob(['bytes'])))

    await deleteSessionAssets(17, session)

    expect((await loadImageSessions(17))[0].id).toBe('kept-session')
    expect(await loadImageAsset(17, 'asset')).toBeUndefined()
  })

  it('rejects open and transaction failures and closes opened connections', async () => {
    indexedDb.failOpen = true
    await expect(loadImageSessions(17)).rejects.toThrow('Open failed')
    expect(indexedDb.connections).toHaveLength(0)

    indexedDb.failOpen = false
    indexedDb.failTransaction = true
    await expect(loadImageSessions(17)).rejects.toThrow('Transaction failed')
    expect(indexedDb.connections.every((connection) => connection.closed)).toBe(
      true
    )

    indexedDb.failTransaction = false
    indexedDb.failRequest = true
    await expect(loadImageSessions(17)).rejects.toThrow('Request failed')
    expect(indexedDb.connections.every((connection) => connection.closed)).toBe(
      true
    )
  })
})
