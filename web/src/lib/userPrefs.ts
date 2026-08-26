// User preferences (client-side localStorage)
// Settings like order confirmations, etc.

const KEY = 'goexchange_user_prefs'

export interface UserPrefs {
  // Trading confirmations
  confirmOrder: boolean // confirm before placing orders (default: false)
  confirmCancelOrder: boolean // confirm before cancelling orders (default: false)
}

const DEFAULTS: UserPrefs = {
  confirmOrder: false,
  confirmCancelOrder: false,
}

export function getUserPrefs(): UserPrefs {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return { ...DEFAULTS }
    const obj = JSON.parse(raw)
    return { ...DEFAULTS, ...obj }
  } catch {
    return { ...DEFAULTS }
  }
}

export function setUserPref<K extends keyof UserPrefs>(key: K, value: UserPrefs[K]): void {
  const prefs = getUserPrefs()
  prefs[key] = value
  try {
    localStorage.setItem(KEY, JSON.stringify(prefs))
  } catch {}
}

// Convenience: subscribe to changes via custom event
const listeners = new Set<() => void>()

export function subscribeUserPrefs(fn: () => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

export function notifyPrefsChanged() {
  listeners.forEach(fn => fn())
  // Also fire a window event for non-React listeners
  window.dispatchEvent(new CustomEvent('user-prefs-changed'))
}