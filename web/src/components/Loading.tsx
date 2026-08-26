// Loading skeleton component - placeholder during data fetching
import { type ReactNode } from 'react'

interface SkeletonProps {
  className?: string
  rows?: number
}

export function Skeleton({ className = '', rows = 1 }: SkeletonProps) {
  return (
    <div className="space-y-2 animate-pulse">
      {Array.from({ length: rows }).map((_, i) => (
        <div
          key={i}
          className={`bg-gray-700/50 rounded h-4 ${className}`}
          style={{ width: `${60 + Math.random() * 40}%` }}
        />
      ))}
    </div>
  )
}

export function TableSkeleton({ rows = 5, cols = 4 }: { rows?: number; cols?: number }) {
  return (
    <div className="animate-pulse space-y-2">
      {Array.from({ length: rows }).map((_, r) => (
        <div key={r} className="grid gap-2" style={{ gridTemplateColumns: `repeat(${cols}, 1fr)` }}>
          {Array.from({ length: cols }).map((_, c) => (
            <div key={c} className="bg-gray-700/50 rounded h-8" />
          ))}
        </div>
      ))}
    </div>
  )
}

export function CardSkeleton() {
  return (
    <div className="card animate-pulse">
      <div className="bg-gray-700/50 rounded h-6 w-1/3 mb-3" />
      <div className="space-y-2">
        <div className="bg-gray-700/50 rounded h-4 w-full" />
        <div className="bg-gray-700/50 rounded h-4 w-5/6" />
        <div className="bg-gray-700/50 rounded h-4 w-4/6" />
      </div>
    </div>
  )
}

interface SpinnerProps {
  size?: 'sm' | 'md' | 'lg'
  className?: string
}

export function Spinner({ size = 'md', className = '' }: SpinnerProps) {
  const sizeClass = {
    sm: 'h-4 w-4',
    md: 'h-8 w-8',
    lg: 'h-12 w-12',
  }[size]
  return (
    <div className={`${sizeClass} ${className}`}>
      <svg
        className="animate-spin h-full w-full text-brand"
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        viewBox="0 0 24 24"
      >
        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
        <path
          className="opacity-75"
          fill="currentColor"
          d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
        />
      </svg>
    </div>
  )
}

export function CenterSpinner({ children }: { children?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center py-12 gap-3">
      <Spinner size="lg" />
      {children && <div className="text-sm text-muted">{children}</div>}
    </div>
  )
}
