import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { AgentsPage } from './AgentsPage'
import { ProvidersPage } from './ProvidersPage'

const TABS = [
  { id: 'agents', label: 'Agents' },
  { id: 'providers', label: 'Providers' },
]

export function SettingsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [tab, setTab] = useState(searchParams.get('tab') ?? 'agents')

  useEffect(() => {
    setSearchParams({ tab }, { replace: true })
  }, [tab, setSearchParams])

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-white">Settings</h1>
        <p className="text-slate-400 text-sm mt-1">Configure agents, providers, and system options.</p>
      </div>

      {/* Tab bar */}
      <div className="flex gap-1 border-b border-slate-800 pb-0">
        {TABS.map(t => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={`px-4 py-2 text-sm font-medium rounded-t-lg transition-colors -mb-px border-b-2 ${
              tab === t.id
                ? 'text-violet-300 border-violet-500 bg-violet-900/10'
                : 'text-slate-400 border-transparent hover:text-white hover:border-slate-600'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* Tab content */}
      <div>
        {tab === 'agents' && <AgentsPage />}
        {tab === 'providers' && <ProvidersPage />}
      </div>
    </div>
  )
}
