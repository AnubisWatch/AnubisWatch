import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, test, vi } from 'vitest'
import { Souls } from './Souls'
const mocks = vi.hoisted(() => ({ create: vi.fn(), fetch: vi.fn(), souls: [] }))
vi.mock('../stores/soulStore', () => ({ useSoulStore: () => ({ souls: mocks.souls, initialChecks: {}, fetchSouls: mocks.fetch, createSoul: mocks.create, retryInitialCheck: vi.fn(), updateSoul: vi.fn(), deleteSoul: vi.fn() }) }))
vi.mock('../hooks/useRealtimeRefresh', () => ({ useRealtimeRefresh: vi.fn() }))
function deferred() { let resolve!: () => void; let reject!: (e: Error) => void; const promise = new Promise<void>((a,b) => { resolve=a; reject=b }); return { promise, resolve, reject } }
function open(name = 'First draft') {
 fireEvent.click(screen.getByRole('button', { name: 'Add Soul' }))
 fireEvent.change(screen.getByPlaceholderText('e.g., Production API'), { target: { value: name } })
 fireEvent.change(screen.getByLabelText('HTTP URL'), { target: { value: 'https://fixture.invalid' } })
}
function setup() { mocks.create.mockReset(); vi.spyOn(window, 'alert').mockImplementation(() => {}); return render(<MemoryRouter><Souls /></MemoryRouter>) }
function submit() { fireEvent.submit(screen.getByRole('dialog').querySelector('form')!) }
afterEach(() => { cleanup(); vi.restoreAllMocks() })
test('current success and failure affect their own form', async () => {
 setup(); mocks.create.mockResolvedValue(undefined); open(); submit(); await act(async () => {}); expect(screen.queryByRole('dialog')).toBeNull(); cleanup();
 setup(); mocks.create.mockRejectedValue(new Error('current')); open(); submit(); await act(async () => {}); expect(window.alert).toHaveBeenCalledWith('Failed to create soul: current'); expect(screen.getByRole('button', { name: 'Create Soul' })).toBeEnabled();
})
test('old success cannot close, reset or unlock a new pending form', async () => {
 const old=deferred(), current=deferred(); setup(); mocks.create.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise); open(); submit(); fireEvent.click(screen.getByRole('button', { name: 'Cancel' })); open('New draft'); submit(); expect(mocks.create).toHaveBeenCalledTimes(2);
 await act(async () => { old.resolve(); await old.promise }); expect(screen.getByPlaceholderText('e.g., Production API')).toHaveValue('New draft'); expect(screen.getByRole('button', { name: 'Creating...' })).toBeDisabled();
 await act(async () => { current.resolve(); await current.promise }); expect(screen.queryByRole('dialog')).toBeNull();
})
test('old failure cannot alert in reopened form', async () => {
 const old=deferred(); setup(); mocks.create.mockReturnValue(old.promise); open(); submit(); fireEvent.click(screen.getByRole('button', { name: 'Cancel' })); open('New draft'); await act(async () => { old.reject(new Error('old')); await old.promise.catch(() => {}) }); expect(window.alert).not.toHaveBeenCalled(); expect(screen.getByPlaceholderText('e.g., Production API')).toHaveValue('New draft'); expect(screen.getByRole('button', { name: 'Create Soul' })).toBeEnabled();
})
test('unmounted form ignores pending failure', async () => {
 const old=deferred(); const ui=setup(); mocks.create.mockReturnValue(old.promise); open(); submit(); ui.unmount(); await act(async () => { old.reject(new Error('old')); await old.promise.catch(() => {}) }); expect(window.alert).not.toHaveBeenCalled();
})
