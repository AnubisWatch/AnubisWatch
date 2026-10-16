import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from './client'

function lastBody(): Record<string, unknown> {
  const call = vi.mocked(fetch).mock.calls.at(-1)
  return JSON.parse(String((call?.[1] as RequestInit).body))
}

describe('alert rule serialization', () => {
  afterEach(() => vi.restoreAllMocks())

  // Editing a rule in the UI used to collapse it to one condition and drop
  // scope, escalation and auto_resolve, because PUT replaces the record.
  it('replaces only the primary condition and keeps the rest of the rule', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve({}) })
    await api.put('/rules/r1', {
      id: 'r1',
      name: 'API down',
      enabled: true,
      channels: ['c1'],
      severity: 'critical',
      condition: 'downtime',
      threshold: 0,
      consecutive: 5,
      auto_resolve: true,
      scope: { type: 'tag', tags: ['prod'] },
      cooldown: '15m',
      escalation: { stages: [{ wait: '10m', channels: ['c2'] }] },
      conditions: [
        { type: 'consecutive_failures', threshold: 3 },
        { type: 'threshold', metric: 'latency', operator: '>', value: 2000 },
      ],
    })
    const body = lastBody()
    const conditions = body.conditions as Record<string, unknown>[]
    expect(conditions).toHaveLength(2)
    expect(conditions[0]).toMatchObject({ type: 'consecutive_failures', threshold: 5 })
    expect(conditions[1]).toMatchObject({ metric: 'latency', value: 2000 })
    expect(body.scope).toEqual({ type: 'tag', tags: ['prod'] })
    expect(body.cooldown).toBe('15m')
    expect(body.escalation).toEqual({ stages: [{ wait: '10m', channels: ['c2'] }] })
    expect(body.auto_resolve).toBe(true)
    expect(body).not.toHaveProperty('condition')
  })

  it('fills scope and cooldown defaults for a new rule', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 201, json: () => Promise.resolve({}) })
    await api.post('/rules', { name: 'new', enabled: true, channels: ['c1'], severity: 'warning', condition: 'response_time', threshold: 500 })
    const body = lastBody()
    expect(body.scope).toEqual({ type: 'all' })
    expect(body.cooldown).toBe('5m')
    expect((body.conditions as unknown[]).length).toBe(1)
  })
})
