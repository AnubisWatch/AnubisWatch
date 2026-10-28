import { describe, expect, it } from 'vitest'
import { ApiError } from '../api/client'
import { describeApiError, isQuotaError, quotaErrorMessage } from './quota'

const quota = (body: Record<string, unknown>) =>
  new ApiError(409, { error: 'workspace quota exceeded', code: 'quota_exceeded', ...body })

describe('quota error helpers', () => {
  it.each([
    [quota({ quota: 'max_souls', limit: 5, current: 5 }), 'Quota reached: this workspace may hold at most 5 souls. Delete one first, or ask an administrator to raise tenants.default_quotas.max_souls.'],
    [quota({ quota: 'max_journeys', limit: 1 }), 'Quota reached: this workspace may hold at most 1 journey. Delete one first, or ask an administrator to raise tenants.default_quotas.max_journeys.'],
    [quota({ quota: 'max_alert_channels' }), 'Quota reached for alert channels in this workspace. Delete one first, or ask an administrator to raise tenants.default_quotas.max_alert_channels.'],
    [quota({ quota: 'check_interval_min', limit: '30s' }), 'This workspace requires a check interval of at least 30s. Choose a longer interval, or ask an administrator to lower tenants.default_quotas.check_interval_min.'],
  ])('describes quota refusal %#', (err, expected) => {
    expect(isQuotaError(err)).toBe(true)
    expect(quotaErrorMessage(err)).toBe(expected)
    expect(describeApiError(err, 'fallback')).toBe(expected)
  })

  it.each([
    [new ApiError(409, { error: 'status page slug already in use' }), 'status page slug already in use'],
    [new Error('boom'), 'boom'],
    ['not an error', 'fallback'],
  ])('leaves other failures alone %#', (err, expected) => {
    expect(isQuotaError(err)).toBe(false)
    expect(quotaErrorMessage(err)).toBeNull()
    expect(describeApiError(err, 'fallback')).toBe(expected)
  })

  it('keeps the structured fields of an API error', () => {
    const err = quota({ quota: 'max_souls', limit: 2, current: 3 })
    expect(err).toBeInstanceOf(Error)
    expect(err.message).toBe('workspace quota exceeded')
    expect(err).toMatchObject({ status: 409, code: 'quota_exceeded', quota: 'max_souls', limit: 2, current: 3 })
    expect(new ApiError(500).message).toBe('HTTP 500')
  })
})
