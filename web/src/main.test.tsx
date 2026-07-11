import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({ render: vi.fn(), applyTheme: vi.fn(), register: vi.fn() }))
vi.mock('react-dom/client', () => ({ createRoot: () => ({ render: mocks.render }) }))
vi.mock('./App', () => ({ default: () => null }))
vi.mock('./stores/themeStore', () => ({
  useThemeStore: { getState: () => ({ theme: 'dark' }) },
  applyTheme: mocks.applyTheme,
}))

describe('main bootstrap', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.clearAllMocks()
    document.body.innerHTML = '<div id="root"></div>'
  })

  it('applies the saved theme and mounts without service worker support', async () => {
    const original = Object.getOwnPropertyDescriptor(navigator, 'serviceWorker')
    delete (navigator as Navigator & { serviceWorker?: unknown }).serviceWorker
    await import('./main')
    expect(mocks.applyTheme).toHaveBeenCalledWith('dark')
    expect(mocks.render).toHaveBeenCalledOnce()
    if (original) Object.defineProperty(navigator, 'serviceWorker', original)
  })

  it('registers the worker, handles updates, reload acceptance, and registration failure', async () => {
    const listeners = new Map<string, EventListener>()
    const workerListeners = new Map<string, EventListener>()
    const worker = { state: 'installing', addEventListener: vi.fn((name: string, cb: EventListener) => workerListeners.set(name, cb)) }
    const registration = { installing: worker, addEventListener: vi.fn((name: string, cb: EventListener) => listeners.set(name, cb)) }
    mocks.register.mockResolvedValueOnce(registration).mockRejectedValueOnce(new Error('no worker'))
    Object.defineProperty(navigator, 'serviceWorker', { configurable: true, value: { register: mocks.register, controller: {} } })
    const addSpy = vi.spyOn(window, 'addEventListener')
    vi.stubGlobal('confirm', vi.fn(() => false))

    await import('./main')
    const load = addSpy.mock.calls.find(([name]) => name === 'load')?.[1] as EventListener
    await load(new Event('load'))
    listeners.get('updatefound')?.(new Event('updatefound'))
    worker.state = 'installed'
    workerListeners.get('statechange')?.(new Event('statechange'))
    expect(confirm).toHaveBeenCalled()

    vi.mocked(confirm).mockReturnValue(true)
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    workerListeners.get('statechange')?.(new Event('statechange'))
    expect(confirm).toHaveBeenCalledTimes(2)
    consoleError.mockRestore()
    await load(new Event('load'))
  })

  it('does not reload an installed worker when there is no controller', async () => {
    const listeners = new Map<string, EventListener>()
    const workerListeners = new Map<string, EventListener>()
    const worker = { state: 'installed', addEventListener: (name: string, cb: EventListener) => workerListeners.set(name, cb) }
    mocks.register.mockResolvedValue({ installing: worker, addEventListener: (name: string, cb: EventListener) => listeners.set(name, cb) })
    Object.defineProperty(navigator, 'serviceWorker', { configurable: true, value: { register: mocks.register, controller: null } })
    const addSpy = vi.spyOn(window, 'addEventListener')
    vi.stubGlobal('confirm', vi.fn())
    await import('./main')
    const load = addSpy.mock.calls.find(([name]) => name === 'load')?.[1] as EventListener
    await load(new Event('load'))
    listeners.get('updatefound')?.(new Event('updatefound'))
    workerListeners.get('statechange')?.(new Event('statechange'))
    expect(confirm).not.toHaveBeenCalled()
  })

  it('ignores updatefound when no installing worker exists', async () => {
    const listeners = new Map<string, EventListener>()
    mocks.register.mockResolvedValue({ installing: null, addEventListener: (name: string, cb: EventListener) => listeners.set(name, cb) })
    Object.defineProperty(navigator, 'serviceWorker', { configurable: true, value: { register: mocks.register, controller: null } })
    const addSpy = vi.spyOn(window, 'addEventListener')
    await import('./main')
    const load = addSpy.mock.calls.find(([name]) => name === 'load')?.[1] as EventListener
    await load(new Event('load'))
    expect(() => listeners.get('updatefound')?.(new Event('updatefound'))).not.toThrow()
  })
})
