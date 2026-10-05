import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { SoulProtocolFields } from './SoulProtocolFields'
import { buildSoulPayload, defaultSoulFormData } from '../utils/soulForm'
it.each([0.5, 1.5, 2.25, 2])('preserves ICMP interval %s seconds through input and payload', interval => {
  const formData = { ...defaultSoulFormData, type: 'icmp' as const, icmpInterval: interval }
  const setFormData = vi.fn()
  render(<SoulProtocolFields formData={formData} setFormData={setFormData} />)
  const input = screen.getByLabelText('Interval Seconds') as HTMLInputElement
  expect(input.checkValidity()).toBe(true)
  fireEvent.change(input, { target: { value: String(interval + 0.25) } })
  const changed = setFormData.mock.lastCall?.[0]
  expect(changed.icmpInterval).toBe(interval + 0.25)
  expect(changed.icmpCount).toBe(formData.icmpCount)
  expect(buildSoulPayload(changed).icmp?.interval).toBe(`${interval + 0.25}s`)
})
it('blank ICMP interval still uses the existing serializer default', () => {
  const setFormData = vi.fn()
  render(<SoulProtocolFields formData={{ ...defaultSoulFormData, type: 'icmp' }} setFormData={setFormData} />)
  fireEvent.change(screen.getByLabelText('Interval Seconds'), { target: { value: '' } })
  expect(buildSoulPayload(setFormData.mock.lastCall?.[0]).icmp?.interval).toBe('1s')
})
