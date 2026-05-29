import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { api, type Task, type Agent, type Project, type AgentDraft, type Provider } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'
import { Card, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Label, Textarea } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { parseOutput, timeAgo, taskStatusVariant, taskStatusLabel } from '@/lib/utils'
import { MarkdownOutput } from '@/components/ui/markdown-output'
import { FollowUpThread } from '@/components/ui/follow-up-thread'

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

function TaskDetail({ task, agents, agentName, projectName, onRetry, onClose }: {
  task: Task
  agents: Agent[]
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
      <FollowUpThread task={task} agents={agents} onSent={onRetry} />
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

function InboxTaskCard({ task, agents, agentName, projectName, onAction }: {
  task: Task
  agents: Agent[]
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
            agents={agents}
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

// ---- Pending hire card ----

function HireApprovalCard({ draft, providers, onAction }: {
  draft: AgentDraft
  providers: Provider[]
  onAction: () => void
}) {
  const [editing, setEditing] = useState(false)
  const [busy, setBusy] = useState(false)
  const [selectedProvider, setSelectedProvider] = useState(draft.provider_id)
  const [editName, setEditName] = useState(draft.name)
  const [editPersona, setEditPersona] = useState(draft.persona)
  const [editInstructions, setEditInstructions] = useState(draft.instructions)
  const [editGuardrails, setEditGuardrails] = useState(draft.guardrails)
  const [expanded, setExpanded] = useState(false)
  const [error, setError] = useState('')

  const saveEdits = async () => {
    setBusy(true)
    try {
      await api.agentDrafts.update(draft.id, {
        name: editName,
        persona: editPersona,
        instructions: editInstructions,
        guardrails: editGuardrails,
        provider_id: selectedProvider,
      })
      setEditing(false)
      onAction()
    } catch (e: any) { setError(e.message) }
    finally { setBusy(false) }
  }

  const approve = async () => {
    setBusy(true)
    try { await api.agentDrafts.approve(draft.id, selectedProvider); onAction() }
    catch (e: any) { setError(e.message) }
    finally { setBusy(false) }
  }

  const reject = async () => {
    if (!confirm(`Reject hire proposal for "${draft.name}"? This cannot be undone.`)) return
    setBusy(true)
    try { await api.agentDrafts.reject(draft.id); onAction() }
    finally { setBusy(false) }
  }

  const dismiss = async () => {
    setBusy(true)
    try { await api.agentDrafts.dismiss(draft.id); onAction() }
    finally { setBusy(false) }
  }

  return (
    <Card className="border-purple-900/50 bg-purple-950/10">
      <CardBody>
        <div className="flex items-start gap-4">
          {/* Icon */}
          <div className="w-8 h-8 rounded-full bg-purple-900/40 flex items-center justify-center text-sm flex-shrink-0">
            🧑‍💼
          </div>
          <div className="flex-1 min-w-0">
            {/* Header */}
            <div className="flex items-start justify-between gap-2 mb-1">
              <div className="flex items-center gap-2">
                <Badge variant="default" className="bg-purple-900/60 text-purple-300 border-purple-700/50 text-xs">Pending Hire</Badge>
                <h3 className="font-semibold text-white">{editing ? editName : draft.name}</h3>
              </div>
            </div>

            {/* Provenance */}
            <p className="text-xs text-slate-500 mb-3">
              Proposed by <span className="text-slate-300">{draft.created_by_agent_name}</span>
              {draft.created_by_task_title && <> · via task <span className="text-slate-300">{draft.created_by_task_title}</span></>}
              {' · '}{new Date(draft.created_at).toLocaleDateString()}
            </p>

            {/* Edit mode */}
            {editing ? (
              <div className="space-y-3 mb-4">
                <div>
                  <Label htmlFor={`dn-${draft.id}`}>Name</Label>
                  <Input id={`dn-${draft.id}`} value={editName} onChange={e => setEditName(e.target.value)} />
                </div>
                <div>
                  <Label htmlFor={`dp-${draft.id}`}>Persona</Label>
                  <Textarea id={`dp-${draft.id}`} value={editPersona} onChange={e => setEditPersona(e.target.value)} rows={3} />
                </div>
                <div>
                  <Label htmlFor={`di-${draft.id}`}>Instructions</Label>
                  <Textarea id={`di-${draft.id}`} value={editInstructions} onChange={e => setEditInstructions(e.target.value)} rows={5} />
                </div>
                <div>
                  <Label htmlFor={`dg-${draft.id}`}>Guardrails</Label>
                  <Textarea id={`dg-${draft.id}`} value={editGuardrails} onChange={e => setEditGuardrails(e.target.value)} rows={3} />
                </div>
                {error && <p className="text-xs text-red-400">{error}</p>}
                <div className="flex gap-2">
                  <Button size="sm" onClick={saveEdits} disabled={busy}>{busy ? 'Saving…' : 'Save'}</Button>
                  <Button size="sm" variant="secondary" onClick={() => setEditing(false)}>Cancel</Button>
                </div>
              </div>
            ) : (
              /* Preview / expanded view */
              <div className="mb-4">
                <div className="bg-slate-900 rounded-lg p-3 text-xs text-slate-300 mb-2">
                  <p className="text-slate-500 mb-1 font-medium">Persona</p>
                  <p className="whitespace-pre-wrap">{draft.persona}</p>
                </div>
                {expanded && (
                  <>
                    <div className="bg-slate-900 rounded-lg p-3 text-xs text-slate-300 mb-2">
                      <p className="text-slate-500 mb-1 font-medium">Instructions</p>
                      <p className="whitespace-pre-wrap">{draft.instructions}</p>
                    </div>
                    {draft.guardrails && (
                      <div className="bg-slate-900 rounded-lg p-3 text-xs text-slate-300 mb-2">
                        <p className="text-slate-500 mb-1 font-medium">Guardrails</p>
                        <p className="whitespace-pre-wrap">{draft.guardrails}</p>
                      </div>
                    )}
                  </>
                )}
                <button
                  className="text-xs text-violet-400 hover:text-violet-300 mt-1"
                  onClick={() => setExpanded(e => !e)}
                >
                  {expanded ? '▲ Show less' : '▼ Show instructions & guardrails'}
                </button>
              </div>
            )}

            {/* Provider picker + actions */}
            {!editing && (
              <div className="flex items-center gap-3 flex-wrap">
                <div className="flex items-center gap-2">
                  <label className="text-xs text-slate-400">Provider:</label>
                  <select
                    value={selectedProvider}
                    onChange={e => setSelectedProvider(e.target.value)}
                    className="text-xs bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
                  >
                    {providers.map(p => (
                      <option key={p.id} value={p.id}>{p.name}</option>
                    ))}
                  </select>
                </div>
                <Button size="sm" onClick={approve} disabled={busy || !selectedProvider}>
                  ✓ Approve &amp; Create Agent
                </Button>
                <Button size="sm" variant="ghost" onClick={() => { setEditing(true); setError('') }}>
                  ✏ Edit
                </Button>
                <Button size="sm" variant="danger" onClick={reject} disabled={busy}>
                  ✕ Reject
                </Button>
                <Button size="sm" variant="ghost" onClick={dismiss} disabled={busy}>
                  Dismiss
                </Button>
              </div>
            )}
          </div>
        </div>
      </CardBody>
    </Card>
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
  const [drafts, setDrafts] = useState<AgentDraft[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [projects, setProjects] = useState<Project[]>([])
  const [providers, setProviders] = useState<Provider[]>([])
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    try {
      const [t, d, a, p, prov] = await Promise.all([
        api.inbox.listAttention(),
        api.agentDrafts.list(),
        api.agents.list(),
        api.projects.list(),
        api.providers.list(),
      ])
      setTasks(t)
      setDrafts(d)
      setAgents(a)
      setProjects(p)
      setProviders(prov)
    } finally { setLoading(false) }
  }, [])

  useEffect(() => {
    load()
    const unsub = phoenixWS.on((ev) => {
      if (
        ev.type === 'inbox.new_item' ||
        ev.type === 'task.status_changed' ||
        ev.type === 'agent_draft.created'
      ) load()
    })
    return unsub
  }, [load])

  const agentName = (id: string) => agents.find(a => a.id === id)?.name ?? 'Unknown Agent'
  const projectName = (id: string) => projects.find(p => p.id === id)?.name ?? 'Unknown Project'

  const awaiting = tasks.filter(t => t.status === 'awaiting_approval')
  const failed = tasks.filter(t => t.status === 'failed')
  const totalItems = awaiting.length + failed.length + drafts.length

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-bold text-white">Inbox</h1>
        <p className="text-slate-400 text-sm mt-1">
          {drafts.length > 0 && <><span className="text-purple-400">{drafts.length} pending hire{drafts.length !== 1 ? 's' : ''}</span>{(awaiting.length > 0 || failed.length > 0) ? ', ' : ''}</>}
          {awaiting.length > 0 && <><span className="text-amber-400">{awaiting.length} awaiting approval</span>{failed.length > 0 ? ', ' : ''}</>}
          {failed.length > 0 && <span className="text-red-400">{failed.length} failed</span>}
          {totalItems === 0 && 'All clear — nothing needs your attention'}
        </p>
      </div>

      {loading ? (
        <div className="text-slate-500 text-sm">Loading…</div>
      ) : totalItems === 0 ? (
        <EmptyState icon="⊡" title="All clear"
          description="No tasks need your attention. Agents are running or idle." />
      ) : (
        <div className="space-y-8">
          {/* Pending hires */}
          {drafts.length > 0 && (
            <section>
              <GroupHeading label="Pending Hires" count={drafts.length} color="bg-purple-500" />
              <div className="space-y-3">
                {drafts.map(d => (
                  <HireApprovalCard
                    key={d.id}
                    draft={d}
                    providers={providers}
                    onAction={load}
                  />
                ))}
              </div>
            </section>
          )}

          {/* Awaiting approval */}
          {awaiting.length > 0 && (
            <section>
              <GroupHeading label="Awaiting Approval" count={awaiting.length} color="bg-amber-500" />
              <div className="space-y-3">
                {awaiting.map(t => (
                  <InboxTaskCard
                    key={t.id}
                    task={t}
                    agents={agents}
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
                    agents={agents}
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
