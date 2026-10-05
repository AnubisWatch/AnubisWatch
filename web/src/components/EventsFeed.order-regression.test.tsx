import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { EventsFeed } from './EventsFeed'
const state = vi.hoisted(() => ({ messages: [] as unknown[] }))
vi.mock('../hooks/webSocketContext', () => ({ useWebSocket: () => state }))
vi.mock('../utils/date', () => ({ formatDistanceToNow: () => 'now' }))
const older = { type: 'alert', timestamp: '2030-01-01T10:00:00Z', data: { id: 'old', message: 'Older alert' } }
const newer = { type: 'alert', timestamp: '2030-01-01T10:01:00Z', data: { id: 'new', message: 'Newest alert' } }

it('limits to the latest arrival without mutating source history', () => {
  const history = Object.freeze([older, newer])
  state.messages = history as unknown as unknown[]
  render(<EventsFeed maxEvents={1} />)
  expect(screen.getByText('Newest alert')).toBeInTheDocument()
  expect(screen.queryByText('Older alert')).not.toBeInTheDocument()
  expect(history[0]).toBe(older)
})
it('dismissal fills the remaining visible slot with the next newest event', () => {
  state.messages = [older, newer]
  render(<EventsFeed maxEvents={1} />)
  fireEvent.click(screen.getByLabelText('Dismiss event'))
  expect(screen.getByText('Older alert')).toBeInTheDocument()
  expect(screen.queryByText('Newest alert')).not.toBeInTheDocument()
})
it('ignores unsupported arrivals before applying the limit', () => {
  state.messages = [older, newer, { type: 'ping', data: {} }]
  render(<EventsFeed maxEvents={1} />)
  expect(screen.getByText('Newest alert')).toBeInTheDocument()
})
it('empty history displays the empty state', () => {
  state.messages = []
  render(<EventsFeed maxEvents={1} />)
  expect(screen.getByText('No recent events')).toBeInTheDocument()
})
