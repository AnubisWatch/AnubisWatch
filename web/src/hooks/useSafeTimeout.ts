import { useCallback, useEffect, useRef } from 'react'

/**
 * Returns a setTimeout whose pending callbacks are cancelled when the
 * component unmounts, so short-lived UI resets ("Copied!", refresh spinners)
 * never update state of an unmounted component.
 */
export function useSafeTimeout(): (callback: () => void, ms: number) => void {
  const timers = useRef(new Set<ReturnType<typeof setTimeout>>())

  useEffect(() => {
    const pending = timers.current
    return () => {
      pending.forEach(clearTimeout)
      pending.clear()
    }
  }, [])

  return useCallback((callback: () => void, ms: number) => {
    const id = setTimeout(() => {
      timers.current.delete(id)
      callback()
    }, ms)
    timers.current.add(id)
  }, [])
}
