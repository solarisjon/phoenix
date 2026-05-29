import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { api, type Task, type Agent, type Project } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'
import { Card, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Label, Textarea } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { parseOutput, timeAgo, taskStatusVariant, taskStatusLabel } from '@/lib/utils'
import { MarkdownOutput } from '@/components/ui/markdown-output'

// ---- Revise modal ----

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

// ---- Task detail slide-over ----

function TaskDetail({ task, agentName, projectName, onRetry, onClose }: {
  task: Task
  agentName: string
  projectName: string
  onRetry: () => void
  onClose: () => void
}) {
  const output = parseOutput(task.output)
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 text-sm">
        <div>
          <p className="text-slate-500 text-xs mb-0.5">Project</p>
          <Link to={`/projects/${task.project_id}`} className="text-violet-400 hover:underline" onClick={onClose}>
            {projectName}
          </Link>
        </div>
        <div>
          <p className="text-slate-500 text-xs mb-0.5">Agent</p>
          <p className="text-white">{agentName}</p>
        </div>
        <div>
          <p className="text-slate-500 text-xs mb-0.5">Status</p>
          <Badge variant={taskStatusVariant(task.status)}>{taskStatusLabel(task.status)}</Badge>
        </div>
        <div>
          <p className="text-slate-500 text-xs mb-0.5">Created</p>
          <p className="text-slate-300">{timeAgo(task.created_at)}</p>
        </div>
      </div>
      {task.description && (
        <div>
          <p className="text-slate-500 text-xs mb-1">Description</p>
          <pre className="text-xs text-slate-300 whitespace-pre-wrap font-mono bg-slate-800 rounded-lg p-3 max-h-32 overflow-y-auto">{task.description}</pre>
        </div>
      )}
      <div>
        <p className="text-slate-500 text-xs mb-1">Output</p>
        <div className="bg-slate-950 border border-slate-800 rounded-lg p-3 max-h-64 overflow-y-auto">
          {output ? <MarkdownOutput content={output} /> : <span className="text-xs text-slate-500">(no output)</span>}
        </div>
      </div>
      {task.status === 'failed' && (
        <div className="flex justify-end">
          <Button onClick={onRetry}>↺ Retry Task</Button>
        </div>
      )}
    </div>
  )
}

// ---- Edit task modal ----

function EditTaskModal({ task, onDone, onClose }: { task: Task; onDone: () => void; onClose: () => void }) {
  const [title, setTitle] = useState(task.title)
  const [description, setDescription] = useState(task.description)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const save = async () => {
    if (!title.trim()) { setError('Title is required'); return }
    setSaving(true)
    try {
      await api.tasks.update(task.id, { title, description })
      onDone()
    } catch (e: any) {
      setError(e.message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-4">
      <div>
        <Label htmlFor="edit-title">Title</Label>
        <Input id="edit-title" value={title} onChange={e => setTitle(e.target.value)} />
      </div>
      <div>
        <Label htmlFor="edit-desc">Description</Label>
        <Textarea id="edit-desc" value={description} onChange={e => setDescription(e.target.value)} rows={5} />
      </div>
      {error && <p className="text-sm text-red-400">{error}</p>}
      <div className="flex gap-3 justify-end">
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button onClick={save} disabled={saving}>{saving ? 'Saving…' : 'Save'}</Button>
      </div>
    </div>
  )
}

// ---- Task card for inbox ----

function InboxTaskCard({ task, agentName, projectName, onAction }: {
  task: Task
  agentName: string
  projectName: string
  onAction: () => void
}) {
  const [revising, setRevising] = useState(false)
  const [detail, setDetail] = useState(false)
  const [editing, setEditing] = useState(false)
  const [busy, setBusy] = useState(false)

  const approve = async () => {
    setBusy(true)
    try { await api.inbox.approve(task.id); onAction() } finally { setBusy(false) }
  }
  const reject = async () => {
    if (!confirm('Reject this task? It will be marked as failed.')) return
    setBusy(true)
    try { await api.inbox.reject(task.id); onAction() } finally { setBusy(false) }
  }
  const retry = async () => {
    setBusy(true)
    try { await api.tasks.retry(task.id); onAction() } finally { setBusy(false) }
  }
  const dismiss = async () => {
    if (!confirm('Dismiss this task? It will be hidden from the inbox.')) return
    setBusy(true)
    try { await api.tasks.dismiss(task.id); onAction() } finally { setBusy(false) }
  }

  const isFailed = task.status === 'failed'
  const borderClass = isFailed ? 'border-red-900/40' : 'border-amber-900/40'
  const dotClass = isFailed ? 'bg-red-500' : 'bg-amber-500 animate-pulse'

  return (
    <>
      <Card className={borderClass}>
        <CardBody>
          <div className="flex items-start gap-4">
            <div className={`w-2 h-2 rounded-full mt-1.5 flex-shrink-0 ${dotClass}`} />
            <div className="flex-1 min-w-0">
              {/* Header row */}
              <div className="flex items-start justify-between gap-2 mb-1">
                <button className="text-left flex-1 min-w-0" onClick={() => setDetail(true)}>
                  <h3 className="font-medium text-white hover:text-violet-300 transition-colors truncate">{task.title}</h3>
                </button>
                <Badge variant={taskStatusVariant(task.status)}>{taskStatusLabel(task.status)}</Badge>
              </div>

              {/* Meta */}
              <div className="flex items-center gap-2 text-xs text-slate-500 mb-3 flex-wrap">
                <Link to={`/projects/${task.project_id}`} className="text-violet-400 hover:underline">{projectName}</Link>
                <span>·</span>
                <span>{agentName}</span>
                <span>·</span>
                <span>{timeAgo(task.created_at)}</span>
              </div>

              {/* Output preview */}
              <div className="bg-slate-800 rounded-lg p-3 mb-3 cursor-pointer" onClick={() => setDetail(true)}>
                <p className="text-xs text-slate-400 mb-1">Output</p>
                <pre className="text-xs text-slate-300 whitespace-pre-wrap font-mono max-h-24 overflow-hidden line-clamp-4">
                  {parseOutput(task.output) || '(no output)'}
                </pre>
              </div>

              {/* Actions */}
              <div className="flex gap-2 flex-wrap">
                {isFailed ? (
                  <Button size="sm" onClick={retry} disabled={busy}>↺ Retry</Button>
                ) : (
                  <>
                    <Button size="sm" onClick={approve} disabled={busy}>✓ Approve</Button>
                    <Button size="sm" variant="secondary" onClick={() => setRevising(true)} disabled={busy}>↺ Revise</Button>
                    <Button size="sm" variant="danger" onClick={reject} disabled={busy}>✕ Reject</Button>
                  </>
                )}
                <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>Edit</Button>
                <Button size="sm" variant="ghost" onClick={() => setDetail(true)}>Details</Button>
                <Button size="sm" variant="ghost" onClick={dismiss} disabled={busy}>Dismiss</Button>
              </div>
            </div>
          </div>
        </CardBody>
      </Card>

      {revising && (
        <Modal title={`Revise: ${task.title}`} onClose={() => setRevising(false)}>
          <ReviseModal task={task} onDone={() => { setRevising(false); onAction() }} onClose={() => setRevising(false)} />
        </Modal>
      )}

      {detail && (
        <Modal title={task.title} onClose={() => setDetail(false)} className="max-w-2xl">
          <TaskDetail
            task={task}
            agentName={agentName}
            projectName={projectName}
            onRetry={() => { setDetail(false); retry() }}
            onClose={() => setDetail(false)}
          />
        </Modal>
      )}

      {editing && (
        <Modal title="Edit Task" onClose={() => setEditing(false)} className="max-w-xl">
          <EditTaskModal
            task={task}
            onDone={() => { setEditing(false); onAction() }}
            onClose={() => setEditing(false)}
          />
        </Modal>
      )}
    </>
  )
}

// ---- Group heading ----

function GroupHeading({ label, count, color }: { label: string; count: number; color: string }) {
  return (
    <div className={`flex items-center gap-3 mb-3`}>
      <div className={`w-2 h-2 rounded-full flex-shrink-0 ${color}`} />
      <h2 className="text-sm font-semibold text-slate-300 uppercase tracking-wide">{label}</h2>
      <span className="text-xs text-slate-600 font-normal">({count})</span>
      <div className="flex-1 border-t border-slate-800" />
    </div>
  )
}

// ---- Page ----

export function InboxPage() {
  const [tasks, setTasks] = useState<Task[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    try {
      const [t, a, p] = await Promise.all([
        api.inbox.listAttention(),
        api.agents.list(),
        api.projects.list(),
      ])
      setTasks(t)
      setAgents(a)
      setProjects(p)
    } finally { setLoading(false) }
  }, [])

  useEffect(() => {
    load()
    const unsub = phoenixWS.on((ev) => {
      if (ev.type === 'inbox.new_item' || ev.type === 'task.status_changed') load()
    })
    return unsub
  }, [load])

  const agentName = (id: string) => agents.find(a => a.id === id)?.name ?? 'Unknown Agent'
  const projectName = (id: string) => projects.find(p => p.id === id)?.name ?? 'Unknown Project'

  const awaiting = tasks.filter(t => t.status === 'awaiting_approval')
  const failed = tasks.filter(t => t.status === 'failed')

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-bold text-white">Inbox</h1>
        <p className="text-slate-400 text-sm mt-1">
          Tasks needing your attention —{' '}
          <span className="text-amber-400">{awaiting.length} awaiting approval</span>
          {failed.length > 0 && <>, <span className="text-red-400">{failed.length} failed</span></>}
        </p>
      </div>

      {loading ? (
        <div className="text-slate-500 text-sm">Loading…</div>
      ) : tasks.length === 0 ? (
        <EmptyState icon="⊡" title="All clear"
          description="No tasks need your attention. Agents are running or idle." />
      ) : (
        <div className="space-y-8">
          {/* Awaiting approval */}
          {awaiting.length > 0 && (
            <section>
              <GroupHeading label="Awaiting Approval" count={awaiting.length} color="bg-amber-500" />
              <div className="space-y-3">
                {awaiting.map(t => (
                  <InboxTaskCard
                    key={t.id}
                    task={t}
                    agentName={agentName(t.agent_id)}
                    projectName={projectName(t.project_id)}
                    onAction={load}
                  />
                ))}
              </div>
            </section>
          )}

          {/* Failed */}
          {failed.length > 0 && (
            <section>
              <GroupHeading label="Failed" count={failed.length} color="bg-red-500" />
              <div className="space-y-3">
                {failed.map(t => (
                  <InboxTaskCard
                    key={t.id}
                    task={t}
                    agentName={agentName(t.agent_id)}
                    projectName={projectName(t.project_id)}
                    onAction={load}
                  />
                ))}
              </div>
            </section>
          )}
        </div>
      )}
    </div>
  )
}
