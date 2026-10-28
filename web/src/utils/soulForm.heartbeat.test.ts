import { describe, expect, it } from 'vitest'
import type { Soul } from '../api/client'
import {
  buildSoulPayload,
  defaultSoulFormData,
  mergeSoulEdit,
  nextSoulFormDataForType,
  soulFormDataFromSoul,
  soulTypeHasTarget,
  soulTypeOptions,
} from './soulForm'

describe('heartbeat soul form', () => {
  it('offers the type and builds a payload without a target', () => {
    expect(soulTypeOptions.map((o) => o.value)).toContain('heartbeat')
    expect(soulTypeHasTarget('heartbeat')).toBe(false)
    expect(soulTypeHasTarget('http')).toBe(true)

    const form = { ...nextSoulFormDataForType({ ...defaultSoulFormData, name: 'backup', target: 'x' }, 'heartbeat'), weight: 3600, heartbeatGrace: 600 }
    const payload = buildSoulPayload(form)
    expect(payload.type).toBe('heartbeat')
    expect(payload.target).toBe('')
    expect(payload.heartbeat).toEqual({ grace: '600s' })
    expect(buildSoulPayload({ ...form, heartbeatGrace: 0 }).heartbeat).toEqual({})
  })

  it('round-trips grace and keeps the redacted token on edit', () => {
    const soul = {
      id: 's1', name: 'backup', type: 'heartbeat', target: '', enabled: true, weight: 3600, timeout: 10,
      heartbeat: { grace: '10m0s', token: '[REDACTED]' },
    } as Soul
    const form = soulFormDataFromSoul(soul)
    expect(form.heartbeatGrace).toBe(600)
    const merged = mergeSoulEdit(soul, buildSoulPayload({ ...form, heartbeatGrace: 900 }))
    expect(merged.heartbeat).toEqual({ grace: '900s', token: '[REDACTED]' })
  })
})
