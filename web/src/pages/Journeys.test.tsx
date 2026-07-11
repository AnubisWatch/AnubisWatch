import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { Journeys } from './Journeys'

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
}))

vi.mock('../api/client', () => ({
  api: apiMocks,
}))

type JourneyFixture = {
  id: string
  name: string
  description?: string
  enabled: boolean
  weight: number
  timeout: number
  step_count: number
  last_status: 'passed' | 'failed' | 'pending' | 'unknown'
  avg_duration: number
  success_rate: number
  last_run?: string
  steps?: Array<{
    name?: string
    type?: string
    target?: string
    timeout?: number
    assertions?: Array<{ type?: string; target?: string; operator?: string; expected?: string }>
  }>
  continue_on_failure?: boolean
}

const checkout: JourneyFixture = {
  id: 'journey-1',
  name: 'Checkout Flow',
  description: 'Critical purchase path',
  enabled: true,
  weight: 60,
  timeout: 30,
  step_count: 1,
  last_status: 'passed',
  avg_duration: 1200,
  success_rate: 99,
  last_run: '2024-01-02T03:04:05Z',
  steps: [{
    name: 'Open checkout',
    type: 'http',
    target: 'https://example.com/checkout',
    timeout: 10,
    assertions: [{ type: 'status_code', target: '', operator: 'equals', expected: '200' }],
  }],
  continue_on_failure: false,
}

const variants: JourneyFixture[] = [
  checkout,
  { id: 'j2', name: 'Failed DNS', enabled: false, weight: 0, timeout: 0, step_count: 0, last_status: 'failed', avg_duration: 0, success_rate: 75 },
  { id: 'j3', name: 'Pending TCP', description: '', enabled: true, weight: 10, timeout: 5, step_count: 2, last_status: 'pending', avg_duration: 500, success_rate: 50 },
  { id: 'j4', name: 'Unknown TLS', enabled: true, weight: 20, timeout: 6, step_count: 0, last_status: 'unknown', avg_duration: 0, success_rate: 0 },
]

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

async function renderLoaded(journeys: JourneyFixture[] = [checkout]) {
  apiMocks.get.mockResolvedValue(journeys)
  render(<Journeys />)
  await screen.findByText(journeys[0]?.name ?? 'No sacred voyages charted')
}

function openCreate() {
  fireEvent.click(screen.getByRole('button', { name: 'Create Journey' }))
  return screen.getByRole('dialog')
}

function addStep(dialog: HTMLElement) {
  fireEvent.click(within(dialog).getByRole('button', { name: 'Add Step' }))
}

describe('Journeys', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.get.mockResolvedValue([checkout])
    apiMocks.post.mockResolvedValue({})
    apiMocks.put.mockResolvedValue({})
    apiMocks.delete.mockResolvedValue({})
    vi.spyOn(window, 'alert').mockImplementation(() => undefined)
    vi.spyOn(window, 'confirm').mockReturnValue(true)
  })

  it('shows loading, all journey visual variants, stats, and resets filters', async () => {
    const request = deferred<JourneyFixture[]>()
    apiMocks.get.mockReturnValueOnce(request.promise)
    render(<Journeys />)
    expect(screen.getByRole('status', { name: 'Loading journeys' })).toBeInTheDocument()
    request.resolve(variants)

    await screen.findByText('Checkout Flow')
    expect(screen.getByText('Failed DNS')).toBeInTheDocument()
    expect(screen.getByText('Pending TCP')).toBeInTheDocument()
    expect(screen.getByText('Unknown TLS')).toBeInTheDocument()
    expect(screen.getByText('75%')).toBeInTheDocument()
    expect(screen.getByText('1.2s')).toBeInTheDocument()
    expect(screen.getAllByText('0 steps').length).toBeGreaterThan(0)

    const search = screen.getByPlaceholderText('Search journeys...')
    fireEvent.change(search, { target: { value: 'critical' } })
    expect(screen.getByText('Checkout Flow')).toBeInTheDocument()
    fireEvent.change(search, { target: { value: 'missing' } })
    expect(screen.queryByText('Checkout Flow')).not.toBeInTheDocument()
    fireEvent.change(search, { target: { value: '' } })
    expect(search).toHaveValue('')

    const filter = screen.getByRole('combobox')
    for (const value of ['enabled', 'disabled', 'issues', 'all']) {
      fireEvent.change(filter, { target: { value } })
    }
    expect(screen.getByText('Failed DNS')).toBeInTheDocument()
  })

  it('renders empty, 404, Error, and non-Error initial-load outcomes and retries', async () => {
    apiMocks.get.mockResolvedValueOnce(null)
    const first = render(<Journeys />)
    await screen.findByText('No sacred voyages charted')
    fireEvent.click(screen.getByRole('button', { name: 'Chart First Voyage' }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    first.unmount()

    apiMocks.get.mockRejectedValueOnce(new Error('404 missing'))
    const second = render(<Journeys />)
    await screen.findByText('No sacred voyages charted')
    second.unmount()

    apiMocks.get.mockRejectedValueOnce(new Error('server exploded')).mockResolvedValueOnce([])
    const third = render(<Journeys />)
    expect(await screen.findByText('server exploded')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Try Again' }))
    await screen.findByText('No sacred voyages charted')
    third.unmount()

    apiMocks.get.mockRejectedValueOnce('bad response').mockResolvedValueOnce([])
    render(<Journeys />)
    expect(await screen.findByText('Unknown error')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Refresh journeys' }))
    await screen.findByText('No sacred voyages charted')
  })

  it('creates a journey with edited steps and assertions', async () => {
    await renderLoaded()
    const dialog = openCreate()
    expect(within(dialog).getByText(/No steps yet/)).toBeInTheDocument()

    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'New Journey' } })
    fireEvent.change(within(dialog).getByLabelText('Description'), { target: { value: 'A description' } })
    fireEvent.change(within(dialog).getByLabelText('Interval (s)'), { target: { value: '120' } })
    fireEvent.change(within(dialog).getByLabelText('Timeout (s)'), { target: { value: '45' } })
    fireEvent.click(within(dialog).getByLabelText('Continue on failure'))
    addStep(dialog)

    fireEvent.change(within(dialog).getByPlaceholderText('Step name'), { target: { value: 'Connect' } })
    fireEvent.change(within(dialog).getByPlaceholderText('Target URL or host:port'), { target: { value: 'example.com:443' } })
    fireEvent.change(within(dialog).getByPlaceholderText('Timeout (s)'), { target: { value: '20' } })
    const selects = within(dialog).getAllByRole('combobox')
    fireEvent.change(selects[0], { target: { value: 'tcp' } })
    fireEvent.change(selects[1], { target: { value: 'body_contains' } })
    fireEvent.change(selects[2], { target: { value: 'contains' } })
    fireEvent.change(within(dialog).getByPlaceholderText('Expected'), { target: { value: 'welcome' } })

    fireEvent.click(within(dialog).getByText('+ Add'))
    expect(within(dialog).getAllByPlaceholderText('Expected')).toHaveLength(2)
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove assertion 2' }))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create Journey' }))

    await waitFor(() => expect(apiMocks.post).toHaveBeenCalledWith('/journeys', expect.objectContaining({
      name: 'New Journey', description: 'A description', weight: 120, timeout: 45,
      step_count: 1, continue_on_failure: true, enabled: true, last_status: 'unknown',
      avg_duration: 0, success_rate: 100,
      steps: [{ name: 'Connect', type: 'tcp', target: 'example.com:443', timeout: 20,
        assertions: [{ type: 'body_contains', target: '', operator: 'contains', expected: 'welcome' }] }],
    })))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('guards save against a blank name and missing steps', async () => {
    await renderLoaded()
    const dialog = openCreate()
    const saveButton = within(dialog).getByRole('button', { name: 'Create Journey' })
    const reactPropsKey = Object.keys(saveButton).find(key => key.startsWith('__reactProps' + '$'))!
    const invokeSave = () => (saveButton as unknown as Record<string, { onClick: () => void }>)[reactPropsKey].onClick()

    await act(async () => invokeSave())
    expect(apiMocks.post).not.toHaveBeenCalled()

    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'No Steps' } })
    const updatedButton = within(dialog).getByRole('button', { name: 'Create Journey' })
    const updatedKey = Object.keys(updatedButton).find(key => key.startsWith('__reactProps' + '$'))!
    await act(async () => (updatedButton as unknown as Record<string, { onClick: () => void }>)[updatedKey].onClick())
    expect(apiMocks.post).not.toHaveBeenCalled()
  })

  it('exercises numeric fallbacks, step removal, cancel, close, and Escape resets', async () => {
    await renderLoaded()
    let dialog = openCreate()
    fireEvent.change(within(dialog).getByLabelText('Interval (s)'), { target: { value: '' } })
    fireEvent.change(within(dialog).getByLabelText('Timeout (s)'), { target: { value: '' } })
    expect(within(dialog).getByLabelText('Interval (s)')).toHaveValue(60)
    expect(within(dialog).getByLabelText('Timeout (s)')).toHaveValue(30)
    addStep(dialog)
    fireEvent.change(within(dialog).getByPlaceholderText('Timeout (s)'), { target: { value: '' } })
    expect(within(dialog).getByPlaceholderText('Timeout (s)')).toHaveValue(10)
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove step 1' }))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    dialog = openCreate()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Close dialog' }))
    dialog = openCreate()
    fireEvent.keyDown(dialog, { key: 'Enter' })
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    fireEvent.keyDown(dialog, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('reaches the filtered-empty panel when the backing collection changes between render checks', async () => {
    let lengthRead = 0
    const changingCollection = {
      get length() { return [1, 1, 0, 1][lengthRead++] ?? 1 },
      filter: () => [],
      reduce: () => 0,
    } as unknown as JourneyFixture[]
    apiMocks.get.mockResolvedValueOnce(changingCollection)
    render(<Journeys />)
    expect(await screen.findByText('No voyages match your sacred filters')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Clear Sacred Filters' }))
  })

  it('opens an edit journey with no steps and uses an empty step list', async () => {
    const noSteps: JourneyFixture = {
      id: 'no-steps', name: 'No Steps Yet', enabled: true, weight: 60, timeout: 30,
      step_count: 0, last_status: 'unknown', avg_duration: 0, success_rate: 100,
      steps: undefined,
    }
    await renderLoaded([noSteps])
    fireEvent.click(screen.getByRole('button', { name: 'Edit journey No Steps Yet' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText(/No steps yet/)).toBeInTheDocument()
  })

  it('opens sparse edit defaults and saves existing metadata', async () => {
    const sparse: JourneyFixture = {
      id: 'sparse', name: 'Sparse', description: undefined, enabled: false,
      weight: 0, timeout: 0, step_count: 2, last_status: 'failed', avg_duration: 0,
      success_rate: 0, steps: [{ assertions: undefined }, { assertions: [{}] }], continue_on_failure: true,
    }
    await renderLoaded([sparse])
    fireEvent.click(screen.getByRole('button', { name: 'Edit journey Sparse' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByLabelText('Interval (s)')).toHaveValue(60)
    expect(within(dialog).getByLabelText('Timeout (s)')).toHaveValue(30)
    expect(within(dialog).getAllByPlaceholderText('Step name')[0]).toHaveValue('')
    expect(within(dialog).getAllByPlaceholderText('Timeout (s)')[0]).toHaveValue(10)
    expect(within(dialog).getByLabelText('Continue on failure')).toBeChecked()
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Sparse Updated' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save Journey' }))
    await waitFor(() => expect(apiMocks.put).toHaveBeenCalledWith('/journeys/sparse', expect.objectContaining({
      name: 'Sparse Updated', enabled: false, last_status: 'failed', avg_duration: 0, success_rate: 0,
    })))
  })

  it('shows saving state and reports create failures for Error and non-Error values', async () => {
    await renderLoaded()
    const save = deferred<object>()
    apiMocks.post.mockReturnValueOnce(save.promise)
    let dialog = openCreate()
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Slow' } })
    addStep(dialog)
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create Journey' }))
    expect(await within(dialog).findByText('Saving...')).toBeInTheDocument()
    save.reject(new Error('cannot save'))
    await waitFor(() => expect(window.alert).toHaveBeenCalledWith('Failed to save journey: cannot save'))

    apiMocks.post.mockRejectedValueOnce('no detail')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create Journey' }))
    await waitFor(() => expect(window.alert).toHaveBeenCalledWith('Failed to save journey: Unknown error'))
  })

  it('toggles, deletes with both confirmation results, and runs successfully', async () => {
    await renderLoaded([checkout, variants[1]])
    fireEvent.click(screen.getByRole('button', { name: 'Disable journey Checkout Flow' }))
    await waitFor(() => expect(apiMocks.put).toHaveBeenCalledWith('/journeys/journey-1', { enabled: false }))
    fireEvent.click(screen.getByRole('button', { name: 'Enable journey Failed DNS' }))
    await waitFor(() => expect(apiMocks.put).toHaveBeenCalledWith('/journeys/j2', { enabled: true }))

    vi.mocked(window.confirm).mockReturnValueOnce(false).mockReturnValueOnce(true)
    fireEvent.click(screen.getByRole('button', { name: 'Delete journey Checkout Flow' }))
    expect(apiMocks.delete).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Delete journey Checkout Flow' }))
    await waitFor(() => expect(apiMocks.delete).toHaveBeenCalledWith('/journeys/journey-1'))

    const run = deferred<object>()
    apiMocks.post.mockReturnValueOnce(run.promise)
    const runButton = screen.getByRole('button', { name: 'Run journey Checkout Flow' })
    fireEvent.click(runButton)
    expect(runButton).toBeDisabled()
    run.resolve({ status: 'alive', duration: 1 })
    await waitFor(() => expect(apiMocks.post).toHaveBeenCalledWith('/journeys/journey-1/run'))
    await waitFor(() => expect(runButton).not.toBeDisabled())
    expect(screen.getByRole('button', { name: 'Run journey Failed DNS' })).toBeDisabled()
  })

  it('reports run failures for Error and non-Error values', async () => {
    await renderLoaded()
    apiMocks.post.mockRejectedValueOnce(new Error('runner down'))
    fireEvent.click(screen.getByRole('button', { name: 'Run journey Checkout Flow' }))
    await waitFor(() => expect(window.alert).toHaveBeenCalledWith('Journey run failed: runner down'))
    apiMocks.post.mockRejectedValueOnce('opaque')
    fireEvent.click(screen.getByRole('button', { name: 'Run journey Checkout Flow' }))
    await waitFor(() => expect(window.alert).toHaveBeenCalledWith('Journey run failed: Unknown error'))
  })

  it('renders every history status, duration format, step fallback, and closes via content and backdrop', async () => {
    await renderLoaded()
    apiMocks.get.mockResolvedValueOnce([
      { id: 'r1', journey_id: 'journey-1', status: 'alive', started_at: 1, completed_at: 2, duration: 999,
        steps: [{ name: 'Named', step_index: 0, status: 'alive', duration: 5, message: '' }] },
      { id: 'r2', journey_id: 'journey-1', status: 'dead', started_at: 2, completed_at: 3, duration: 1000,
        steps: [{ name: '', step_index: 1, status: 'dead', duration: 2000, message: '' }] },
      { id: 'r3', journey_id: 'journey-1', status: 'degraded', started_at: 3, completed_at: 4, duration: 2500,
        steps: [{ name: 'Slow', step_index: 2, status: 'degraded', duration: 1000, message: '' }] },
      { id: 'r4', journey_id: 'journey-1', status: 'unknown', started_at: 4, completed_at: 5, duration: 0, steps: [] },
    ])
    fireEvent.click(screen.getByRole('button', { name: 'View run history for Checkout Flow' }))
    expect(await screen.findByText('Voyage History: Checkout Flow')).toBeInTheDocument()
    const heading = screen.getByText('Voyage History: Checkout Flow')
    const panel = heading.closest('.bg-gray-900') as HTMLElement
    expect(within(panel).getByText('Passed')).toBeInTheDocument()
    expect(within(panel).getByText('Failed')).toBeInTheDocument()
    expect(within(panel).getByText('degraded')).toBeInTheDocument()
    expect(within(panel).getByText('unknown')).toBeInTheDocument()
    expect(within(panel).getByText('Step 2')).toBeInTheDocument()
    expect(within(panel).getAllByText('1.00s').length).toBeGreaterThan(0)


    const backdrop = panel.parentElement as HTMLElement
    fireEvent.click(panel)
    expect(screen.getByText('Voyage History: Checkout Flow')).toBeInTheDocument()
    fireEvent.click(backdrop)
    expect(screen.queryByText('Voyage History: Checkout Flow')).not.toBeInTheDocument()
  })

  it('handles history empty/null, 404, retryable Error, and non-Error failures', async () => {
    await renderLoaded()
    apiMocks.get.mockResolvedValueOnce(null)
    fireEvent.click(screen.getByRole('button', { name: 'View run history for Checkout Flow' }))
    await screen.findByText('No voyages have been charted yet')
    fireEvent.click(screen.getByText('Voyage History: Checkout Flow').parentElement!.parentElement!.querySelector('button')!)

    apiMocks.get.mockRejectedValueOnce(new Error('404 no runs'))
    fireEvent.click(screen.getByRole('button', { name: 'View run history for Checkout Flow' }))
    await screen.findByText('No voyages have been charted yet')
    fireEvent.click(screen.getByText('Voyage History: Checkout Flow').parentElement!.parentElement!.querySelector('button')!)

    apiMocks.get.mockRejectedValueOnce(new Error('history offline')).mockResolvedValueOnce([])
    fireEvent.click(screen.getByRole('button', { name: 'View run history for Checkout Flow' }))
    expect(await screen.findByText('history offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Try Again' }))
    await screen.findByText('No voyages have been charted yet')
    fireEvent.click(screen.getByText('Voyage History: Checkout Flow').parentElement!.parentElement!.querySelector('button')!)

    apiMocks.get.mockRejectedValueOnce('opaque history')
    fireEvent.click(screen.getByRole('button', { name: 'View run history for Checkout Flow' }))
    expect(await screen.findByText('Failed to load runs')).toBeInTheDocument()
    const heading = screen.getByText('Voyage History: Checkout Flow')
    fireEvent.click(heading.parentElement!.parentElement!.querySelector('button')!)
  })
})
