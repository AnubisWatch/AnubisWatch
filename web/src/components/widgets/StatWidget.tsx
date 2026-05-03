import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import type { WidgetConfig } from '../../api/client'

interface StatWidgetProps {
  widget: WidgetConfig
  dashboardId: string
}

export function StatWidget({ widget, dashboardId }: StatWidgetProps) {
  const [data, setData] = useState<Record<string, number> | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    const fetch = async () => {
      setLoading(true)
      try {
        const result = await api.post<Record<string, number>>(
          `/dashboards/${dashboardId}/query`,
          widget.query
        )
        if (!cancelled) setData(result)
      } catch {
        // silently handle
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    fetch()
    return () => { cancelled = true }
  }, [dashboardId, widget.query])

  if (loading) return <div className="flex items-center justify-center h-full"><div className="w-6 h-6 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" /></div>

  const value = data ? data[widget.query.metric] ?? Object.values(data)[0] ?? '—' : '—'
  const label = widget.query.metric

  return (
    <div className="flex flex-col items-center justify-center h-full">
      <p className="text-gray-400 text-sm mb-1">{label}</p>
      <p className="text-4xl font-bold text-white">{typeof value === 'number' ? value.toLocaleString() : value}</p>
    </div>
  )
}
