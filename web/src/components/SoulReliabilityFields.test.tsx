import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { Soul } from '../api/client'
import {
  buildSoulPayload,
  defaultSoulFormData,
  mergeSoulEdit,
  soulFormDataFromSoul,
  type SoulFormData,
} from '../utils/soulForm'
import { FlappingBadge, SoulReliabilityFields } from './SoulReliabilityFields'

function renderFields(overrides: Partial<SoulFormData> = {}) {
  const setFormData = vi.fn()
  const formData = { ...defaultSoulFormData, ...overrides }
  render(<SoulReliabilityFields formData={formData} setFormData={setFormData} />)
  return { setFormData, formData }
}

describe('SoulReliabilityFields', () => {
  it('sits under an Advanced disclosure and edits retries', () => {
    const { setFormData, formData } = renderFields()
    expect(screen.getByText('Advanced').tagName).toBe('SUMMARY')
    expect(screen.getByLabelText('Retry interval (seconds)')).toBeDisabled()

    fireEvent.change(screen.getByLabelText('Retries'), { target: { value: '3' } })
    expect(setFormData).toHaveBeenLastCalledWith({ ...formData, retries: 3 })
  })

  it('enables the retry interval once retries are set', () => {
    const { setFormData, formData } = renderFields({ retries: 2 })
    const interval = screen.getByLabelText('Retry interval (seconds)')
    expect(interval).toBeEnabled()
    fireEvent.change(interval, { target: { value: '7' } })
    expect(setFormData).toHaveBeenLastCalledWith({ ...formData, retryInterval: 7 })
  })

  it('shows flap threshold and window only when flap detection is enabled', () => {
    const { setFormData, formData } = renderFields()
    expect(screen.queryByLabelText('Flap threshold (changes)')).toBeNull()
    fireEvent.change(screen.getByLabelText('Flap detection'), { target: { value: 'enabled' } })
    expect(setFormData).toHaveBeenLastCalledWith({ ...formData, flapMode: 'enabled' })
  })

  it('edits flap threshold and window', () => {
    const { setFormData, formData } = renderFields({ flapMode: 'enabled' })
    fireEvent.change(screen.getByLabelText('Flap threshold (changes)'), { target: { value: '4' } })
    expect(setFormData).toHaveBeenLastCalledWith({ ...formData, flapThreshold: 4 })
    fireEvent.change(screen.getByLabelText('Flap window (minutes)'), { target: { value: '15' } })
    expect(setFormData).toHaveBeenLastCalledWith({ ...formData, flapWindowMinutes: 15 })
  })

  it('hides retries for heartbeat souls', () => {
    renderFields({ type: 'heartbeat' })
    expect(screen.queryByLabelText('Retries')).toBeNull()
    expect(screen.getByLabelText('Flap detection')).toBeInTheDocument()
  })
})

describe('FlappingBadge', () => {
  it('renders only while flapping', () => {
    const { rerender } = render(<FlappingBadge flapping />)
    expect(screen.getByTestId('flapping-badge')).toHaveTextContent('Flapping')
    rerender(<FlappingBadge flapping={false} />)
    expect(screen.queryByTestId('flapping-badge')).toBeNull()
  })
})

describe('retry and flap payload', () => {
  it('omits the fields by default', () => {
    const payload = buildSoulPayload({ ...defaultSoulFormData, name: 'a', target: 'https://a' })
    expect(payload.retries).toBeUndefined()
    expect(payload.retry_interval).toBeUndefined()
    expect(payload.flap_detection).toBeUndefined()
  })

  it('serializes retries and flap overrides as durations', () => {
    const payload = buildSoulPayload({
      ...defaultSoulFormData,
      retries: 9,
      retryInterval: 3,
      flapMode: 'enabled',
      flapThreshold: 4,
      flapWindowMinutes: 15,
    })
    expect(payload.retries).toBe(5)
    expect(payload.retry_interval).toBe('3s')
    expect(payload.flap_detection).toEqual({ enabled: true, threshold: 4, window: '15m' })
    expect(buildSoulPayload({ ...defaultSoulFormData, flapMode: 'disabled' }).flap_detection).toEqual({ enabled: false })
  })

  it('round-trips a stored soul and clears settings on edit', () => {
    const soul = {
      id: 's1', name: 'api', type: 'http', target: 'https://a', enabled: true, weight: 60, timeout: 10,
      retries: 2, retry_interval: '4s', flap_detection: { enabled: true, threshold: 6, window: '20m0s' }, flapping: true,
    } as Soul
    const form = soulFormDataFromSoul(soul)
    expect(form).toMatchObject({ retries: 2, retryInterval: 4, flapMode: 'enabled', flapThreshold: 6, flapWindowMinutes: 20 })

    const cleared = mergeSoulEdit(soul, buildSoulPayload({ ...form, retries: 0, flapMode: 'inherit' }))
    const body = JSON.parse(JSON.stringify(cleared))
    expect(body.retries).toBeUndefined()
    expect(body.retry_interval).toBeUndefined()
    expect(body.flap_detection).toBeUndefined()
    expect(body.flapping).toBeUndefined()
  })
})
