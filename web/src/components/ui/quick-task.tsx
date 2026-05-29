import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '@/lib/api'
import type { Agent } from '@/lib/api'

/**
 * Floating "+" button always visible in the bottom-right corner.
 * Opens a compact modal to fire a one-off task without needing a project.
 * Tasks land in the "Quick Tasks" sandbox project (auto-created on first use).
 */
export function QuickTaskButton() {
  const [open, setOpen] = useState(false)

  // Keyboard shortcut: Cmd+K / Ctrl+K
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault()
        setOpen(o => !o)
      }
      if (e.key === 'Escape') setOpen(false)
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  return (
    <>
      {/* Floating action button */}
      <button
        onClick={() => setOpen(true)}
        title="Quick Task (⌘K)"
        className="fixed bottom-6 right-6 z-40 w-12 h-12 rounded-full bg-violet-600 hover:bg-violet-500 text-white shadow-lg shadow-violet-900/40 flex items-center justify-center text-xl transition-all hover:scale-105 active:scale-95"
      >
        ✦
      </button>

      {open && <QuickTaskModal onClose={() => setOpen(false)} />}
    </>
  )
}

function QuickTaskModal({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate()
  const [agents, setAgents] = useState<Agent[]>([])
  const [agentId, setAgentId] = useState('')
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [running, setRunning] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api.agents.list().then(list => {
      const active = list.filter(a => a.status === 'active')
      setAgents(active.length ? active : list)
      setAgentId((active.length ? active : list)[0]?.id ?? '')
    })
  }, [])

  const submit = async () => {
    if (!title.trim()) { setError('Title is required'); return }
    if (!agentId) { setError('Select an agent'); return }
    setRunning(true)
    setError('')
    try {
      const task = await api.tasks.quick(agentId, title.trim(), description.trim())
      onClose()
      navigate('/tasks')
    } catch (e: any) {
      setError(e.message ?? 'Failed to create task')
      setRunning(false)
    }
  }

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 z-40 bg-black/50 backdrop-blur-sm"
        onClick={onClose}
      />

      {/* Modal — centered, compact */}
      <div className="fixed left-1/2 top-1/3 -translate-x-1/2 -translate-y-1/2 z-50 w-full max-w-md">
        <div className="bg-slate-900 border border-slate-700 rounded-2xl shadow-2xl overflow-hidden">
          {/* Header */}
          <div className="flex items-center justify-between px-5 pt-5 pb-3">
            <div>
              <h2 className="text-base font-semibold text-white">Quick Task</h2>
              <p className="text-xs text-slate-500 mt-0.5">Run a one-off task — no project needed</p>
            </div>
            <button
              onClick={onClose}
              className="text-slate-500 hover:text-white text-lg leading-none"
            >
              ✕
            </button>
          </div>

          <div className="px-5 pb-5 space-y-4">
            {/* Agent picker */}
            <div>
              <label className="text-xs font-medium text-slate-400 block mb-1.5">Agent</label>
              <select
                value={agentId}
                onChange={e => setAgentId(e.target.value)}
                className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-violet-500"
              >
                {agents.length === 0 && <option value="">Loading…</option>}
                {agents.map(a => (
                  <option key={a.id} value={a.id}>{a.name}</option>
                ))}
              </select>
            </div>

            {/* Title */}
            <div>
              <label className="text-xs font-medium text-slate-400 block mb-1.5">What do you need?</label>
              <input
                autoFocus
                type="text"
                value={title}
                onChange={e => setTitle(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && !e.shiftKey && submit()}
                placeholder="e.g. Draft a job description for a data analyst"
                className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:border-violet-500"
              />
            </div>

            {/* Description */}
            <div>
              <label className="text-xs font-medium text-slate-400 block mb-1.5">
                Details <span className="text-slate-600 font-normal">(optional)</span>
              </label>
              <textarea
                value={description}
                onChange={e => setDescription(e.target.value)}
                rows={3}
                placeholder="Any extra context or requirements…"
                className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:border-violet-500 resize-none"
              />
            </div>

            {error && <p className="text-xs text-red-400">{error}</p>}

            {/* Actions */}
            <div className="flex items-center justify-between pt-1">
              <span className="text-xs text-slate-600">⌘K to toggle</span>
              <div className="flex gap-2">
                <button
                  onClick={onClose}
                  className="px-3 py-1.5 text-sm text-slate-400 hover:text-white transition-colors"
                >
                  Cancel
                </button>
                <button
                  onClick={submit}
                  disabled={running || !title.trim() || !agentId}
                  className="bg-violet-600 hover:bg-violet-500 disabled:opacity-40 text-white text-sm font-medium px-4 py-1.5 rounded-lg transition-colors"
                >
                  {running ? 'Starting…' : 'Run Task ↵'}
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </>
  )
}
