import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { Journeys } from './Journeys'
const mocks = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn(), put: vi.fn(), delete: vi.fn() }))
vi.mock('../api/client', () => ({ api: mocks }))
function deferred() { let resolve!: () => void; let reject!: (e: Error) => void; const promise = new Promise<void>((a,b) => { resolve=a; reject=b }); return { promise, resolve, reject } }
function open(name = 'First draft') { fireEvent.click(screen.getByRole('button', { name: 'Create Journey' })); fireEvent.change(screen.getByLabelText('Name'), { target: { value: name } }); fireEvent.click(screen.getByRole('button', { name: 'Add Step' })) }
async function setup() { mocks.post.mockReset(); mocks.get.mockResolvedValue([]); vi.spyOn(window, 'alert').mockImplementation(() => {}); const ui=render(<Journeys />); await act(async () => {}); return ui }
function submit() { fireEvent.click(screen.getAllByRole('button', { name: 'Create Journey' }).at(-1)!) }
afterEach(() => { cleanup(); vi.restoreAllMocks() })
test('current success and failure affect their own form', async () => {
 await setup(); mocks.post.mockResolvedValue(undefined); open(); submit(); await act(async () => {}); expect(screen.queryByRole('dialog')).toBeNull(); cleanup();
 await setup(); mocks.post.mockRejectedValue(new Error('current')); open(); submit(); await act(async () => {}); expect(window.alert).toHaveBeenCalledWith('Failed to save journey: current'); expect(screen.getAllByRole('button', { name: 'Create Journey' }).at(-1)).toBeEnabled();
})
test('old success cannot close, reset or unlock a new pending form', async () => {
 const old=deferred(), current=deferred(); await setup(); mocks.post.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise); open(); submit(); fireEvent.click(screen.getByRole('button', { name: 'Cancel' })); open('New draft'); submit(); expect(mocks.post).toHaveBeenCalledTimes(2);
 await act(async () => { old.resolve(); await old.promise }); expect(screen.getByLabelText('Name')).toHaveValue('New draft'); expect(screen.getByRole('button', { name: 'Saving...' })).toBeDisabled();
 await act(async () => { current.resolve(); await current.promise }); expect(screen.queryByRole('dialog')).toBeNull();
})
test('old failure cannot alert in reopened form', async () => {
 const old=deferred(); await setup(); mocks.post.mockReturnValue(old.promise); open(); submit(); fireEvent.click(screen.getByRole('button', { name: 'Cancel' })); open('New draft'); await act(async () => { old.reject(new Error('old')); await old.promise.catch(() => {}) }); expect(window.alert).not.toHaveBeenCalled(); expect(screen.getByLabelText('Name')).toHaveValue('New draft'); expect(screen.getAllByRole('button', { name: 'Create Journey' }).at(-1)).toBeEnabled();
})
test('unmounted form ignores pending failure', async () => {
 const old=deferred(); const ui=await setup(); mocks.post.mockReturnValue(old.promise); open(); submit(); ui.unmount(); await act(async () => { old.reject(new Error('old')); await old.promise.catch(() => {}) }); expect(window.alert).not.toHaveBeenCalled();
})
