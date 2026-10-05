import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route, Link } from 'react-router-dom'
import { afterEach, expect, test, vi } from 'vitest'
import { DashboardDetail } from './DashboardDetail'
const mocks=vi.hoisted(() => ({ post: vi.fn(), get: vi.fn(), put: vi.fn() }))
vi.mock('../api/client', () => ({ api: mocks }))
function deferred() { let resolve!: (v: { id: string }) => void; const promise=new Promise<{ id: string }>(r => { resolve=r }); return { promise, resolve } }
async function setup() { mocks.post.mockReset(); mocks.get.mockReset(); mocks.get.mockResolvedValue({ id:'existing',name:'Existing dashboard',widgets:[],refresh_sec:0 }); const ui=render(<MemoryRouter initialEntries={['/dashboards/new']}><Link to="/dashboards/existing">Other dashboard</Link><Routes><Route path="/dashboards" element={<div>Dashboard list</div>} /><Route path="/dashboards/:id" element={<DashboardDetail />} /></Routes></MemoryRouter>); await act(async () => {}); return ui }
function submit() { fireEvent.click(screen.getByRole('button', { name:'Create Dashboard' })) }
afterEach(cleanup)
test('current create navigates with the returned ID', async () => { await setup(); mocks.post.mockResolvedValue({ id:'created' }); submit(); await act(async () => {}); expect(mocks.get).toHaveBeenCalledWith('/dashboards/created'); expect(screen.getByText('Existing dashboard')).toBeInTheDocument() })
test('cancelled create leaves the list open', async () => { const old=deferred(); await setup(); mocks.post.mockReturnValue(old.promise); submit(); fireEvent.click(screen.getByRole('button', { name:'Cancel' })); await act(async () => { old.resolve({ id:'created' }); await old.promise }); expect(screen.getByText('Dashboard list')).toBeInTheDocument() })
test('creation cannot redirect a reused component viewing another ID', async () => { const old=deferred(); await setup(); mocks.post.mockReturnValue(old.promise); submit(); fireEvent.click(screen.getByRole('link', { name:'Other dashboard' })); await act(async () => {}); await act(async () => { old.resolve({ id:'created' }); await old.promise }); expect(mocks.get).toHaveBeenCalledWith('/dashboards/existing'); expect(mocks.get).not.toHaveBeenCalledWith('/dashboards/created') })
test('unmounted creation finishes without updating or navigating', async () => { const old=deferred(); const ui=await setup(); mocks.post.mockReturnValue(old.promise); submit(); ui.unmount(); await act(async () => { old.resolve({ id:'created' }); await old.promise }); expect(document.body).not.toHaveTextContent('Existing dashboard') })
