import { renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useSafeTimeout } from './useSafeTimeout'

// "Copied!" and refresh-spinner resets used to fire after unmount and set
// state on a dead component (React reports it once the environment is gone).
describe('useSafeTimeout', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('runs callbacks while mounted', () => {
    const fn = vi.fn()
    const { result } = renderHook(() => useSafeTimeout())
    result.current(fn, 100)
    vi.advanceTimersByTime(100)
    expect(fn).toHaveBeenCalledOnce()
  })

  it('cancels pending callbacks on unmount', () => {
    const fn = vi.fn()
    const { result, unmount } = renderHook(() => useSafeTimeout())
    result.current(fn, 100)
    unmount()
    vi.advanceTimersByTime(1000)
    expect(fn).not.toHaveBeenCalled()
  })
})
