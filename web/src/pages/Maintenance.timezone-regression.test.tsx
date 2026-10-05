import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { Maintenance } from './Maintenance'
const mocks = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
vi.mock('../api/client', () => ({ api: { ...mocks } }))
vi.mock('../api/hooks', () => ({ useSouls: () => ({ souls: [] }) }))
afterEach(() => vi.useRealTimers())
const windowData = { id: 'w1', name: 'Window', description: '', soul_ids: [], tags: [], start_time: '2030-01-02T10:00:00.000Z', end_time: '2030-01-02T12:00:00.000Z', recurring: '', enabled: true }

it.each([
  ['winter', '2030-01-02T10:00', '2030-01-02T12:00'],
  ['summer', '2030-07-02T10:00', '2030-07-02T12:00'],
  ['midnight crossing', '2030-01-02T23:00', '2030-01-03T01:00'],
])('name-only edit preserves instants: %s', async (label, start, end) => {
  mocks.put.mockClear()
  const stored = { ...windowData, start_time: new Date(start).toISOString(), end_time: new Date(end).toISOString() }
  mocks.get.mockResolvedValue([stored])
  mocks.put.mockResolvedValue({})
  render(<Maintenance />)
  await screen.findByText(stored.name)
  fireEvent.click(screen.getByLabelText('Edit maintenance window ' + stored.name))
  const dialog = screen.getByRole('dialog')
  const times = dialog.querySelectorAll<HTMLInputElement>('input[type="datetime-local"]')
  expect(times[0].value).toBe(start)
  expect(times[1].value).toBe(end)
  fireEvent.change(within(dialog).getByDisplayValue(stored.name), { target: { value: 'Renamed window: ' + label } })
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Save Changes' })) })
  expect(mocks.put).toHaveBeenCalledWith('/maintenance/w1', expect.objectContaining({
    name: 'Renamed window: ' + label, start_time: stored.start_time, end_time: stored.end_time,
  }))
})
it('normalizes an explicit timestamp offset to the same instant when editing', async () => {
  mocks.put.mockClear()
  const stored = { ...windowData, start_time: '2030-01-02T10:00:00+05:30', end_time: '2030-01-02T12:00:00+05:30' }
  mocks.get.mockResolvedValue([stored])
  mocks.put.mockResolvedValue({})
  render(<Maintenance />)
  await screen.findByText(stored.name)
  fireEvent.click(screen.getByLabelText('Edit maintenance window ' + stored.name))
  const dialog = screen.getByRole('dialog')
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Save Changes' })) })
  expect(mocks.put).toHaveBeenCalledWith('/maintenance/w1', expect.objectContaining({
    start_time: new Date(stored.start_time).toISOString(), end_time: new Date(stored.end_time).toISOString(),
  }))
})
