import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { AgentsPage } from './AgentsPage'
import { ProvidersPage } from './ProvidersPage'

function SystemTab() {
  const [downloading, setDownloading] = useState(false)
  const [lastBackup, setLastBackup] = useState<string | null>(null)

  const downloadBackup = async () => {
    setDownloading(true)
    try {
      const res = await fetch('/api/admin/backup')
      if (!res.ok) throw new Error(await res.text())
      const blob = await res.blob()
      const disposition = res.headers.get('Content-Disposition') ?? ''
      const match = disposition.match(/filename="([^"]+)"/)
      const filename = match?.[1] ?? 'phoenix-backup.db'
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = filename
      a.click()
      URL.revokeObjectURL(url)
      setLastBackup(new Date().toLocaleTimeString())
    } catch (e) {
      console.error('Backup failed:', e)
    } finally {
      setDownloading(false)
    }
  }

  return (
    <div className="space-y-6 max-w-xl">
      <div>
        <h2 className="text-lg font-semibold text-white mb-1">Database</h2>
        <p className="text-slate-400 text-sm">Download a consistent snapshot of the Phoenix database. Safe to run while the server is active — the backup is taken without interrupting ongoing tasks.</p>
      </div>
      <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 flex items-center justify-between">
        <div>
          <div className="text-sm font-medium text-white">Download backup</div>
          <div className="text-xs text-slate-500 mt-1">
            Saves a <code className="text-slate-400">phoenix-backup-YYYYMMDD-HHMMSS.db</code> file
          </div>
          {lastBackup && (
            <div className="text-xs text-green-400 mt-1">✓ Last downloaded at {lastBackup}</div>
          )}
        </div>
        <button
          onClick={downloadBackup}
          disabled={downloading}
          className="bg-violet-600 hover:bg-violet-700 disabled:opacity-50 text-white text-sm font-medium px-4 py-2 rounded-lg transition-colors shrink-0"
        >
          {downloading ? 'Preparing…' : '⬇ Download .db'}
        </button>
      </div>
      <div className="text-xs text-slate-600">
        The downloaded file is a standard SQLite database. You can open it with any SQLite tool
        (e.g. <code>sqlite3</code>, DB Browser for SQLite) to inspect or restore data.
      </div>
    </div>
  )
}

const TABS = [
  { id: 'agents', label: 'Agents' },
  { id: 'providers', label: 'Providers' },
  { id: 'system', label: 'System' },
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
        {tab === 'system' && <SystemTab />}
      </div>
    </div>
  )
}
