import { describe, it, expect, vi } from 'vitest'
import { render, screen, act } from '@testing-library/react'
import { Component, ReactNode } from 'react'
import { ErrorBoundary } from './ErrorBoundary'

// Helper to throw during render inside act()
function ThrowOnRender({ message }: { message: string }) {
  throw new Error(message)
}

describe('ErrorBoundary', () => {
  it('renders children when no error', () => {
    render(
      <ErrorBoundary>
        <div data-testid="child">Content</div>
      </ErrorBoundary>
    )
    expect(screen.getByTestId('child')).toBeInTheDocument()
  })

  it('catches render errors and shows fallback', async () => {
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const err = new Error('Render error')

    await act(async () => {
      render(
        <ErrorBoundary>
          <ThrowOnRender message="Render error" />
        </ErrorBoundary>
      )
    })

    expect(screen.getByText('Something went wrong')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /reload page/i })).toBeInTheDocument()
    expect(screen.getByText('Render error')).toBeInTheDocument()

    consoleSpy.mockRestore()
  })

  it('shows custom fallback when provided', async () => {
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    await act(async () => {
      render(
        <ErrorBoundary fallback={<div data-testid="custom-fallback">Custom fallback</div>}>
          <ThrowOnRender message="Custom error" />
        </ErrorBoundary>
      )
    })

    expect(screen.getByTestId('custom-fallback')).toBeInTheDocument()
    consoleSpy.mockRestore()
  })

  it('has reload page button when error occurs', async () => {
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    await act(async () => {
      render(
        <ErrorBoundary>
          <ThrowOnRender message="Reload test" />
        </ErrorBoundary>
      )
    })

    expect(screen.getByRole('button', { name: /reload page/i })).toBeInTheDocument()
    consoleSpy.mockRestore()
  })

  it('shows error message in fallback UI', async () => {
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    await act(async () => {
      render(
        <ErrorBoundary>
          <ThrowOnRender message="Specific error message" />
        </ErrorBoundary>
      )
    })

    expect(screen.getByText('Specific error message')).toBeInTheDocument()
    consoleSpy.mockRestore()
  })
})