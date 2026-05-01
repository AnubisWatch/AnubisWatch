import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { formatDistanceToNow } from './date'

describe('formatDistanceToNow', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-01-15T12:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns just now for recent dates', () => {
    expect(formatDistanceToNow(new Date('2026-01-15T11:59:30Z'))).toBe('just now')
    expect(formatDistanceToNow(new Date('2026-01-15T11:59:01Z'))).toBe('just now')
  })

  it('returns minutes ago', () => {
    expect(formatDistanceToNow(new Date('2026-01-15T11:59:00Z'))).toBe('1m ago')
    expect(formatDistanceToNow(new Date('2026-01-15T11:30:00Z'))).toBe('30m ago')
    expect(formatDistanceToNow(new Date('2026-01-15T11:01:00Z'))).toBe('59m ago')
  })

  it('returns hours ago', () => {
    expect(formatDistanceToNow(new Date('2026-01-15T11:00:00Z'))).toBe('1h ago')
    expect(formatDistanceToNow(new Date('2026-01-15T06:00:00Z'))).toBe('6h ago')
    expect(formatDistanceToNow(new Date('2026-01-14T12:01:00Z'))).toBe('23h ago')
  })

  it('returns days ago', () => {
    expect(formatDistanceToNow(new Date('2026-01-14T12:00:00Z'))).toBe('1d ago')
    expect(formatDistanceToNow(new Date('2026-01-01T12:00:00Z'))).toBe('14d ago')
    expect(formatDistanceToNow(new Date('2025-12-17T12:00:00Z'))).toBe('29d ago')
  })

  it('returns months ago', () => {
    expect(formatDistanceToNow(new Date('2026-01-15T11:00:00Z').getTime() - 35 * 24 * 60 * 60 * 1000)).toBe('1mo ago')
    expect(formatDistanceToNow(new Date('2026-01-15T11:00:00Z').getTime() - 210 * 24 * 60 * 60 * 1000)).toBe('7mo ago')
    expect(formatDistanceToNow(new Date('2026-01-15T11:00:00Z').getTime() - 359 * 24 * 60 * 60 * 1000)).toBe('11mo ago')
  })

  it('returns years ago', () => {
    expect(formatDistanceToNow(new Date('2026-01-15T11:00:00Z').getTime() - 365 * 24 * 60 * 60 * 1000)).toBe('1y ago')
    expect(formatDistanceToNow(new Date('2026-01-15T11:00:00Z').getTime() - 5 * 365 * 24 * 60 * 60 * 1000)).toBe('5y ago')
  })

  it('handles future dates as just now', () => {
    expect(formatDistanceToNow(new Date('2026-01-15T12:00:01Z'))).toBe('just now')
  })

  it('accepts string input', () => {
    expect(formatDistanceToNow('2026-01-15T11:30:00Z')).toBe('30m ago')
  })
})
