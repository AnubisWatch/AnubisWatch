import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { AlertCircle, Building2, Check, ChevronsUpDown, Loader2 } from 'lucide-react'
import { api, User, Workspace } from '../api/client'

interface WorkspaceSwitcherProps {
  user: User | null
  onWorkspaceSwitched?: () => void
}

export function WorkspaceSwitcher({ user, onWorkspaceSwitched }: WorkspaceSwitcherProps) {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [loading, setLoading] = useState(false)
  const [switchingTo, setSwitchingTo] = useState<string | null>(null)
  const [open, setOpen] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const containerRef = useRef<HTMLDivElement | null>(null)

  const currentWorkspace = useMemo(
    () => workspaces.find((workspace) => workspace.id === user?.workspace),
    [user?.workspace, workspaces]
  )
  const currentLabel = currentWorkspace?.name || user?.workspace || 'Default'
  const canSwitch = workspaces.length > 1

  const loadWorkspaces = useCallback(async () => {
    if (!user) {
      setWorkspaces([])
      return
    }

    setLoading(true)
    try {
      const result = await api.get<Workspace[]>('/workspaces')
      setWorkspaces(Array.isArray(result) ? result : [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load workspaces')
    } finally {
      setLoading(false)
    }
  }, [user])

  useEffect(() => {
    loadWorkspaces()
  }, [loadWorkspaces])

  useEffect(() => {
    const handlePointerDown = (event: PointerEvent) => {
      if (!containerRef.current?.contains(event.target as Node)) {
        setOpen(false)
      }
    }

    document.addEventListener('pointerdown', handlePointerDown)
    return () => document.removeEventListener('pointerdown', handlePointerDown)
  }, [])

  const handleSwitch = async (workspaceID: string) => {
    if (!user || workspaceID === user.workspace || switchingTo) {
      setOpen(false)
      return
    }

    setSwitchingTo(workspaceID)
    setError(null)
    try {
      await api.post<User>('/auth/workspace', { workspace: workspaceID })
      setOpen(false)
      if (onWorkspaceSwitched) {
        onWorkspaceSwitched()
      } else {
        window.location.reload()
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to switch workspace')
    } finally {
      setSwitchingTo(null)
    }
  }

  if (!user) {
    return null
  }

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={() => canSwitch && setOpen((value) => !value)}
        disabled={!canSwitch || loading}
        className="flex min-w-0 max-w-48 items-center gap-2 rounded-xl border border-[#D4AF37]/20 bg-[#D4AF37]/5 px-3 py-2 text-left text-[#D4AF37] transition-all hover:border-[#D4AF37]/40 hover:bg-[#D4AF37]/10 disabled:cursor-default disabled:opacity-80"
        aria-label="Switch workspace"
        aria-haspopup="menu"
        aria-expanded={open}
      >
        {loading ? <Loader2 className="h-4 w-4 shrink-0 animate-spin" /> : <Building2 className="h-4 w-4 shrink-0" />}
        <span className="min-w-0 truncate text-xs font-cinzel font-semibold tracking-wide">{currentLabel}</span>
        {canSwitch && <ChevronsUpDown className="h-3.5 w-3.5 shrink-0 text-[#D4AF37]/70" />}
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 mt-2 w-72 overflow-hidden rounded-xl border border-gray-700/70 bg-gray-950 shadow-2xl shadow-black/40"
        >
          <div className="max-h-80 overflow-y-auto py-1">
            {workspaces.map((workspace) => {
              const active = workspace.id === user.workspace
              const switching = switchingTo === workspace.id

              return (
                <button
                  key={workspace.id}
                  type="button"
                  role="menuitem"
                  onClick={() => handleSwitch(workspace.id)}
                  disabled={Boolean(switchingTo)}
                  className={`flex w-full items-center gap-3 px-3 py-2.5 text-left transition-colors ${
                    active ? 'bg-[#D4AF37]/10 text-[#D4AF37]' : 'text-gray-300 hover:bg-gray-900 hover:text-white'
                  } disabled:cursor-wait disabled:opacity-70`}
                  aria-label={`Switch to ${workspace.name || workspace.id}`}
                >
                  <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-gray-900 text-[#D4AF37]">
                    {switching ? <Loader2 className="h-4 w-4 animate-spin" /> : <Building2 className="h-4 w-4" />}
                  </div>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium">{workspace.name || workspace.id}</p>
                    <p className="truncate text-xs text-gray-500">{workspace.id}</p>
                  </div>
                  {active && <Check className="h-4 w-4 shrink-0" />}
                </button>
              )
            })}
          </div>

          {error && (
            <div className="flex items-start gap-2 border-t border-gray-800 px-3 py-2 text-xs text-rose-300">
              <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <span>{error}</span>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
