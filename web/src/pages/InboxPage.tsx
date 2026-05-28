import { useState, useEffect, useCallback } from 'react'
import { api, type Task, type Agent } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'
import { Card, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Modal } from '@/components/ui/modal'
import { Label, Textarea } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { parseOutput, timeAgo } from '@/lib/utils'

function ReviseModal({ task, onDone, onClose }: { task: Task; onDone: () => void; onClose: () => void }) {
  const [feedback, setFeedback] = useState('')
  const [saving, setSaving] = useState(false)

  const submit = async () => {
    if (!feedback.trim()) return
    setSaving(true)
    try { await api.inbox.revise(task.id, feedback); onDone() }
    catch { /* ignore */ }
    finally { setSaving(false) }
  }

  return (
    <div className="space-y-4">
      <div className="bg-slate-800 rounded-lg p-3">
        <p className="text-xs text-slate-400 mb-1">Agent output</p>
        <pre className="text-xs text-slate-300 whitespace-pre-wrap font-mono max-h-40 overflow-y-auto">{parseOutput(task.output)}</pre>
      </div>
      <div>
        <Label htmlFor="feedback">Revision Feedback</Label>
        <Textarea id="feedback" value={feedback} onChange={e => setFeedback(e.target.value)} rows={4}
          placeholder="Tell the agent what to change or improve…" />
      </div>
      <div className="flex gap-3 justify-end">
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button onClick={submit} disabled={saving || !feedback.trim()}>{saving ? 'Sending…' : 'Send for Revision'}</Button>
      </div>
    </div>
  )
}

export function InboxPage() {
  const [tasks, setTasks] = useState<Task[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [loading, setLoading] = useState(true)
  const [revising, setRevising] = useState<Task | null>(null)

  const load = useCallback(async () => {
    try {
      const [t, a] = await Promise.all([api.inbox.list(), api.agents.list()])
      setTasks(t); setAgents(a)
    } finally { setLoading(false) }
  }, [])

  useEffect(() => {
    load()
    const unsub = phoenixWS.on((ev) => {
      if (ev.type === 'inbox.new_item' || ev.type === 'task.status_changed') load()
    })
    return unsub
  }, [load])

  const approve = async (task: Task) => {
    await api.inbox.approve(task.id)
    load()
  }

  const reject = async (task: Task) => {
    if (!confirm('Reject this task? It will be marked as failed.')) return
    await api.inbox.reject(task.id)
    load()
  }

  const agentName = (id: string) => agents.find(a => a.id === id)?.name ?? 'Unknown'

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-white">Inbox</h1>
        <p className="text-slate-400 text-sm mt-1">Tasks waiting for your review and approval</p>
      </div>

      {loading ? (
        <div className="text-slate-500 text-sm">Loading…</div>
      ) : tasks.length === 0 ? (
        <EmptyState icon="⊡" title="Inbox is empty"
          description="No tasks are waiting for your approval. Agents are either running or idle." />
      ) : (
        <div className="space-y-4">
          {tasks.map(t => (
            <Card key={t.id} className="border-amber-900/40">
              <CardBody>
                <div className="flex items-start gap-4">
                  <div className="w-2 h-2 rounded-full bg-amber-500 mt-1.5 flex-shrink-0 animate-pulse" />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1">
                      <h3 className="font-medium text-white">{t.title}</h3>
                      <span className="text-xs text-slate-500">·</span>
                      <span className="text-xs text-slate-400">{agentName(t.agent_id)}</span>
                      <span className="text-xs text-slate-600 ml-auto">{timeAgo(t.created_at)}</span>
                    </div>
                    <div className="bg-slate-800 rounded-lg p-3 mb-3">
                      <p className="text-xs text-slate-400 mb-1">Agent output</p>
                      <pre className="text-xs text-slate-300 whitespace-pre-wrap font-mono max-h-48 overflow-y-auto">
                        {parseOutput(t.output)}
                      </pre>
                    </div>
                    <div className="flex gap-2">
                      <Button size="sm" onClick={() => approve(t)}>✓ Approve</Button>
                      <Button size="sm" variant="secondary" onClick={() => setRevising(t)}>↺ Revise</Button>
                      <Button size="sm" variant="danger" onClick={() => reject(t)}>✕ Reject</Button>
                    </div>
                  </div>
                </div>
              </CardBody>
            </Card>
          ))}
        </div>
      )}

      {revising && (
        <Modal title={`Revise: ${revising.title}`} onClose={() => setRevising(null)}>
          <ReviseModal task={revising} onDone={() => { setRevising(null); load() }} onClose={() => setRevising(null)} />
        </Modal>
      )}
    </div>
  )
}
