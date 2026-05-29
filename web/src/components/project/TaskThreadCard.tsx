import { useState } from 'react'
import { api } from '../../lib/api'
import type { Task, Agent } from '../../lib/api'

interface Props {
  task: Task
  followUps: Task[]
  agents: Agent[]
  onUpdate: () => void
}

function parseOutputText(output: string): string {
  if (!output || output === '{}') return ''
  try {
    const parsed = JSON.parse(output)
    if (parsed.text) return parsed.text
    if (parsed.error) return `Error: ${parsed.error}`
    return ''
  } catch {
    return output
  }
}

function relativeTime(dateStr: string | null): string {
  if (!dateStr) return ''
  const diff = Date.now() - new Date(dateStr).getTime()
  const m = Math.floor(diff / 60000)
  if (m < 1) return 'just now'
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

function statusBadge(status: Task['status']) {
  const styles: Record<string, string> = {
    completed: 'bg-green-50 border border-green-200 text-green-700',
    running: 'bg-violet-50 border border-violet-200 text-violet-700',
    queued: 'bg-indigo-50 border border-indigo-200 text-indigo-700',
    failed: 'bg-red-50 border border-red-200 text-red-700',
    awaiting_approval: 'bg-amber-50 border border-amber-200 text-amber-700',
    pending: 'bg-stone-50 border border-stone-200 text-stone-600',
  }
  const labels: Record<string, string> = {
    completed: 'Done', running: 'Running', queued: 'Queued',
    failed: 'Failed', awaiting_approval: 'Awaiting Approval', pending: 'Pending',
  }
  return (
    <span className={`text-xs rounded-md px-2 py-0.5 font-medium ${styles[status] ?? ''}`}>
      {labels[status] ?? status}
    </span>
  )
}

export function TaskThreadCard({ task, followUps, agents, onUpdate }: Props) {
  const [expanded, setExpanded] = useState(false)
  const [replyText, setReplyText] = useState('')
  const [sending, setSending] = useState(false)

  const agentName = agents.find(a => a.id === task.agent_id)?.name ?? 'Unknown agent'
  const outputText = parseOutputText(task.output)
  const preview = outputText.split('\n').slice(0, 2).join('\n')
  const hasMore = outputText.split('\n').length > 2 || outputText.length > 200

  async function sendFollowUp() {
    if (!replyText.trim()) return
    setSending(true)
    try {
      await api.tasks.followUp(task.id, replyText.trim())
      setReplyText('')
      onUpdate()
    } catch (e) {
      console.error('Follow-up failed:', e)
    } finally {
      setSending(false)
    }
  }

  return (
    <div className="bg-white border border-stone-200 rounded-xl shadow-sm hover:shadow-md transition-shadow">
      {/* Card header */}
      <div className="p-4 pb-3">
        <div className="flex items-start justify-between gap-2 mb-1">
          <div className="font-semibold text-stone-900 text-sm leading-snug">{task.title}</div>
          {statusBadge(task.status)}
        </div>
        <div className="text-xs text-stone-400">
          {agentName} · {relativeTime(task.completed_at ?? task.created_at)}
          {task.cost_usd > 0 && ` · $${task.cost_usd.toFixed(3)}`}
        </div>
      </div>

      {/* Output preview */}
      {outputText && (
        <div className="mx-4 mb-3 bg-stone-50 border-l-2 border-stone-200 rounded-r-lg px-3 py-2">
          <p className="text-xs text-stone-500 whitespace-pre-wrap leading-relaxed">
            {expanded ? outputText : preview}
          </p>
          {hasMore && (
            <button
              onClick={() => setExpanded(e => !e)}
              className="text-xs text-indigo-600 hover:text-indigo-800 mt-1"
            >
              {expanded ? 'show less' : 'read more'}
            </button>
          )}
        </div>
      )}

      {/* Follow-up thread */}
      {followUps.length > 0 && (
        <div className="mx-4 mb-3 border-l-2 border-stone-200 pl-3 space-y-2">
          {followUps.map(fu => {
            const fuText = parseOutputText(fu.output)
            const fuAgent = agents.find(a => a.id === fu.agent_id)?.name ?? 'Unknown'
            return (
              <div key={fu.id}>
                {/* Human instruction */}
                <p className="text-xs text-stone-600 italic mb-1">"{fu.description}"</p>
                {/* Agent response */}
                {fu.status === 'running' || fu.status === 'queued' ? (
                  <div className="bg-violet-50 border border-violet-200 rounded-lg px-3 py-2 text-xs text-violet-700">
                    ● {fu.status === 'running' ? 'Running follow-up...' : 'Queued...'} — {fuAgent}
                  </div>
                ) : fuText ? (
                  <div className="bg-stone-50 border-l-2 border-stone-200 rounded-r-lg px-3 py-2">
                    <p className="text-xs text-stone-500 whitespace-pre-wrap leading-relaxed">
                      {fuText.split('\n').slice(0, 3).join('\n')}
                      {fuText.split('\n').length > 3 && <span className="text-stone-400"> ...</span>}
                    </p>
                    <div className="flex items-center gap-2 mt-1">
                      {statusBadge(fu.status)}
                      <span className="text-xs text-stone-400">{relativeTime(fu.completed_at ?? fu.created_at)}</span>
                    </div>
                  </div>
                ) : fu.status === 'failed' ? (
                  <div className="bg-red-50 border border-red-200 rounded-lg px-3 py-2 text-xs text-red-600">
                    Follow-up failed
                  </div>
                ) : null}
              </div>
            )
          })}
        </div>
      )}

      {/* Reply input — shown on completed tasks */}
      {(task.status === 'completed' || task.status === 'failed') && (
        <div className="px-4 pb-4">
          <div className="flex gap-2 items-center bg-stone-50 border border-stone-200 rounded-lg px-3 py-2">
            <input
              type="text"
              value={replyText}
              onChange={e => setReplyText(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && !e.shiftKey && sendFollowUp()}
              placeholder="Follow up on this task..."
              disabled={sending}
              className="flex-1 bg-transparent text-xs text-stone-700 placeholder-stone-400 outline-none"
            />
            <button
              onClick={sendFollowUp}
              disabled={sending || !replyText.trim()}
              className="bg-indigo-600 hover:bg-indigo-700 disabled:opacity-40 text-white rounded-md text-xs px-2 py-1 transition-colors"
            >
              {sending ? '...' : '↵'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
