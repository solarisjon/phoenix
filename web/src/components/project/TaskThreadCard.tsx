import { useState, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../../lib/api'
import type { Task, Agent } from '../../lib/api'
import { phoenixWS } from '../../lib/ws'
import type { TaskOutputStreamPayload, TaskStatusChangedPayload } from '../../lib/ws'
import { MarkdownOutput } from '../ui/markdown-output'

function ElapsedTimer({ startedAt }: { startedAt: string }) {
  const [elapsed, setElapsed] = useState(0)
  useEffect(() => {
    const origin = new Date(startedAt).getTime()
    const tick = () => setElapsed(Math.floor((Date.now() - origin) / 1000))
    tick()
    const id = setInterval(tick, 1000)
    return () => clearInterval(id)
  }, [startedAt])
  const m = Math.floor(elapsed / 60)
  const s = elapsed % 60
  return <span className="text-xs text-stone-400 tabular-nums">{m > 0 ? `${m}m ${s}s` : `${s}s`}</span>
}

interface Props {
  task: Task
  followUps: Task[]
  criticReviews: Task[]
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

export function TaskThreadCard({ task, followUps, criticReviews, agents, onUpdate }: Props) {
  const navigate = useNavigate()
  const [expanded, setExpanded] = useState(false)
  const [stream, setStream] = useState('')
  const streamRef = useRef<HTMLDivElement>(null)
  const [replyText, setReplyText] = useState('')
  const [sending, setSending] = useState(false)
  const [pinned, setPinned] = useState(false)
  const [pinning, setPinning] = useState(false)
  const [writingObsidian, setWritingObsidian] = useState(false)
  const [obsidianResult, setObsidianResult] = useState<string | null>(null)

  useEffect(() => {
    if (task.status !== 'running') return
    return phoenixWS.on((ev) => {
      if (ev.type === 'task.output_stream') {
        const p: TaskOutputStreamPayload = ev.payload
        if (p.task_id === task.id) {
          setStream(prev => prev + p.chunk)
          // auto-scroll
          setTimeout(() => {
            const el = streamRef.current
            if (el) el.scrollTop = el.scrollHeight
          }, 0)
        }
      }
      if (ev.type === 'task.status_changed') {
        const p: TaskStatusChangedPayload = ev.payload
        if (p.task_id === task.id) onUpdate()
      }
    })
  }, [task.id, task.status, onUpdate])

  const agentName = agents.find(a => a.id === task.agent_id)?.name ?? 'Unknown agent'
  const outputText = stream || parseOutputText(task.output)
  const preview = outputText.split('\n').slice(0, 2).join('\n')
  const hasMore = outputText.split('\n').length > 2 || outputText.length > 200

  const isRunning = task.status === 'running'
  const isQueued = task.status === 'queued'

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

  async function pinToBriefing() {
    if (pinned || pinning) return
    setPinning(true)
    try {
      const outputText = parseOutputText(task.output)
      await api.memos.create({
        project_id: task.project_id,
        task_id: task.id,
        agent_id: task.agent_id,
        agent_name: agentName,
        title: task.title,
        body: outputText || task.description || '(no output)',
        priority: 'normal',
      })
      setPinned(true)
    } catch (e) {
      console.error('Pin failed:', e)
    } finally {
      setPinning(false)
    }
  }

  async function writeToObsidian() {
    if (writingObsidian) return
    setWritingObsidian(true)
    setObsidianResult(null)
    try {
      const result = await api.obsidian.writeTask(task.id)
      setObsidianResult(result.filename)
    } catch (e) {
      console.error('Obsidian write failed:', e)
      setObsidianResult('error')
    } finally {
      setWritingObsidian(false)
    }
  }

  return (
    <div className="bg-white border border-stone-200 rounded-xl shadow-sm hover:shadow-md transition-shadow">
      {/* Card header */}
      <div className="p-4 pb-3">
        <div className="flex items-start justify-between gap-2 mb-1">
          <div className="flex items-center gap-2 flex-wrap">
            <div className="font-semibold text-stone-900 text-sm leading-snug">{task.title}</div>
            {task.is_critic_review && (
              <span className="text-xs rounded-md px-2 py-0.5 font-medium bg-amber-50 border border-amber-200 text-amber-700">
                🎯 Critic Review
              </span>
            )}
          </div>
          {statusBadge(task.status)}
        </div>
        <div className="flex items-center gap-2 text-xs text-stone-400">
          <span>{agentName}</span>
          {isRunning && task.started_at && (
            <>
              <span>·</span>
              <span className="w-1.5 h-1.5 rounded-full bg-violet-500 animate-pulse inline-block" />
              <ElapsedTimer startedAt={task.started_at} />
            </>
          )}
          {!isRunning && (
            <>
              <span>·</span>
              <span>{relativeTime(task.completed_at ?? task.created_at)}</span>
            </>
          )}
          {task.cost_usd > 0 && <><span>·</span><span>${task.cost_usd.toFixed(3)}</span></>}
        </div>
        {task.source && (
          <div className="text-xs text-stone-400 mt-0.5">↳ {task.source}</div>
        )}
      </div>

      {/* Running: live stream or intent description */}
      {isRunning && (
        <div className="mx-4 mb-3">
          {stream ? (
            <div
              ref={streamRef}
              className="bg-stone-50 border-l-2 border-violet-300 rounded-r-lg px-3 py-2 max-h-48 overflow-y-auto"
            >
              <MarkdownOutput content={stream} compact />
            </div>
          ) : task.description ? (
            <div className="flex gap-2 bg-violet-50 border border-violet-200 rounded-lg px-3 py-2">
              <span className="w-1.5 h-1.5 rounded-full bg-violet-500 animate-pulse mt-1 flex-shrink-0" />
              <p className="text-xs text-violet-700 leading-relaxed">{task.description}</p>
            </div>
          ) : (
            <div className="flex items-center gap-2">
              <span className="w-1.5 h-1.5 rounded-full bg-violet-500 animate-pulse" />
              <span className="text-xs text-violet-500">Waiting for first response…</span>
            </div>
          )}
        </div>
      )}

      {/* Queued: show what it will do */}
      {isQueued && (
        <div className="mx-4 mb-3">
          {task.description ? (
            <div className="bg-indigo-50 border border-indigo-200 rounded-lg px-3 py-2">
              <p className="text-xs text-indigo-500 mb-1 font-medium">Queued — waiting for agent</p>
              <p className="text-xs text-indigo-700 leading-relaxed">{task.description}</p>
            </div>
          ) : (
            <p className="text-xs text-stone-400 italic">Queued — waiting for agent…</p>
          )}
        </div>
      )}

      {/* Completed/failed: show output with expand */}
      {!isRunning && !isQueued && outputText && (
        <div className="mx-4 mb-3 bg-stone-50 border-l-2 border-stone-200 rounded-r-lg px-3 py-2">
          <MarkdownOutput content={expanded ? outputText : preview} compact />
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

      {/* Critic review thread */}
      {criticReviews.length > 0 && (
        <div className="mx-4 mb-3 border-l-2 border-amber-200 pl-3 space-y-2">
          <p className="text-[11px] font-semibold uppercase tracking-wide text-amber-700">Critic Reviews</p>
          {criticReviews.map(review => {
            const reviewText = parseOutputText(review.output)
            const reviewAgent = agents.find(a => a.id === review.agent_id)?.name ?? 'Unknown'
            return (
              <div key={review.id} className="bg-amber-50 border border-amber-200 rounded-lg px-3 py-2">
                <div className="flex items-center gap-2 flex-wrap mb-1">
                  <span className="text-xs font-medium text-amber-800">🎯 {reviewAgent}</span>
                  {statusBadge(review.status)}
                  <span className="text-xs text-amber-700/70">{relativeTime(review.completed_at ?? review.created_at)}</span>
                </div>
                {review.status === 'running' || review.status === 'queued' ? (
                  <p className="text-xs text-amber-700">{review.status === 'running' ? 'Review in progress…' : 'Review queued…'}</p>
                ) : reviewText ? (
                  <div className="text-xs text-amber-900 leading-relaxed">
                    <MarkdownOutput content={reviewText} compact />
                  </div>
                ) : null}
              </div>
            )
          })}
        </div>
      )}

      {/* Reply input + pin — shown on completed tasks */}
      {(task.status === 'completed' || task.status === 'failed') && (
        <div className="px-4 pb-4 space-y-2">
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
          {task.status === 'completed' && (
            <div className="flex items-center justify-end gap-3">
              {obsidianResult && obsidianResult !== 'error' ? (
                <span className="text-xs text-emerald-600">✓ Saved to Obsidian</span>
              ) : obsidianResult === 'error' ? (
                <span className="text-xs text-red-500">Obsidian write failed</span>
              ) : (
                <button
                  onClick={writeToObsidian}
                  disabled={writingObsidian}
                  className="text-xs text-stone-400 hover:text-violet-600 disabled:opacity-40 transition-colors"
                  title="Generate and save a note to Obsidian"
                >
                  {writingObsidian ? 'Writing…' : '🗒 Write to Obsidian'}
                </button>
              )}
              {pinned ? (
                <button
                  onClick={() => navigate('/briefing')}
                  className="text-xs text-violet-600 hover:text-violet-800 transition-colors"
                >
                  ✓ Pinned — view in Briefing →
                </button>
              ) : (
                <button
                  onClick={pinToBriefing}
                  disabled={pinning}
                  className="text-xs text-stone-400 hover:text-indigo-600 disabled:opacity-40 transition-colors"
                >
                  {pinning ? 'Pinning…' : '📋 Pin to Briefing'}
                </button>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
