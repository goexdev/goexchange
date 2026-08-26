// Toast notification system - simple global toast manager
import { useEffect, useState, useCallback, createContext, useContext, type ReactNode } from 'react'

export type ToastType = 'success' | 'error' | 'info' | 'warning'

export interface Toast {
  id: string
  type: ToastType
  message: string
  duration?: number
}

interface ToastContextValue {
  toasts: Toast[]
  showToast: (message: string, type?: ToastType, duration?: number) => void
  removeToast: (id: string) => void
}

const ToastContext = createContext<ToastContextValue | null>(null)

// Safe no-op for testing/SSR when no provider
const noopToast = {
  toasts: [],
  showToast: () => {},
  removeToast: () => {},
}

export function useToast() {
  const ctx = useContext(ToastContext)
  return ctx ?? noopToast
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])

  const removeToast = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }, [])

  const showToast = useCallback(
    (message: string, type: ToastType = 'info', duration: number = 4000) => {
      const id = `toast-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`
      setToasts((prev) => [...prev, { id, type, message, duration }])
      if (duration > 0) {
        setTimeout(() => removeToast(id), duration)
      }
    },
    [removeToast]
  )

  return (
    <ToastContext.Provider value={{ toasts, showToast, removeToast }}>
      {children}
      <ToastContainer toasts={toasts} onRemove={removeToast} />
    </ToastContext.Provider>
  )
}

function ToastContainer({ toasts, onRemove }: { toasts: Toast[]; onRemove: (id: string) => void }) {
  return (
    <div className="fixed top-4 right-4 z-50 space-y-2 max-w-sm">
      {toasts.map((toast) => (
        <ToastItem key={toast.id} toast={toast} onRemove={onRemove} />
      ))}
    </div>
  )
}

function ToastItem({ toast, onRemove }: { toast: Toast; onRemove: (id: string) => void }) {
  const [exiting, setExiting] = useState(false)

  const handleClose = () => {
    setExiting(true)
    setTimeout(() => onRemove(toast.id), 200)
  }

  const bgColor = {
    success: 'bg-green-600 border-green-700',
    error: 'bg-red-600 border-red-700',
    warning: 'bg-yellow-600 border-yellow-700',
    info: 'bg-blue-600 border-blue-700',
  }[toast.type]

  const icon = {
    success: '✓',
    error: '✕',
    warning: '⚠',
    info: 'ℹ',
  }[toast.type]

  return (
    <div
      className={`${bgColor} text-white px-4 py-3 rounded shadow-lg border flex items-start gap-3 min-w-[280px] ${
        exiting ? 'opacity-0' : 'opacity-100'
      } transition-opacity`}
      role="alert"
    >
      <span className="text-lg font-bold">{icon}</span>
      <div className="flex-1 text-sm">{toast.message}</div>
      <button
        onClick={handleClose}
        className="text-white/80 hover:text-white text-lg leading-none"
        aria-label="Close"
      >
        ×
      </button>
    </div>
  )
}
