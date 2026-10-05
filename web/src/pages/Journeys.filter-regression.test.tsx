import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { Journeys } from './Journeys'
const api = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('../api/client', () => ({ api }))
const journey = { id: 'j1', name: 'Checkout', enabled: true, weight: 60, timeout: 30, step_count: 1, last_status: 'passed', avg_duration: 1, success_rate: 100 }

it.each(['search', 'disabled', 'issues'])('shows and clears empty results for %s', async mode => {
  api.get.mockResolvedValue([journey])
  render(<Journeys />)
  await screen.findByText(journey.name)
  if (mode === 'search') {
    fireEvent.change(screen.getByPlaceholderText('Search journeys...'), { target: { value: 'missing' } })
  } else {
    fireEvent.change(screen.getByRole('combobox'), { target: { value: mode } })
  }
  expect(screen.queryByText(journey.name)).not.toBeInTheDocument()
  expect(screen.getByText('No voyages match your sacred filters')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Clear Sacred Filters' }))
  expect(screen.getByText(journey.name)).toBeInTheDocument()
  expect(screen.getByPlaceholderText('Search journeys...')).toHaveValue('')
  expect(screen.getByRole('combobox')).toHaveValue('all')
})
