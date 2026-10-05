import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { StatusPages } from './StatusPages'
const mocks=vi.hoisted(() => ({ create: vi.fn(), update: vi.fn(), pages:[], souls:[] }))
vi.mock('../api/hooks', () => ({ useStatusPages: () => ({ pages:mocks.pages,loading:false,createPage:mocks.create,updatePage:mocks.update,deletePage:vi.fn(),refetch:vi.fn() }),useSouls: () => ({ souls:mocks.souls }) }))
function deferred() { let resolve!: () => void; let reject!: (e: Error) => void; const promise = new Promise<void>((a,b) => { resolve=a; reject=b }); return { promise, resolve, reject } }
function open(name='First draft') { fireEvent.click(screen.getByRole('button', { name:'Create Page' })); fireEvent.change(screen.getByLabelText('Name'), { target:{ value:name } }); fireEvent.change(screen.getByLabelText('Slug'), { target:{ value:'fixture' } }) }
async function setup() { mocks.create.mockReset(); const ui=render(<StatusPages />); await act(async () => {}); return ui }
function submit() { fireEvent.click(screen.getByRole('button', { name:'Create Status Page' })) }
afterEach(cleanup)
test('current success and failure affect their own form', async () => {
 await setup(); mocks.create.mockResolvedValue(undefined); open(); submit(); await act(async () => {}); expect(screen.queryByRole('dialog')).toBeNull(); cleanup();
 await setup(); mocks.create.mockRejectedValue(new Error('current')); open(); submit(); await act(async () => {}); expect(screen.getByRole('button', { name: 'Create Status Page' })).toBeEnabled();
})
test('old success cannot close, reset or unlock a new pending form', async () => {
 const old=deferred(), current=deferred(); await setup(); mocks.create.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise); open(); submit(); fireEvent.click(screen.getByRole('button', { name: 'Cancel' })); open('New draft'); submit(); expect(mocks.create).toHaveBeenCalledTimes(2);
 await act(async () => { old.resolve(); await old.promise }); expect(screen.getByLabelText('Name')).toHaveValue('New draft'); expect(screen.getByRole('button', { name: 'Saving...' })).toBeDisabled();
 await act(async () => { current.resolve(); await current.promise }); expect(screen.queryByRole('dialog')).toBeNull();
})
test('old failure cannot alert in reopened form', async () => {
 const old=deferred(); await setup(); mocks.create.mockReturnValue(old.promise); open(); submit(); fireEvent.click(screen.getByRole('button', { name: 'Cancel' })); open('New draft'); await act(async () => { old.reject(new Error('old')); await old.promise.catch(() => {}) }); expect(screen.getByLabelText('Name')).toHaveValue('New draft'); expect(screen.getByRole('button', { name: 'Create Status Page' })).toBeEnabled();
})
test('old failure cannot unlock a new pending save', async () => {
 const old=deferred(), current=deferred(); await setup(); mocks.create.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise); open(); submit(); fireEvent.click(screen.getByRole('button', { name:'Cancel' })); open('New draft'); submit(); await act(async () => { old.reject(new Error('old')); await old.promise.catch(() => {}) }); expect(screen.getByRole('button', { name:'Saving...' })).toBeDisabled(); current.resolve(); await act(async () => { await current.promise }); expect(screen.queryByRole('dialog')).toBeNull();
})
