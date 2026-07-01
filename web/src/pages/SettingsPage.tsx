import { useEffect, useState, useRef } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useNavigate } from 'react-router-dom'
import { AgentsPage } from './AgentsPage'
import { ProvidersPage } from './ProvidersPage'
import { api } from '../lib/api'
import type { SystemSettings, SysInfo, Project, ThemeResponse } from '../lib/api'
import { timeAgo } from '../lib/utils'
import { getErrorMessage } from '../lib/errors'
import { THEMES, getTheme, setTheme, injectCommunityThemes } from '../lib/theme'
import type { ThemeEntry, ThemeKind } from '../lib/theme'

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
  const [settings, setSettings] = useState<SystemSettings>({ global_guardrails_enabled: false, global_guardrails: '', core_plugins_enabled: false, community_plugins_enabled: false, obsidian_enabled: false, obsidian_root: '', obsidian_auto_write: false, theme: '', dynamic_orchestration_enabled: false, orchestrator_agent_id: '', max_subtask_depth: 2, max_subtasks_per_level: 5, orchestrator_confidence_threshold: 0.75 })
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

const CONFIRM_WORD = 'RESET'

function DangerZoneSection() {
  const [phase, setPhase] = useState<'idle' | 'confirm' | 'resetting' | 'done'>('idle')
  const [typed, setTyped] = useState('')
  const [error, setError] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  const openConfirm = () => {
    setTyped('')
    setError(null)
    setPhase('confirm')
    setTimeout(() => inputRef.current?.focus(), 50)
  }

  const cancel = () => {
    setPhase('idle')
    setTyped('')
    setError(null)
  }

  const execute = async () => {
    if (typed !== CONFIRM_WORD) return
    setPhase('resetting')
    setError(null)
    try {
      await api.admin.reset()
      setPhase('done')
    } catch (e: unknown) {
      setError(getErrorMessage(e, 'Reset failed'))
      setPhase('confirm')
    }
  }

  if (phase === 'done') {
    return (
      <div className="space-y-4">
        <div>
          <h2 className="text-lg font-semibold text-red-400">Danger Zone</h2>
        </div>
        <div className="bg-emerald-950/30 border border-emerald-700/40 rounded-xl p-5 text-center space-y-2">
          <p className="text-emerald-400 font-medium">Reset complete</p>
          <p className="text-slate-400 text-sm">All configuration has been deleted. Reload the page to start fresh.</p>
          <button
            onClick={() => window.location.reload()}
            className="mt-2 bg-violet-600 hover:bg-violet-700 text-white text-sm font-medium px-5 py-2 rounded-lg transition-colors"
          >
            Reload now
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold text-red-400">Danger Zone</h2>
        <p className="text-slate-400 text-sm mt-0.5">
          Destructive, irreversible actions. There is no undo.
        </p>
      </div>

      <div className="border border-red-900/50 rounded-xl overflow-hidden">
        <div className="bg-red-950/20 px-5 py-4 flex items-start justify-between gap-4">
          <div>
            <p className="text-sm font-semibold text-red-300">Factory reset</p>
            <p className="text-xs text-slate-400 mt-1 leading-relaxed">
              Permanently deletes <span className="text-white font-medium">all providers, agents, projects, tasks, teams, memos, and plugins</span>.
              System settings are also cleared. Your user account is preserved.
              This cannot be undone — take a database backup first if you want to be able to recover.
            </p>
          </div>
          <button
            onClick={openConfirm}
            disabled={phase === 'resetting'}
            className="shrink-0 bg-red-700 hover:bg-red-600 disabled:opacity-50 text-white text-sm font-medium px-4 py-2 rounded-lg transition-colors"
          >
            Reset…
          </button>
        </div>

        {phase === 'confirm' && (
          <div className="bg-slate-950 border-t border-red-900/40 px-5 py-4 space-y-3">
            <div className="flex items-start gap-2 text-xs text-amber-300 bg-amber-900/20 border border-amber-700/40 rounded-lg px-3 py-2.5">
              <span className="text-base leading-none mt-0.5">⚠</span>
              <span>
                <strong>This will permanently erase all data.</strong> There is no backup, no undo, and no recovery path.
                Make sure you have downloaded a database backup before continuing.
              </span>
            </div>
            <div>
              <label className="text-xs text-slate-400 block mb-1.5">
                Type <span className="font-mono font-bold text-red-300">{CONFIRM_WORD}</span> to confirm:
              </label>
              <input
                ref={inputRef}
                type="text"
                value={typed}
                onChange={e => setTyped(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter' && typed === CONFIRM_WORD) execute() }}
                placeholder={CONFIRM_WORD}
                className="w-full bg-slate-900 border border-red-800/60 rounded-lg px-3 py-2 text-sm text-white font-mono placeholder-slate-700 focus:outline-none focus:ring-2 focus:ring-red-600"
                spellCheck={false}
                autoComplete="off"
              />
            </div>
            {error && (
              <p className="text-xs text-red-400">{error}</p>
            )}
            <div className="flex gap-2 justify-end">
              <button
                onClick={cancel}
                className="px-4 py-2 text-sm text-slate-400 hover:text-white transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={execute}
                disabled={typed !== CONFIRM_WORD}
                className="bg-red-700 hover:bg-red-600 disabled:opacity-40 disabled:cursor-not-allowed text-white text-sm font-semibold px-5 py-2 rounded-lg transition-colors"
              >
                Erase everything
              </button>
            </div>
          </div>
        )}

        {phase === 'resetting' && (
          <div className="bg-slate-950 border-t border-red-900/40 px-5 py-4 flex items-center gap-3">
            <span className="animate-spin inline-block w-4 h-4 border-2 border-red-400 border-t-transparent rounded-full" />
            <span className="text-sm text-slate-400">Erasing all data…</span>
          </div>
        )}
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

      <hr className="border-slate-800" />

      {/* Danger zone */}
      <DangerZoneSection />
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



function ThemeCard({ theme, current, onApply }: { theme: ThemeEntry; current: string; onApply: (id: string) => void }) {
  const active = theme.id === current
  return (
    <button
      onClick={() => onApply(theme.id)}
      className={`relative flex flex-col gap-2 p-3 rounded-xl border text-left transition-all ${
        active
          ? 'border-violet-500 bg-violet-600/10 ring-1 ring-violet-500/40'
          : 'border-slate-800 hover:border-slate-600 hover:bg-slate-800/50'
      }`}
    >
      {active && (
        <span className="absolute top-2 right-2 text-violet-400 text-xs font-semibold">✓</span>
      )}
      <span className="flex gap-1">
        {theme.preview.map((c, i) => (
          <span key={i} className="w-5 h-5 rounded-md border border-white/10 flex-shrink-0"
            style={{ backgroundColor: c }} />
        ))}
      </span>
      <span>
        <span className="block text-sm font-medium text-slate-200 leading-none mb-0.5">
          {theme.label}
          {theme.isCustom && (
            <span className="ml-1.5 text-[9px] font-semibold px-1 py-px rounded bg-violet-600/20 text-violet-400 align-middle">CUSTOM</span>
          )}
        </span>
        <span className="block text-xs text-slate-500">{theme.description}</span>
      </span>
    </button>
  )
}

function AppearanceTab() {
  const [current, setCurrent] = useState<string>(getTheme)
  const [allThemes, setAllThemes] = useState<ThemeEntry[]>([...THEMES])
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    api.themes.list().then((community: ThemeResponse[]) => {
      if (community.length === 0) return
      const toInject = community
        .filter(t => t.vars && Object.keys(t.vars).length > 0)
        .map(t => ({ id: t.id, vars: t.vars! }))
      injectCommunityThemes(toInject)
      const customEntries: ThemeEntry[] = community.map(t => ({
        id: t.id,
        kind: (t.kind || 'dark') as ThemeKind,
        label: t.label,
        description: 'Custom theme',
        preview: t.preview || [],
        isCustom: true,
        vars: t.vars,
      }))
      setAllThemes([...THEMES, ...customEntries])
    }).catch(() => {})
  }, [])

  const apply = async (id: string) => {
    setTheme(id)
    setCurrent(id)
    setSaving(true)
    try {
      const s = await api.admin.getSettings()
      await api.admin.saveSettings({ ...s, theme: id })
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    } catch { /* non-critical */ } finally {
      setSaving(false)
    }
  }

  const darkThemes = allThemes.filter(t => t.kind === 'dark' && !t.isCustom)
  const lightThemes = allThemes.filter(t => t.kind === 'light' && !t.isCustom)
  const customThemes = allThemes.filter(t => t.isCustom)

  return (
    <div className="space-y-8">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold text-white">Appearance</h2>
          <p className="text-slate-400 text-sm mt-0.5">Choose a colour theme for the Phoenix UI. Your selection is saved to this browser and synced to the server.</p>
        </div>
        {saving && <span className="text-xs text-slate-500">Saving…</span>}
        {saved && !saving && <span className="text-xs text-violet-400">Saved</span>}
      </div>

      <div className="space-y-4">
        <h3 className="text-sm font-semibold text-slate-400 uppercase tracking-wider">Dark</h3>
        <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
          {darkThemes.map(t => <ThemeCard key={t.id} theme={t} current={current} onApply={apply} />)}
        </div>
      </div>

      <div className="space-y-4">
        <h3 className="text-sm font-semibold text-slate-400 uppercase tracking-wider">Light</h3>
        <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
          {lightThemes.map(t => <ThemeCard key={t.id} theme={t} current={current} onApply={apply} />)}
        </div>
      </div>

      {customThemes.length > 0 && (
        <div className="space-y-4">
          <h3 className="text-sm font-semibold text-slate-400 uppercase tracking-wider">Custom</h3>
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
            {customThemes.map(t => <ThemeCard key={t.id} theme={t} current={current} onApply={apply} />)}
          </div>
        </div>
      )}
    </div>
  )
}

function TaskTemplatesTab() {
  const [templates, setTemplates] = useState<import('../lib/api').TaskTemplate[]>([])
  const [loading, setLoading] = useState(true)

  const load = () => {
    api.taskTemplates.list().then(setTemplates).catch(() => {}).finally(() => setLoading(false))
  }
  useEffect(() => { load() }, [])

  const remove = async (id: string) => {
    if (!confirm('Delete this template?')) return
    await api.taskTemplates.delete(id).catch(() => {})
    load()
  }

  if (loading) return <div className="text-slate-500 text-sm">Loading…</div>

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold text-white">Task Templates</h2>
        <p className="text-slate-400 text-sm mt-1">
          Reusable prompt scaffolds. Create them from the task compose form using "Save as template".
          Supports <code className="bg-slate-800 px-1 rounded text-xs text-slate-300">{'{{date}}'}</code> and{' '}
          <code className="bg-slate-800 px-1 rounded text-xs text-slate-300">{'{{project_name}}'}</code> variables.
        </p>
      </div>
      {templates.length === 0 ? (
        <div className="text-slate-500 text-sm py-8 text-center border border-dashed border-slate-700 rounded-lg">
          No templates yet. Open any project, compose a task, and click "Save as template".
        </div>
      ) : (
        <div className="space-y-2">
          {templates.map(t => (
            <div key={t.id} className="flex items-start justify-between gap-4 bg-slate-900 border border-slate-800 rounded-lg px-4 py-3">
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-medium text-white text-sm">{t.name}</span>
                  <span className="text-xs text-slate-500">{t.project_id ? 'project' : 'global'}</span>
                </div>
                <p className="text-xs text-slate-400 mt-0.5 truncate">{t.title}</p>
                {t.description && <p className="text-xs text-slate-600 mt-0.5 truncate">{t.description}</p>}
              </div>
              <button
                onClick={() => remove(t.id)}
                className="text-xs text-red-400 hover:text-red-300 flex-shrink-0 mt-0.5"
              >
                Delete
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function ToggleSwitch({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full border-2 border-transparent transition-colors focus:outline-none focus:ring-2 focus:ring-violet-500 focus:ring-offset-2 focus:ring-offset-slate-900 ${
        checked ? 'bg-violet-600' : 'bg-slate-700'
      }`}
    >
      <span className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ${
        checked ? 'translate-x-5' : 'translate-x-0.5'
      }`} />
    </button>
  )
}

function OrchestrationTab() {
  const [settings, setSettings] = useState<import('../lib/api').SystemSettings | null>(null)
  const [agents, setAgents] = useState<import('../lib/api').Agent[]>([])
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    Promise.all([api.admin.getSettings(), api.agents.list()])
      .then(([s, a]) => { setSettings(s); setAgents(a) })
      .catch(() => {})
  }, [])

  const save = async () => {
    if (!settings) return
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

  if (!settings) return <div className="text-slate-500 text-sm">Loading…</div>

  return (
    <div className="space-y-8">
      <div>
        <h2 className="text-lg font-semibold text-white mb-1">Dynamic Orchestration</h2>
        <p className="text-slate-400 text-sm">
          When enabled, tasks submitted to projects with no assigned agents are routed to a global orchestrator agent.
          The orchestrator analyses the task, selects the best model, and optionally decomposes it into subtasks.
        </p>
      </div>

      {/* Master switch */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-sm font-medium text-slate-200">Enable Dynamic Orchestration</p>
          <p className="text-xs text-slate-500 mt-0.5">
            Routes unassigned-project tasks to the orchestrator agent instead of failing.
          </p>
        </div>
        <ToggleSwitch
          checked={settings.dynamic_orchestration_enabled ?? false}
          onChange={v => setSettings(s => s ? { ...s, dynamic_orchestration_enabled: v } : s)}
        />
      </div>

      {settings.dynamic_orchestration_enabled && (
        <div className="space-y-6 border-l-2 border-violet-800/40 pl-5">

          {/* Orchestrator agent picker */}
          <div>
            <label className="text-sm font-medium text-slate-200 block mb-1.5">Orchestrator Agent</label>
            <p className="text-xs text-slate-500 mb-2">
              The agent used to analyse and route tasks. Should be powered by a planning-capable model.
            </p>
            <select
              value={settings.orchestrator_agent_id ?? ''}
              onChange={e => setSettings(s => s ? { ...s, orchestrator_agent_id: e.target.value } : s)}
              className="w-full bg-slate-900 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-violet-500"
            >
              <option value="">— No orchestrator selected —</option>
              {agents.map(a => (
                <option key={a.id} value={a.id}>
                  {a.name}{a.is_orchestrator ? ' ★' : ''}
                </option>
              ))}
            </select>
          </div>

          {/* Max subtask depth */}
          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="text-sm font-medium text-slate-200">Max Subtask Depth</label>
              <span className="text-sm text-violet-300 font-mono">{settings.max_subtask_depth ?? 2}</span>
            </div>
            <p className="text-xs text-slate-500 mb-2">Maximum recursive decomposition depth (1 = no recursion).</p>
            <input
              type="range"
              min={1}
              max={5}
              step={1}
              value={settings.max_subtask_depth ?? 2}
              onChange={e => setSettings(s => s ? { ...s, max_subtask_depth: parseInt(e.target.value) } : s)}
              className="w-full accent-violet-500"
            />
            <div className="flex justify-between text-xs text-slate-600 mt-0.5">
              <span>1</span><span>2</span><span>3</span><span>4</span><span>5</span>
            </div>
          </div>

          {/* Max subtasks per level */}
          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="text-sm font-medium text-slate-200">Max Subtasks per Level</label>
              <span className="text-sm text-violet-300 font-mono">{settings.max_subtasks_per_level ?? 5}</span>
            </div>
            <p className="text-xs text-slate-500 mb-2">Maximum number of parallel subtasks spawned at each level.</p>
            <input
              type="range"
              min={1}
              max={10}
              step={1}
              value={settings.max_subtasks_per_level ?? 5}
              onChange={e => setSettings(s => s ? { ...s, max_subtasks_per_level: parseInt(e.target.value) } : s)}
              className="w-full accent-violet-500"
            />
            <div className="flex justify-between text-xs text-slate-600 mt-0.5">
              <span>1</span><span>3</span><span>5</span><span>7</span><span>10</span>
            </div>
          </div>

          {/* Confidence threshold */}
          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="text-sm font-medium text-slate-200">Auto-run Confidence Threshold</label>
              <span className="text-sm text-violet-300 font-mono">{((settings.orchestrator_confidence_threshold ?? 0.75) * 100).toFixed(0)}%</span>
            </div>
            <p className="text-xs text-slate-500 mb-2">
              Plans with confidence ≥ this threshold run automatically.
              Below it, tasks go to <span className="text-amber-400">awaiting approval</span> for human review.
            </p>
            <input
              type="range"
              min={50}
              max={100}
              step={5}
              value={Math.round((settings.orchestrator_confidence_threshold ?? 0.75) * 100)}
              onChange={e => setSettings(s => s ? { ...s, orchestrator_confidence_threshold: parseInt(e.target.value) / 100 } : s)}
              className="w-full accent-violet-500"
            />
            <div className="flex justify-between text-xs text-slate-600 mt-0.5">
              <span>50%</span><span>60%</span><span>70%</span><span>80%</span><span>90%</span><span>100%</span>
            </div>
          </div>
        </div>
      )}

      {error && (
        <div className="text-xs text-red-400 bg-red-900/20 border border-red-700/30 rounded-lg px-3 py-2">{error}</div>
      )}

      <div className="flex items-center gap-3">
        <button
          onClick={save}
          disabled={saving}
          className="bg-violet-600 hover:bg-violet-700 disabled:opacity-50 text-white text-sm font-medium px-5 py-2 rounded-lg transition-colors"
        >
          {saving ? 'Saving…' : 'Save settings'}
        </button>
        {saved && <span className="text-xs text-green-400">✓ Saved</span>}
      </div>
    </div>
  )
}

const TABS = [
  { id: 'agents', label: 'Agents' },
  { id: 'providers', label: 'Providers' },
  { id: 'orchestration', label: 'Orchestration' },
  { id: 'system', label: 'System' },
  { id: 'appearance', label: 'Appearance' },
  { id: 'archived', label: 'Archived' },
  { id: 'templates', label: 'Templates' },
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
        {tab === 'orchestration' && <OrchestrationTab />}
        {tab === 'system' && <SystemTab />}
        {tab === 'appearance' && <AppearanceTab />}
        {tab === 'archived' && <ArchivedProjectsTab />}
        {tab === 'templates' && <TaskTemplatesTab />}
      </div>
    </div>
  )
}
