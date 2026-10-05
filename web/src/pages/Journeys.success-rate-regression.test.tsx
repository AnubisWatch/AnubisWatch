import { render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { Journeys } from './Journeys'
const api = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('../api/client', () => ({ api }))
const journey = { id: 'j1', name: 'Checkout', enabled: true, weight: 60, timeout: 30, step_count: 1, last_status: 'passed', avg_duration: 1, success_rate: 100 }

it.each([
  { name: 'positive control', rates: [100], expected: '100%' },
  { name: 'all failed', rates: [0], expected: '0%' },
  { name: 'mixed', rates: [100, 0], expected: '50%' },
  { name: 'empty', rates: [], expected: '0%' },
  { name: 'fractional', rates: [25, 50, 0], expected: '25%' },
])('average success includes every journey: $name', async ({ rates, expected }) => {
  api.get.mockResolvedValue(rates.map((success_rate, i) => ({ ...journey, id: String(i), success_rate })))
  render(<Journeys />)
  await screen.findByText('Avg Success')
  expect(screen.getByText('Avg Success').parentElement?.querySelector('p:last-child')?.textContent).toBe(expected)
})
