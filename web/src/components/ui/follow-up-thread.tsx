/**
 * FollowUpThread — chat-style refinement UI for any task detail modal.
 *
 * Shows existing follow-up tasks as a thread, plus an input to send a new one.
 * Visible on any completed or failed task (and awaiting_approval).
 */

import { useState, useEffect, useCallback } from 'react'
import { api } from '@/lib/api'
import type { Task, Agent } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { MarkdownOutput } from '@/components/ui/markdown-output'
import { taskStatusVariant, taskStatusLabel, parseOutput, timeAgo } from '@/lib/utils'

interface Props {
  task: Task
  agents: Agent[]
  /** Called when a follow-up is sent, so the parent can refresh */
  onSent?: () => void
}

export function FollowUpThread({ task, agents, onSent }: Props) {
  const [followUps, setFollowUps] = useState<Task[]>([])
  const [text, setText] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')

  const loadFollowUps = useCallback(async () => {
    try {
      // List all tasks for this project then filter by follow_up_of === task.id
      const all = await api.tasks.list(task.project_id)
      const children = all
        .filter(t => t.follow_up_of === task.id)
        .sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime())
      setFollowUps(children)
    } catch { /* ignore */ }
  }, [task.id, task.project_id])

  useEffect(() => {
    loadFollowUps()
  }, [loadFollowUps])

  const send = async () => {
    if (!text.trim()) return
    setError('')
    setSending(true)
    try {
      await api.tasks.followUp(task.id, text.trim())
      setText('')
      await loadFollowUps()
      onSent?.()
    } catch (e: any) {
      setError(e.message ?? 'Failed to send follow-up')
    } finally {
      setSending(false)
    }
  }

  const canFollowUp = task.status === 'completed' || task.status === 'failed' || task.status === 'awaiting_approval'

  if (!canFollowUp && followUps.length === 0) return null

  return (
    <div className="border-t border-slate-800 pt-4 space-y-3">
      <p className="text-xs font-medium text-slate-400 uppercase tracking-wide">
        Follow-up Thread
        {followUps.length > 0 && (
          <span className="ml-2 text-slate-600 normal-case font-normal">({followUps.length} message{followUps.length !== 1 ? 's' : ''})</span>
        )}
      </p>

      {/* Existing follow-up tasks */}
      {followUps.length > 0 && (
        <div className="space-y-3">
          {followUps.map(fu => {
            const agentName = agents.find(a => a.id === fu.agent_id)?.name ?? 'Agent'
            const fuOutput = parseOutput(fu.output)
            return (
              <div key={fu.id} className="space-y-1.5">
                {/* Human message bubble */}
                {fu.description && (
                  <div className="flex justify-end">
                    <div className="bg-violet-900/40 border border-violet-800/40 rounded-2xl rounded-tr-sm px-3 py-2 max-w-[85%]">
                      <p className="text-sm text-violet-100">{fu.description}</p>
                    </div>
                  </div>
                )}
                {/* Agent response bubble */}
                <div className="flex justify-start">
                  <div className="max-w-[92%]">
                    <p className="text-xs text-slate-500 mb-1 ml-1">{agentName} · {timeAgo(fu.created_at)}</p>
                    {(fu.status === 'running' || fu.status === 'queued') ? (
                      <div className="bg-slate-800 border border-slate-700 rounded-2xl rounded-tl-sm px-3 py-2">
                        <div className="flex items-center gap-1.5">
                          <span className="w-1.5 h-1.5 rounded-full bg-violet-400 animate-pulse" />
                          <span className="text-xs text-slate-400">
                            {fu.status === 'running' ? 'Working on it…' : 'Queued…'}
                          </span>
                        </div>
                      </div>
                    ) : fuOutput ? (
                      <div className="bg-slate-800 border border-slate-700 rounded-2xl rounded-tl-sm px-3 py-2">
                        <MarkdownOutput content={fuOutput} />
                        <div className="mt-1.5 flex items-center gap-2">
                          <Badge variant={taskStatusVariant(fu.status)} className="text-xs">
                            {taskStatusLabel(fu.status)}
                          </Badge>
                        </div>
                      </div>
                    ) : fu.status === 'failed' ? (
                      <div className="bg-red-950/40 border border-red-900/40 rounded-2xl rounded-tl-sm px-3 py-2">
                        <p className="text-xs text-red-400">Follow-up failed</p>
                      </div>
                    ) : null}
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      )}

      {/* Input — only for terminal states */}
      {canFollowUp && (
        <div className="space-y-1.5">
          <div className="flex gap-2 items-end bg-slate-800 border border-slate-700 rounded-xl px-3 py-2 focus-within:border-violet-600 transition-colors">
            <textarea
              value={text}
              onChange={e => setText(e.target.value)}
              onKeyDown={e => {
                if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() }
              }}
              placeholder="Ask the agent to refine or follow up on this task… (Enter to send)"
              disabled={sending}
              rows={2}
              className="flex-1 bg-transparent text-sm text-slate-200 placeholder-slate-500 outline-none resize-none"
            />
            <button
              onClick={send}
              disabled={sending || !text.trim()}
              className="flex-shrink-0 bg-violet-600 hover:bg-violet-500 disabled:opacity-40 disabled:cursor-not-allowed text-white rounded-lg text-xs px-3 py-1.5 transition-colors font-medium"
            >
              {sending ? '…' : '↵ Send'}
            </button>
          </div>
          {error && <p className="text-xs text-red-400">{error}</p>}
          <p className="text-xs text-slate-600">Shift+Enter for a new line · Enter to send</p>
        </div>
      )}
    </div>
  )
}
