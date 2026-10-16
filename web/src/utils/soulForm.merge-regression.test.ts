import { describe, expect, it } from 'vitest'
import type { Soul } from '../api/client'
import { buildSoulPayload, mergeSoulEdit, soulFormDataFromSoul } from './soulForm'

const original = {
  id: 's1',
  name: 'API',
  type: 'http',
  target: 'https://api.example.com/health',
  enabled: true,
  weight: 60,
  timeout: 10,
  status: 'healthy',
  latency: 42,
  created_at: '2026-01-01T00:00:00Z',
  http: {
    method: 'GET',
    valid_status: [200],
    headers: { Authorization: '[REDACTED]' },
    body_regex: 'ok',
    json_path: { '$.status': 'up' },
    follow_redirects: true,
  },
} as unknown as Soul

// Loose view of the merged payload for assertions.
type Merged = Record<string, unknown> & { http: Record<string, unknown>; tcp?: unknown }

describe('mergeSoulEdit', () => {
  // Saving the edit form used to wipe every field it does not render.
  it('keeps fields the form does not show', () => {
    const form = { ...soulFormDataFromSoul(original), name: 'API v2', weight: 30 }
    const merged = mergeSoulEdit(original, buildSoulPayload(form)) as unknown as Merged
    expect(merged.name).toBe('API v2')
    expect(merged.weight).toBe(30)
    expect(merged.http.body_regex).toBe('ok')
    expect(merged.http.json_path).toEqual({ '$.status': 'up' })
    expect(merged.http.follow_redirects).toBe(true)
    // The redaction placeholder goes back so the server keeps the secret.
    expect(merged.http.headers).toEqual({ Authorization: '[REDACTED]' })
    for (const key of ['id', 'status', 'latency', 'created_at']) {
      expect(merged).not.toHaveProperty(key)
    }
  })

  it('takes form values over the original', () => {
    const form = { ...soulFormDataFromSoul(original), httpMethod: 'HEAD', httpValidStatus: '200, 204' }
    const merged = mergeSoulEdit(original, buildSoulPayload(form)) as unknown as Merged
    expect(merged.http.method).toBe('HEAD')
    expect(merged.http.valid_status).toEqual([200, 204])
  })

  it('drops the old protocol block when the type changes', () => {
    const form = { ...soulFormDataFromSoul(original), type: 'tcp' as const, target: 'api.example.com:443' }
    const merged = mergeSoulEdit(original, buildSoulPayload(form)) as unknown as Merged
    expect(merged.type).toBe('tcp')
    expect(merged).not.toHaveProperty('http')
    expect(merged.tcp).toBeDefined()
  })
})
