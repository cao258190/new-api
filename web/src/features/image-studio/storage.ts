/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import type { ImageStudioAsset, ImageStudioSession } from './types'

const DB_VERSION = 1
const DB_PREFIX = 'new-api-image-studio'

type StoreName = 'sessions' | 'assets'
type TransactionResult<T> = (
  transaction: IDBTransaction,
  setResult: (value: T) => void,
  fail: (error: unknown) => void
) => void

function toError(error: unknown, fallback: string): Error {
  if (error instanceof Error) {
    return error
  }
  if (error instanceof DOMException) {
    return new Error(error.message, { cause: error })
  }
  return new Error(fallback, { cause: error })
}

function openDatabase(userId: number): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    if (typeof indexedDB === 'undefined') {
      reject(new Error('IndexedDB is unavailable'))
      return
    }

    const request = indexedDB.open(`${DB_PREFIX}-${userId}`, DB_VERSION)
    let settled = false

    request.addEventListener('upgradeneeded', () => {
      const db = request.result
      try {
        if (!db.objectStoreNames.contains('sessions')) {
          db.createObjectStore('sessions', { keyPath: 'id' })
        }
        if (!db.objectStoreNames.contains('assets')) {
          db.createObjectStore('assets', { keyPath: 'id' })
        }
      } catch (error) {
        request.transaction?.abort()
        if (!settled) {
          settled = true
          reject(toError(error, 'Could not initialize image studio storage'))
        }
      }
    })

    request.addEventListener('blocked', () => {
      if (settled) return
      settled = true
      reject(new Error('Image studio storage is blocked by another connection'))
    })
    request.addEventListener('error', () => {
      if (settled) return
      settled = true
      reject(request.error ?? new Error('Could not open image studio storage'))
    })
    request.addEventListener('success', () => {
      const db = request.result
      if (settled) {
        db.close()
        return
      }
      settled = true
      db.onversionchange = () => db.close()
      resolve(db)
    })
  })
}

function runTransaction<T>(
  userId: number,
  storeNames: StoreName | StoreName[],
  mode: IDBTransactionMode,
  action: TransactionResult<T>
): Promise<T> {
  return openDatabase(userId).then(
    (db) =>
      new Promise<T>((resolve, reject) => {
        let transaction: IDBTransaction
        try {
          transaction = db.transaction(storeNames, mode)
        } catch (error) {
          db.close()
          reject(
            toError(error, 'Could not start image studio storage transaction')
          )
          return
        }

        let settled = false
        let hasResult = false
        let result: T
        const fail = (error: unknown) => {
          if (settled) return
          settled = true
          try {
            transaction.abort()
          } catch {
            // The transaction may already have completed or aborted.
          }
          db.close()
          reject(toError(error, 'Image studio storage transaction failed'))
        }

        transaction.addEventListener('complete', () => {
          if (settled) return
          settled = true
          db.close()
          if (!hasResult) {
            reject(
              new Error(
                'Image studio storage transaction completed without a result'
              )
            )
            return
          }
          resolve(result)
        })
        transaction.addEventListener('error', () => {
          fail(
            transaction.error ??
              new Error('Image studio storage transaction failed')
          )
        })
        transaction.addEventListener('abort', () => {
          fail(
            transaction.error ??
              new Error('Image studio storage transaction was aborted')
          )
        })

        try {
          action(
            transaction,
            (value) => {
              result = value
              hasResult = true
            },
            fail
          )
        } catch (error) {
          fail(error)
        }
      })
  )
}

function observeRequest<T>(
  request: IDBRequest<T>,
  onSuccess: (value: T) => void,
  fail: (error: unknown) => void
): void {
  request.addEventListener('success', () => onSuccess(request.result))
  request.addEventListener('error', () =>
    fail(request.error ?? new Error('Image studio storage request failed'))
  )
}

function collectSessionAssetIds(session: ImageStudioSession): Set<string> {
  const assetIds = new Set<string>()
  const addAssetId = (assetId?: string) => {
    if (assetId) assetIds.add(assetId)
  }

  addAssetId(session.draft?.inputAssetId)
  addAssetId(session.draft?.maskAssetId)
  for (const message of session.messages) {
    addAssetId(message.inputAssetId)
    addAssetId(message.maskAssetId)
    for (const result of message.results) addAssetId(result.assetId)
  }
  return assetIds
}

function deleteAssets(
  transaction: IDBTransaction,
  session: ImageStudioSession,
  fail: (error: unknown) => void
): void {
  const assets = transaction.objectStore('assets')
  for (const assetId of collectSessionAssetIds(session)) {
    observeRequest(assets.delete(assetId), () => undefined, fail)
  }
}

/** Load the image studio sessions belonging to one user. */
export function loadImageSessions(
  userId: number
): Promise<ImageStudioSession[]> {
  return runTransaction(
    userId,
    'sessions',
    'readonly',
    (transaction, setResult, fail) => {
      observeRequest(
        transaction.objectStore('sessions').getAll() as IDBRequest<
          ImageStudioSession[]
        >,
        setResult,
        fail
      )
    }
  )
}

/** Persist a user's image studio session and its message history. */
export function saveImageSession(
  userId: number,
  session: ImageStudioSession
): Promise<IDBValidKey> {
  return runTransaction(
    userId,
    'sessions',
    'readwrite',
    (transaction, setResult, fail) => {
      observeRequest(
        transaction.objectStore('sessions').put(session),
        setResult,
        fail
      )
    }
  )
}

/** Delete a session and every asset referenced by its draft or messages. */
export function deleteImageSession(
  userId: number,
  sessionId: string
): Promise<void> {
  return runTransaction(
    userId,
    ['sessions', 'assets'],
    'readwrite',
    (transaction, setResult, fail) => {
      const sessions = transaction.objectStore('sessions')
      observeRequest(
        sessions.get(sessionId) as IDBRequest<ImageStudioSession | undefined>,
        (session) => {
          if (session) deleteAssets(transaction, session, fail)
          observeRequest(
            sessions.delete(sessionId),
            () => setResult(undefined),
            fail
          )
        },
        fail
      )
    }
  )
}

/** Persist an image asset as a Blob, a fallback URL, or both. */
export function saveImageAsset(
  userId: number,
  asset: ImageStudioAsset
): Promise<IDBValidKey> {
  if (!asset.blob && !asset.url) {
    return Promise.reject(
      new TypeError('An image asset requires a Blob or fallback URL')
    )
  }
  return runTransaction(
    userId,
    'assets',
    'readwrite',
    (transaction, setResult, fail) => {
      observeRequest(
        transaction.objectStore('assets').put(asset),
        setResult,
        fail
      )
    }
  )
}

/** Load a user's image asset, including its persisted Blob when available. */
export function loadImageAsset(
  userId: number,
  assetId: string
): Promise<ImageStudioAsset | undefined> {
  return runTransaction(
    userId,
    'assets',
    'readonly',
    (transaction, setResult, fail) => {
      observeRequest(
        transaction.objectStore('assets').get(assetId) as IDBRequest<
          ImageStudioAsset | undefined
        >,
        setResult,
        fail
      )
    }
  )
}

/** Delete one image asset owned by a user. */
export function deleteImageAsset(
  userId: number,
  assetId: string
): Promise<void> {
  return runTransaction(
    userId,
    'assets',
    'readwrite',
    (transaction, setResult, fail) => {
      observeRequest(
        transaction.objectStore('assets').delete(assetId),
        () => setResult(undefined),
        fail
      )
    }
  )
}

/** Delete every stored asset referenced by a session without deleting the session. */
export function deleteSessionAssets(
  userId: number,
  session: ImageStudioSession
): Promise<void> {
  return runTransaction(
    userId,
    'assets',
    'readwrite',
    (transaction, setResult, fail) => {
      deleteAssets(transaction, session, fail)
      setResult(undefined)
    }
  )
}

/** Fetch an image URL and return its response body as a Blob. */
export async function imageUrlToBlob(url: string): Promise<Blob> {
  const response = await fetch(url)
  if (!response.ok) {
    throw new Error(`Image download failed (${response.status})`)
  }
  return response.blob()
}
