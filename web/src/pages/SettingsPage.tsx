import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useNavigate } from 'react-router-dom'
import { AgentsPage } from './AgentsPage'
import { ProvidersPage } from './ProvidersPage'
import { api } from '../lib/api'
import type { SystemSettings, SysInfo, Project } from '../lib/api'
import { timeAgo } from '../lib/utils'
import { getErrorMessage } from '../lib/errors'

function SystemInfoSection() {
  const [info, setInfo] = useState<SysInfo | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api.admin.sysinfo().then(setInfo).catch(() => {}).finally(() => setLoading(false))
  }, [])

  const formatUptime = (s: number) => {
    const h = Math.floor(s / 3600)
    const m = Math.floor((s % 3600) / 60)
    if (h > 0) return `${h}h ${m}m`
    return `${m}m`
  }

  const formatBytes = (b: number) => {
    if (b >= 1024 * 1024) return `${(b / 1024 / 1024).toFixed(1)} MB`
    if (b >= 1024) return `${(b / 1024).toFixed(1)} KB`
    return `${b} B`
  }

  if (loading) return <div className="text-slate-500 text-sm">Loading…</div>
  if (!info) return <div className="text-slate-500 text-sm">Unavailable</div>

  const rows = [
    { label: 'Version', value: info.version },
    { label: 'Uptime', value: formatUptime(info.uptime_seconds) },
    { label: 'Go runtime', value: info.go_version },
    { label: 'Database size', value: formatBytes(info.db_size_bytes) },
    { label: 'Total tasks', value: info.total_tasks.toLocaleString() },
    { label: 'Active tasks', value: info.active_tasks.toString() },
  ]

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold text-white mb-1">System Info</h2>
        <p className="text-slate-400 text-sm">Runtime details for this Phoenix instance.</p>
      </div>
      <div className="bg-slate-900 border border-slate-800 rounded-xl divide-y divide-slate-800">
        {rows.map(({ label, value }) => (
          <div key={label} className="flex items-center justify-between px-4 py-3">
            <span className="text-sm text-slate-400">{label}</span>
            <span className="text-sm text-white font-mono">{value}</span>
          </div>
        ))}
      </div>
      {info.task_counts.length > 0 && (
        <div className="bg-slate-900 border border-slate-800 rounded-xl divide-y divide-slate-800">
          {info.task_counts.map(({ status, count }) => (
            <div key={status} className="flex items-center justify-between px-4 py-2">
              <span className="text-xs text-slate-500 capitalize">{status}</span>
              <span className="text-xs text-slate-300 font-mono">{count.toLocaleString()}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function GlobalGuardrailsSection() {
  const [settings, setSettings] = useState<SystemSettings>({ global_guardrails_enabled: false, global_guardrails: '', core_plugins_enabled: false, community_plugins_enabled: false, obsidian_enabled: false, obsidian_root: '', obsidian_auto_write: false })
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [generating, setGenerating] = useState(false)
  const [saved, setSaved] = useState(false)
  const [genDescription, setGenDescription] = useState('')
  const [showGenInput, setShowGenInput] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    api.admin.getSettings()
      .then(s => { setSettings(s); setLoading(false) })
      .catch(() => setLoading(false))
  }, [])

  const save = async () => {
    setSaving(true)
    setError(null)
    try {
      const updated = await api.admin.saveSettings(settings)
      setSettings(updated)
      setSaved(true)
      setTimeout(() => setSaved(false), 2500)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  const generate = async () => {
    if (!genDescription.trim()) return
    setGenerating(true)
    setError(null)
    try {
      const result = await api.admin.generateGlobalGuardrails(genDescription)
      setSettings(s => ({ ...s, global_guardrails: result.guardrails }))
      setShowGenInput(false)
      setGenDescription('')
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Generation failed')
    } finally {
      setGenerating(false)
    }
  }

  if (loading) return <div className="text-slate-500 text-sm">Loading…</div>

  return (
    <div className="space-y-4">
      {/* Header row with toggle */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-lg font-semibold text-white">Global Guardrails</h2>
          <p className="text-slate-400 text-sm mt-0.5">
            When enabled, these rules are injected into <span className="text-white font-medium">every agent's</span> system
            prompt across all projects — overriding any conflicting per-agent guardrails. Use this to enforce
            platform-wide constraints (e.g. "never modify Jira issues without explicit instruction").
          </p>
        </div>
        {/* Toggle switch */}
        <button
          type="button"
          role="switch"
          aria-checked={settings.global_guardrails_enabled}
          onClick={() => setSettings(s => ({ ...s, global_guardrails_enabled: !s.global_guardrails_enabled }))}
          className={`relative mt-1 inline-flex h-6 w-11 shrink-0 items-center rounded-full border-2 border-transparent transition-colors focus:outline-none focus:ring-2 focus:ring-violet-500 focus:ring-offset-2 focus:ring-offset-slate-900 ${
            settings.global_guardrails_enabled ? 'bg-violet-600' : 'bg-slate-700'
          }`}
        >
          <span
            className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ${
              settings.global_guardrails_enabled ? 'translate-x-5' : 'translate-x-0.5'
            }`}
          />
        </button>
      </div>

      {/* Status badge */}
      {settings.global_guardrails_enabled ? (
        <div className="flex items-center gap-2 text-xs text-amber-300 bg-amber-900/20 border border-amber-700/40 rounded-lg px-3 py-2">
          <span className="text-base">🔒</span>
          <span>Global guardrails are <strong>active</strong>. Every agent run will include these rules.</span>
        </div>
      ) : (
        <div className="flex items-center gap-2 text-xs text-slate-500 bg-slate-800/50 border border-slate-700/40 rounded-lg px-3 py-2">
          <span className="text-base">○</span>
          <span>Global guardrails are <strong>disabled</strong>. Toggle the switch above to activate.</span>
        </div>
      )}

      {/* Guardrails textarea */}
      <div>
        <div className="flex items-center justify-between mb-1.5">
          <label className="text-sm font-medium text-slate-300">Guardrail rules</label>
          <button
            onClick={() => setShowGenInput(g => !g)}
            className="text-xs text-violet-400 hover:text-violet-300 flex items-center gap-1 transition-colors"
          >
            ✦ AI assist
          </button>
        </div>
        <textarea
          rows={8}
          value={settings.global_guardrails}
          onChange={e => setSettings(s => ({ ...s, global_guardrails: e.target.value }))}
          placeholder={`Write your platform-wide guardrails here, e.g.:\n• Never create, update, or delete Jira issues unless the task description explicitly requests it\n• Do not commit code to any repository without human approval\n• Always confirm before making changes to production systems`}
          className="w-full bg-slate-900 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white placeholder-slate-600 focus:outline-none focus:ring-2 focus:ring-violet-500 resize-y font-mono"
        />
        <p className="text-xs text-slate-600 mt-1">
          Plain text or markdown bullets. These will appear verbatim in every agent's system prompt under
          "Platform-Wide Guardrails (mandatory — overrides all other instructions)".
        </p>
      </div>

      {/* AI generation input */}
      {showGenInput && (
        <div className="bg-violet-950/30 border border-violet-800/40 rounded-xl p-4 space-y-3">
          <p className="text-xs text-violet-300 font-medium">Describe what you want to prevent or enforce, and AI will write the rules:</p>
          <textarea
            rows={3}
            value={genDescription}
            onChange={e => setGenDescription(e.target.value)}
            placeholder="e.g. Prevent agents from modifying Jira issues unless explicitly asked, and stop them committing to git repos without approval"
            className="w-full bg-slate-900 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-600 focus:outline-none focus:ring-2 focus:ring-violet-500 resize-y"
          />
          <div className="flex gap-2 justify-end">
            <button
              onClick={() => { setShowGenInput(false); setGenDescription('') }}
              className="px-3 py-1.5 text-xs text-slate-400 hover:text-white transition-colors"
            >
              Cancel
            </button>
            <button
              onClick={generate}
              disabled={generating || !genDescription.trim()}
              className="bg-violet-600 hover:bg-violet-700 disabled:opacity-50 text-white text-xs font-medium px-4 py-1.5 rounded-lg transition-colors flex items-center gap-1.5"
            >
              {generating ? (
                <>
                  <span className="animate-spin inline-block w-3 h-3 border border-white border-t-transparent rounded-full" />
                  Generating…
                </>
              ) : (
                '✦ Generate guardrails'
              )}
            </button>
          </div>
        </div>
      )}

      {/* Error */}
      {error && (
        <div className="text-xs text-red-400 bg-red-900/20 border border-red-700/30 rounded-lg px-3 py-2">
          {error}
        </div>
      )}

      {/* Save button */}
      <div className="flex items-center gap-3">
        <button
          onClick={save}
          disabled={saving}
          className="bg-violet-600 hover:bg-violet-700 disabled:opacity-50 text-white text-sm font-medium px-5 py-2 rounded-lg transition-colors"
        >
          {saving ? 'Saving…' : 'Save guardrails'}
        </button>
        {saved && <span className="text-xs text-green-400">✓ Saved</span>}
      </div>
    </div>
  )
}

function SystemTab() {
  const [downloading, setDownloading] = useState(false)
  const [lastBackup, setLastBackup] = useState<string | null>(null)
  const [restoreFile, setRestoreFile] = useState<File | null>(null)
  const [restoring, setRestoring] = useState(false)
  const [restoreMsg, setRestoreMsg] = useState<{ ok: boolean; text: string } | null>(null)

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

  const doRestore = async () => {
    if (!restoreFile) return
    setRestoring(true)
    setRestoreMsg(null)
    try {
      const form = new FormData()
      form.append('file', restoreFile)
      const res = await fetch('/api/admin/restore', { method: 'POST', body: form })
      const data = await res.json()
      if (!res.ok) {
        setRestoreMsg({ ok: false, text: data.error ?? 'Restore failed' })
      } else {
        setRestoreMsg({ ok: true, text: data.message })
        setRestoreFile(null)
      }
    } catch (error: unknown) {
      setRestoreMsg({ ok: false, text: getErrorMessage(error, 'Restore failed') })
    } finally {
      setRestoring(false)
    }
  }

  return (
    <div className="space-y-10 max-w-xl">
      {/* System Info */}
      <SystemInfoSection />

      <hr className="border-slate-800" />

      {/* Global Guardrails */}
      <GlobalGuardrailsSection />

      <hr className="border-slate-800" />

      {/* Database backup */}
      <div className="space-y-4">
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

        {/* Restore */}
        <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-3">
          <div>
            <div className="text-sm font-medium text-white">Restore from backup</div>
            <div className="text-xs text-slate-500 mt-1">
              Upload a <code className="text-slate-400">.db</code> backup file. The restore is staged
              and applied on the next server restart — no data is lost until you restart.
            </div>
          </div>
          <div className="flex items-center gap-3">
            <label className="flex-1 cursor-pointer">
              <input
                type="file"
                accept=".db"
                className="hidden"
                onChange={e => { setRestoreFile(e.target.files?.[0] ?? null); setRestoreMsg(null) }}
              />
              <div className="border border-dashed border-slate-700 hover:border-slate-500 rounded-lg px-3 py-2 text-sm text-slate-400 hover:text-white transition-colors truncate">
                {restoreFile ? restoreFile.name : 'Choose .db file…'}
              </div>
            </label>
            <button
              onClick={doRestore}
              disabled={!restoreFile || restoring}
              className="bg-amber-600 hover:bg-amber-700 disabled:opacity-50 text-white text-sm font-medium px-4 py-2 rounded-lg transition-colors shrink-0"
            >
              {restoring ? 'Staging…' : '⬆ Restore'}
            </button>
          </div>
          {restoreMsg && (
            <p className={`text-xs ${restoreMsg.ok ? 'text-green-400' : 'text-red-400'}`}>
              {restoreMsg.ok ? '✓ ' : '✗ '}{restoreMsg.text}
            </p>
          )}
        </div>
      </div>
    </div>
  )
}

function ArchivedProjectsTab() {
  const navigate = useNavigate()
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = async () => {
    setLoading(true)
    setError(null)
    try {
      // Fetch both archived projects and monitors together
      const [archivedProjects, archivedMonitors] = await Promise.all([
        api.projects.listArchived('project'),
        api.projects.listArchived('monitor'),
      ])
      setProjects([...archivedProjects, ...archivedMonitors])
    } catch (error: unknown) {
      setError(getErrorMessage(error))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void load()
    }, 0)
    return () => window.clearTimeout(timer)
  }, [])

  const restore = async (id: string, name: string) => {
    if (!confirm(`Restore "${name}" back to active?`)) return
    setBusy(id)
    setError(null)
    try {
      await api.projects.restore(id)
      await load()
    } catch (error: unknown) {
      setError(getErrorMessage(error))
    } finally {
      setBusy(null)
    }
  }

  const remove = async (id: string, name: string) => {
    if (!confirm(`Permanently delete "${name}" and all its tasks? This cannot be undone.`)) return
    setBusy(id)
    setError(null)
    try {
      await api.projects.delete(id)
      await load()
    } catch (error: unknown) {
      setError(getErrorMessage(error))
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="space-y-6 max-w-2xl">
      <div>
        <h2 className="text-lg font-semibold text-white mb-1">Archived Projects &amp; Monitors</h2>
        <p className="text-slate-400 text-sm">
          Archived projects are hidden from the active views but all tasks and history are preserved.
          Restore a project to bring it back, or delete it permanently to remove all data.
        </p>
      </div>

      {error && (
        <div className="text-xs text-red-400 bg-red-900/20 border border-red-700/30 rounded-lg px-3 py-2">
          {error}
        </div>
      )}

      {loading ? (
        <div className="text-slate-500 text-sm">Loading…</div>
      ) : projects.length === 0 ? (
        <div className="bg-slate-900 border border-slate-800 rounded-xl p-8 text-center">
          <div className="text-3xl mb-2">🗂</div>
          <p className="text-slate-400 text-sm">No archived projects.</p>
          <p className="text-slate-600 text-xs mt-1">When you archive a project it will appear here.</p>
        </div>
      ) : (
        <div className="bg-slate-900 border border-slate-800 rounded-xl divide-y divide-slate-800">
          {projects.map(p => (
            <div key={p.id} className="flex items-center gap-4 px-4 py-3">
              {/* Kind icon */}
              <span className="text-slate-600 text-base shrink-0" title={p.kind === 'monitor' ? 'Monitor' : 'Project'}>
                {p.kind === 'monitor' ? '⟳' : '⊞'}
              </span>

              {/* Name + meta */}
              <div className="flex-1 min-w-0">
                <button
                  onClick={() => navigate(p.kind === 'monitor' ? `/monitors/${p.id}` : `/projects/${p.id}`)}
                  className="text-sm font-medium text-slate-300 hover:text-violet-400 transition-colors truncate block text-left"
                >
                  {p.name}
                </button>
                <div className="flex items-center gap-2 mt-0.5">
                  <span className="text-xs text-slate-600">Archived · created {timeAgo(p.created_at)}</span>
                  {p.working_dir && (
                    <span className="text-xs text-slate-700 font-mono truncate" title={p.working_dir}>
                      📁 {p.working_dir}
                    </span>
                  )}
                </div>
              </div>

              {/* Actions */}
              <div className="flex gap-2 shrink-0">
                <button
                  onClick={() => restore(p.id, p.name)}
                  disabled={busy === p.id}
                  className="px-3 py-1.5 text-xs font-medium rounded-lg border border-violet-600/50 text-violet-400 hover:bg-violet-600/20 disabled:opacity-50 transition-colors"
                >
                  {busy === p.id ? '…' : '↩ Restore'}
                </button>
                <button
                  onClick={() => remove(p.id, p.name)}
                  disabled={busy === p.id}
                  className="px-3 py-1.5 text-xs font-medium rounded-lg border border-red-700/50 text-red-400 hover:bg-red-700/20 disabled:opacity-50 transition-colors"
                >
                  {busy === p.id ? '…' : 'Delete'}
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}



const TABS = [
  { id: 'agents', label: 'Agents' },
  { id: 'providers', label: 'Providers' },
  { id: 'system', label: 'System' },
  { id: 'archived', label: 'Archived' },
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
        {tab === 'archived' && <ArchivedProjectsTab />}
      </div>
    </div>
  )
}
