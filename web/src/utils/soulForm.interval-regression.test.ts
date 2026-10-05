import { describe, expect, it } from 'vitest'
import type { Soul } from '../api/client'
import { buildSoulPayload, soulFormDataFromSoul } from './soulForm'

describe('ICMP interval hydration', () => {
  it.each([
    ['500ms', 0.5], ['1.5s', 1.5], ['1m30s', 90], ['1h2m3s', 3723],
    ['250µs', 0.00025], ['250μs', 0.00025], ['2s', 2], ['5', 5],
    ['', 1], ['invalid', 1], ['5garbage', 1],
  ])('preserves %s as %s seconds through edit and save', (interval, seconds) => {
    const soul: Soul = {
      id: 'ping', name: 'Ping', type: 'icmp', target: 'example.test',
      created_at: '', updated_at: '', icmp: { interval },
    }
    const form = soulFormDataFromSoul(soul)
    expect(form.icmpInterval).toBeCloseTo(seconds, 12)
    expect(buildSoulPayload(form).icmp?.interval).toBe(`${form.icmpInterval}s`)
  })
})
