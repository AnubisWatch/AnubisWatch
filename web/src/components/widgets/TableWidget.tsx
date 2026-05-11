import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import type { WidgetConfig } from '../../api/client'

interface TableWidgetProps {
  widget: WidgetConfig
  dashboardId: string
}

export function TableWidget({ widget, dashboardId }: TableWidgetProps) {
  const [data, setData] = useState<Array<Record<string, unknown>>>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    const fetch = async () => {
      setLoading(true)
      setError(null)
      try {
        const result = await api.post<Array<Record<string, unknown>>>(
          `/dashboards/${dashboardId}/query`,
          widget.query
        )
        if (!cancelled) setData(result || [])
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    fetch()
    return () => { cancelled = true }
  }, [dashboardId, widget.query])

  if (loading) {
    return <div className="flex items-center justify-center h-full"><div className="w-6 h-6 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" /></div>
  }

  if (error) {
    return <div className="flex flex-col items-center justify-center h-full text-center px-4">
      <p className="text-rose-400 text-sm mb-1">Error</p>
      <p className="text-gray-400 text-xs">{error}</p>
    </div>
  }

  if (data.length === 0) {
    return <div className="flex items-center justify-center h-full text-gray-500 text-sm">No data</div>
  }

  const columns = Object.keys(data[0] || {}).slice(0, 5)

  return (
    <div className="overflow-auto h-full">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-gray-700">
            {columns.map(col => (
              <th key={col} className="text-left py-2 px-3 text-gray-400 font-medium capitalize">
                {col.replace(/_/g, ' ')}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.slice(0, 20).map((row, i) => (
            <tr key={i} className="border-b border-gray-800/50 hover:bg-gray-800/30 transition-colors">
              {columns.map(col => (
                <td key={col} className="py-2 px-3 text-gray-300">
                  {typeof row[col] === 'boolean' ? (
                    <span className={`px-1.5 py-0.5 rounded text-xs ${row[col] ? 'bg-emerald-500/10 text-emerald-400' : 'bg-red-500/10 text-red-400'}`}>
                      {row[col] ? 'Yes' : 'No'}
                    </span>
                  ) : String(row[col] ?? '—')}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
