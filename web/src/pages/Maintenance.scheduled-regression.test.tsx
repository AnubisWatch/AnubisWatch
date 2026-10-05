import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { Maintenance } from './Maintenance'
const mocks = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
vi.mock('../api/client', () => ({ api: { ...mocks } }))
vi.mock('../api/hooks', () => ({ useSouls: () => ({ souls: [] }) }))
afterEach(() => vi.useRealTimers())
const windowData = { id: 'w1', name: 'Window', description: '', soul_ids: [], tags: [], start_time: '2030-01-02T10:00:00.000Z', end_time: '2030-01-02T12:00:00.000Z', recurring: '', enabled: true }

const active = { ...windowData, id: 'active', name: 'Active window', start_time: '2030-01-01T10:00:00Z', end_time: '2030-01-01T12:00:00Z' }
const disabled = { ...windowData, id: 'disabled', name: 'Disabled window', enabled: false }
const cases = [
  { name: 'future', windows: [windowData], scheduled: 1, active: 0 },
  { name: 'active', windows: [active], scheduled: 0, active: 1 },
  { name: 'disabled', windows: [disabled], scheduled: 0, active: 0 },
  { name: 'empty', windows: [], scheduled: 0, active: 0 },
  { name: 'mixed', windows: [windowData, active, disabled], scheduled: 1, active: 1 },
  { name: 'inclusive start boundary', windows: [{ ...active, start_time: '2030-01-01T11:00:00Z' }], scheduled: 0, active: 1 },
]
it.each(cases)('maintenance count agrees with filter: $name', async ({ windows, scheduled, active }) => {
  vi.setSystemTime(new Date('2030-01-01T11:00:00Z'))
  mocks.get.mockResolvedValue(windows)
  render(<Maintenance />)
  await screen.findByRole('heading', { name: 'Sacred Rest' })
  const count = (label: string) => screen.getAllByText(label).find(el => el.tagName === 'P')?.parentElement?.querySelector('p:last-child')?.textContent
  expect(count('Total')).toBe(String(windows.length))
  expect(count('Scheduled')).toBe(String(scheduled))
  expect(count('Active Now')).toBe(String(active))
  fireEvent.change(screen.getByDisplayValue('All Windows'), { target: { value: 'scheduled' } })
  expect(screen.queryAllByLabelText(/^Edit maintenance window /)).toHaveLength(scheduled)
})
