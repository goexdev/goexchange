// Cloud-synced favorites with localStorage fallback

import * as api from './api'

const KEY = 'goexchange_favorites'

function readLocal(): string[] {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return []
    const arr = JSON.parse(raw)
    return Array.isArray(arr) ? arr.filter(x => typeof x === 'string') : []
  } catch {
    return []
  }
}

function writeLocal(list: string[]) {
  try {
    localStorage.setItem(KEY, JSON.stringify(list))
  } catch {}
}

// Get favorites - tries cloud first if logged in, falls back to local
export async function getFavorites(): Promise<string[]> {
  const token = localStorage.getItem('goexchange_token')
  if (!token) return readLocal()
  try {
    const res = await api.listFavorites()
    writeLocal(res.favorites || [])
    return res.favorites || []
  } catch {
    return readLocal()
  }
}

export function isFavorite(pair: string): boolean {
  // Synchronous check against local cache (sync fallback for rendering)
  return readLocal().includes(pair)
}

// Toggle favorite - cloud first, local fallback
export async function toggleFavorite(pair: string): Promise<boolean> {
  const token = localStorage.getItem('goexchange_token')
  const local = readLocal()
  const wasFavorite = local.includes(pair)

  // Optimistic local update
  let next: string[]
  if (wasFavorite) {
    next = local.filter(p => p !== pair)
  } else {
    next = [...local, pair]
  }
  writeLocal(next)

  // Cloud sync (if logged in)
  if (token) {
    try {
      if (wasFavorite) {
        await api.removeFavorite(pair)
      } else {
        await api.addFavorite(pair)
      }
    } catch {
      // Cloud failed - keep local change, will retry next load
    }
  }

  return !wasFavorite
}

// Sync from cloud (call after login)
export async function refreshFavorites(): Promise<string[]> {
  return getFavorites()
}
export function getLocalFavorites(): string[] {
  return readLocal()
}
