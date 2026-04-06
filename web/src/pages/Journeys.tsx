import { useState } from 'react'
import { Route, Plus, Play, Pause, Edit, Trash2, CheckCircle, XCircle, Clock } from 'lucide-react'

export function Journeys() {
  const [, setShowCreateModal] = useState(false)

  // Mock journeys data
  const journeys = [
    {
      id: '1',
      name: 'User Login Flow',
      enabled: true,
      weight: 300,
      timeout: 60,
      step_count: 4,
      last_run: new Date(Date.now() - 300000).toISOString(),
      last_status: 'passed',
      avg_duration: 2340,
    },
    {
      id: '2',
      name: 'Checkout Process',
      enabled: true,
      weight: 600,
      timeout: 120,
      step_count: 6,
      last_run: new Date(Date.now() - 600000).toISOString(),
      last_status: 'passed',
      avg_duration: 4560,
    },
    {
      id: '3',
      name: 'API Key Rotation',
      enabled: false,
      weight: 3600,
      timeout: 300,
      step_count: 3,
      last_run: new Date(Date.now() - 86400000).toISOString(),
      last_status: 'failed',
      avg_duration: 0,
    },
  ]

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-text-primary">Journeys</h1>
          <p className="text-text-muted mt-1">Multi-step synthetic monitoring</p>
        </div>
        <button
          onClick={() => setShowCreateModal(true)}
          className="flex items-center gap-2 px-4 py-2 bg-primary text-white rounded-lg hover:bg-primary-dark transition-colors"
        >
          <Plus className="w-4 h-4" />
          Create Journey
        </button>
      </div>

      {/* Journeys Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-3 gap-4">
        {journeys.map((journey) => (
          <div key={journey.id} className="bg-bg-card rounded-lg border border-bg-hover overflow-hidden">
            <div className="p-6">
              <div className="flex items-start justify-between">
                <div className="flex items-center gap-3">
                  <div className={`w-10 h-10 rounded-lg flex items-center justify-center ${
                    journey.enabled ? 'bg-primary/10' : 'bg-text-muted/10'
                  }`}>
                    <Route className={`w-5 h-5 ${journey.enabled ? 'text-primary' : 'text-text-muted'}`} />
                  </div>
                  <div>
                    <h3 className="font-semibold text-text-primary">{journey.name}</h3>
                    <p className="text-sm text-text-muted">{journey.step_count} steps</p>
                  </div>
                </div>
                <span className={`px-2 py-1 rounded text-xs ${
                  journey.enabled ? 'bg-success/10 text-success' : 'bg-text-muted/10 text-text-muted'
                }`}>
                  {journey.enabled ? 'Active' : 'Disabled'}
                </span>
              </div>

              <div className="mt-4 grid grid-cols-2 gap-4">
                <div>
                  <p className="text-sm text-text-muted">Interval</p>
                  <p className="font-medium text-text-primary">{journey.weight}s</p>
                </div>
                <div>
                  <p className="text-sm text-text-muted">Timeout</p>
                  <p className="font-medium text-text-primary">{journey.timeout}s</p>
                </div>
              </div>

              <div className="mt-4 pt-4 border-t border-bg-hover">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    {journey.last_status === 'passed' ? (
                      <CheckCircle className="w-4 h-4 text-success" />
                    ) : (
                      <XCircle className="w-4 h-4 text-error" />
                    )}
                    <span className={`text-sm ${
                      journey.last_status === 'passed' ? 'text-success' : 'text-error'
                    }`}>
                      Last run {journey.last_status}
                    </span>
                  </div>
                  <span className="text-sm text-text-muted">
                    {new Date(journey.last_run).toLocaleTimeString()}
                  </span>
                </div>
                {journey.avg_duration > 0 && (
                  <p className="mt-2 text-sm text-text-muted flex items-center gap-1">
                    <Clock className="w-3 h-3" />
                    Avg: {(journey.avg_duration / 1000).toFixed(1)}s
                  </p>
                )}
              </div>
            </div>

            <div className="px-6 py-4 bg-bg-hover/30 flex justify-end gap-2">
              <button className="p-2 text-text-secondary hover:text-primary hover:bg-primary/10 rounded-lg transition-colors">
                {journey.enabled ? <Pause className="w-4 h-4" /> : <Play className="w-4 h-4" />}
              </button>
              <button className="p-2 text-text-secondary hover:text-primary hover:bg-primary/10 rounded-lg transition-colors">
                <Edit className="w-4 h-4" />
              </button>
              <button className="p-2 text-text-secondary hover:text-error hover:bg-error/10 rounded-lg transition-colors">
                <Trash2 className="w-4 h-4" />
              </button>
            </div>
          </div>
        ))}

        {/* Create New Card */}
        <button
          onClick={() => setShowCreateModal(true)}
          className="bg-bg-card rounded-lg border border-dashed border-bg-hover p-6 flex flex-col items-center justify-center gap-4 hover:border-primary hover:text-primary transition-colors text-text-muted min-h-[280px]"
        >
          <div className="w-16 h-16 rounded-full bg-bg-hover flex items-center justify-center">
            <Plus className="w-8 h-8" />
          </div>
          <div className="text-center">
            <p className="font-medium text-text-primary">Create New Journey</p>
            <p className="text-sm text-text-muted mt-1">Set up multi-step monitoring</p>
          </div>
        </button>
      </div>
    </div>
  )
}
