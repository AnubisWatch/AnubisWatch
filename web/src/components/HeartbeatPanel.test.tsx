import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { HeartbeatPanel } from './HeartbeatPanel'
import { cronScheduleFor } from '../utils/heartbeat'

const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('../api/client', () => ({ api }))

const token = 'tok_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
const info = {
  soul_id: 's1',
  token,
  ping_url: `https://anubis.example.com/api/v1/heartbeat/${token}`,
  start_url: `https://anubis.example.com/api/v1/heartbeat/${token}/start`,
  fail_url: `https://anubis.example.com/api/v1/heartbeat/${token}/fail`,
  interval: '1h0m0s',
  grace: '30m0s',
  state: {
    soul_id: 's1',
    last_ping_at: new Date(Date.now() - 5 * 60_000).toISOString(),
    last_event: 'success',
    last_duration: 42_000_000_000,
    pings: 3,
  },
}

describe('HeartbeatPanel', () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(cleanup)

  it('shows the ping URL, a cron example and the last ping to editors', async () => {
    api.get.mockResolvedValue(info)
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    render(<HeartbeatPanel soulId="s1" canManage />)

    const url = await screen.findByLabelText('Ping URL')
    expect(api.get).toHaveBeenCalledWith('/souls/s1/heartbeat')
    expect(url).toHaveValue(info.ping_url)
    expect(screen.getByTestId('heartbeat-cron-line')).toHaveTextContent(`0 * * * * /path/to/job.sh && curl -fsS -m 10 --retry 3 ${info.ping_url}`)
    expect(screen.getByText('Expected every 1h0m0s, grace 30m0s')).toBeInTheDocument()
    expect(screen.getByTestId('heartbeat-last-ping')).toHaveTextContent('Success')
    expect(screen.getByTestId('heartbeat-last-ping')).toHaveTextContent('42.0s')

    fireEvent.click(screen.getByRole('button', { name: 'Copy ping URL' }))
    expect(writeText).toHaveBeenCalledWith(info.ping_url)
    expect(screen.getByRole('button', { name: 'Copy ping URL' })).toHaveTextContent('Copied')
  })

  it('rotates the token after confirmation', async () => {
    api.get.mockResolvedValue(info)
    const rotated = { ...info, token: 'new', ping_url: 'https://anubis.example.com/api/v1/heartbeat/new' }
    api.post.mockResolvedValue(rotated)
    render(<HeartbeatPanel soulId="s1" canManage />)
    await screen.findByLabelText('Ping URL')

    fireEvent.click(screen.getByRole('button', { name: /Rotate token/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Confirm' }))
    await waitFor(() => expect(screen.getByLabelText('Ping URL')).toHaveValue(rotated.ping_url))
    expect(api.post).toHaveBeenCalledWith('/souls/s1/heartbeat/rotate')
  })

  it('shows viewers the last ping but no secret URL', async () => {
    api.get.mockResolvedValue({ ...info, token: undefined, ping_url: undefined, fail_url: undefined, state: { soul_id: 's1', last_event: 'fail', last_message: 'disk full', last_ping_at: info.state.last_ping_at, pings: 1 } })
    render(<HeartbeatPanel soulId="s1" canManage={false} />)
    await waitFor(() => expect(screen.getByTestId('heartbeat-last-ping')).toHaveTextContent('Failed: disk full'))
    expect(screen.queryByLabelText('Ping URL')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Rotate token/ })).not.toBeInTheDocument()
  })

  it('reports a load error and a soul that never pinged', async () => {
    api.get.mockRejectedValueOnce(new Error('boom'))
    render(<HeartbeatPanel soulId="s1" canManage />)
    expect(await screen.findByRole('alert')).toHaveTextContent('boom')
    expect(screen.getByTestId('heartbeat-last-ping')).toHaveTextContent('Never')
  })

  it('derives a cron schedule from the interval', () => {
    expect(cronScheduleFor('24h0m0s')).toBe('0 3 * * *')
    expect(cronScheduleFor('6h0m0s')).toBe('0 */6 * * *')
    expect(cronScheduleFor('15m0s')).toBe('*/15 * * * *')
    expect(cronScheduleFor('1m0s')).toBe('* * * * *')
    expect(cronScheduleFor('30s')).toBe('* * * * *')
  })
})
