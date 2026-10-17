import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import {
  formatDate,
  formatDateTime,
  formatTime,
  getDisplayTimeZone,
  listTimeZones,
  setDisplayTimeZone,
  useDisplayTimeZone,
} from './date'

// The Timezone setting offered five hard-coded zones and was applied nowhere:
// every date followed the browser's zone regardless of the saved value.
describe('display time zone', () => {
  afterEach(() => setDisplayTimeZone(''))
  const instant = '2026-10-17T12:00:00Z'

  it('formats dates in the configured zone', () => {
    setDisplayTimeZone('Asia/Tokyo') // UTC+9
    expect(formatTime(instant)).toBe(new Date(instant).toLocaleTimeString(undefined, { timeZone: 'Asia/Tokyo' }))
    expect(formatDateTime(instant)).toContain(new Date(instant).toLocaleTimeString(undefined, { timeZone: 'Asia/Tokyo' }))
    setDisplayTimeZone('America/Los_Angeles') // UTC-7: still 17 Oct, but 05:00
    expect(formatTime(instant)).toBe(new Date(instant).toLocaleTimeString(undefined, { timeZone: 'America/Los_Angeles' }))
    expect(formatTime(instant)).not.toBe(new Date(instant).toLocaleTimeString(undefined, { timeZone: 'Asia/Tokyo' }))
    expect(formatDate(instant)).toBe(new Date(instant).toLocaleDateString(undefined, { timeZone: 'America/Los_Angeles' }))
  })

  it('falls back to local time for empty or unknown zones', () => {
    setDisplayTimeZone('Europe/Istambul')
    expect(getDisplayTimeZone()).toBe('')
    expect(formatDateTime(instant)).toBe(new Date(instant).toLocaleString())
  })

  it('lists every zone the runtime knows, UTC first', () => {
    const zones = listTimeZones()
    expect(zones[0]).toBe('UTC')
    expect(zones.length).toBeGreaterThan(300)
    for (const zone of ['Europe/Istanbul', 'America/New_York', 'Asia/Tokyo', 'Pacific/Auckland']) {
      expect(zones).toContain(zone)
    }
    expect(new Set(zones).size).toBe(zones.length)
  })

  it('re-renders subscribers when the zone changes', () => {
    const { result } = renderHook(() => useDisplayTimeZone())
    expect(result.current).toBe('')
    act(() => setDisplayTimeZone('Europe/Istanbul'))
    expect(result.current).toBe('Europe/Istanbul')
  })
})
