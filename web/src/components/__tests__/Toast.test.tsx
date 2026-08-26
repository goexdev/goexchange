import { render, screen, act } from '@testing-library/react'
import { ToastProvider, useToast } from '../Toast'
import { describe, it, expect } from 'vitest'
import { useEffect } from 'react'

describe('Toast Component', () => {
  it('renders children correctly', () => {
    render(
      <ToastProvider>
        <div>test content</div>
      </ToastProvider>
    )
    expect(screen.getByText('test content')).toBeTruthy()
  })

  it('useToast returns object outside provider', () => {
    const TestComponent = () => {
      const toast = useToast()
      return <div>{typeof toast}</div>
    }
    render(<TestComponent />)
    expect(screen.getByText('object')).toBeTruthy()
  })

  it('shows toast on call', () => {
    const TestComponent = () => {
      const toast = useToast()
      useEffect(() => {
        toast.showToast('Test message', 'success')
      }, [])
      return null
    }
    render(
      <ToastProvider>
        <TestComponent />
      </ToastProvider>
    )
    expect(screen.getByText('Test message')).toBeTruthy()
  })
})