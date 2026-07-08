import { useState, useEffect, useRef } from 'react'
import { api, type Task, type Provider } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input, Label, Textarea, Select } from '@/components/ui/input'
import { Modal } from '@/components/ui/modal'
import { formatCost } from '@/lib/utils'
import { getErrorMessage } from '@/lib/errors'

// EditRetryModal pre-fills the original task's title and description so the
// user can tweak the prompt before re-running. The new task is linked to the
// original via follow_up_of so it appears in the follow-up thread.
export function EditRetryModal({ task, onDone, onClose }: {
  task: Task
  onDone: () => void
  onClose: () => void
}) {
  const [title, setTitle] = useState(task.title)
  const [description, setDescription] = useState(task.description)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  // AI assist
  const [providers, setProviders] = useState<Provider[]>([])
  const [showAI, setShowAI] = useState(false)
  const [aiHint, setAiHint] = useState('')
  const [aiProviderID, setAiProviderID] = useState('')
  const [aiGenerating, setAiGenerating] = useState(false)
  const [aiError, setAiError] = useState('')

  // Cost estimate
  type EstimateResult = {
    supported: boolean
    prompt_tokens: number
    estimated_output_tokens: { low: number; high: number }
    estimated_cost_usd: { low: number; high: number }
    provider: { type: string; model: string }
  }
  const [estimating, setEstimating] = useState(false)
  const [estimate, setEstimate] = useState<EstimateResult | null>(null)
  const estimateDebounce = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    api.providers.list().then(list => {
      setProviders(list)
      setAiProviderID(list.find(p => p.type === 'llm')?.id ?? list[0]?.id ?? '')
    }).catch(() => {})
  }, [])

  // Debounced cost estimate when title or description changes
  useEffect(() => {
    if (!task.agent_id) return
    if (estimateDebounce.current) clearTimeout(estimateDebounce.current)
    setEstimate(null)
    estimateDebounce.current = setTimeout(async () => {
      if (!title.trim() && !description.trim()) return
      setEstimating(true)
      try {
        const res = await api.tasks.estimate({ agent_id: task.agent_id, title: title.trim(), description: description.trim() })
        setEstimate(res)
      } catch { /* ignore */ } finally {
        setEstimating(false)
      }
    }, 800)
    return () => { if (estimateDebounce.current) clearTimeout(estimateDebounce.current) }
  }, [title, description, task.agent_id])

  const generateDescription = async () => {
    if (!title.trim()) { setAiError('Enter a task title first'); return }
    setAiGenerating(true)
    setAiError('')
    try {
      const result = await api.tasks.generateDescription(title, aiHint, aiProviderID)
      setDescription(result.description)
      setShowAI(false)
      setAiHint('')
    } catch (e: unknown) {
      setAiError(getErrorMessage(e))
    } finally {
      setAiGenerating(false)
    }
  }

  const submit = async () => {
    if (!title.trim()) { setError('Title is required'); return }
    setSubmitting(true)
    setError('')
    try {
      await api.tasks.create({
        project_id: task.project_id,
        agent_id: task.agent_id,
        title: title.trim(),
        description,
        follow_up_of: task.id,
      })
      onDone()
    } catch (err: unknown) {
      setError(getErrorMessage(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal title="Edit & Retry" onClose={onClose} className="max-w-xl">
      <div className="space-y-4">
        <p className="text-xs text-slate-400">
          Edit the prompt below and submit to create a new run linked to this task.
        </p>
        <div>
          <Label htmlFor="er-title">Title</Label>
          <Input
            id="er-title"
            value={title}
            onChange={e => setTitle(e.target.value)}
          />
        </div>
        <div>
          <div className="flex items-center justify-between mb-1">
            <Label htmlFor="er-desc">Description</Label>
            {providers.length > 0 && (
              <button
                type="button"
                onClick={() => { setShowAI(v => !v); setAiError('') }}
                className="text-xs text-violet-400 hover:text-violet-300 transition-colors"
              >
                ✦ {showAI ? 'Hide AI assist' : 'Generate with AI'}
              </button>
            )}
          </div>
          {showAI && (
            <div className="mb-2 rounded-lg border border-violet-800/50 bg-violet-950/30 p-3 space-y-2">
              <p className="text-xs text-slate-400">AI will write detailed task instructions from the title.</p>
              {providers.length > 1 && (
                <Select value={aiProviderID} onChange={e => setAiProviderID(e.target.value)}>
                  {providers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                </Select>
              )}
              <Textarea
                value={aiHint}
                onChange={e => setAiHint(e.target.value)}
                rows={2}
                placeholder="Additional context (optional)…"
              />
              {aiError && <p className="text-xs text-red-400">{aiError}</p>}
              <div className="flex justify-end">
                <Button size="sm" onClick={generateDescription} disabled={aiGenerating || !title.trim()}>
                  {aiGenerating ? 'Generating…' : '✦ Generate'}
                </Button>
              </div>
            </div>
          )}
          <Textarea
            id="er-desc"
            value={description}
            onChange={e => setDescription(e.target.value)}
            rows={6}
          />
        </div>

        {/* Cost estimate */}
        <div className="flex items-center justify-between text-xs text-slate-500">
          <span>{estimating ? 'Estimating cost…' : 'Cost estimate (auto-updates)'}</span>
          {estimate && (
            <span>
              {estimate.supported ? (
                <span className="text-emerald-400 font-medium">
                  ~{formatCost(estimate.estimated_cost_usd.low)}–{formatCost(estimate.estimated_cost_usd.high)}
                </span>
              ) : (
                <span className="text-slate-600">pricing not available</span>
              )}
            </span>
          )}
        </div>

        {error && <p className="text-sm text-red-400">{error}</p>}
        <div className="flex gap-3 justify-end">
          <Button variant="secondary" onClick={onClose}>Cancel</Button>
          <Button onClick={submit} disabled={submitting}>
            {submitting ? 'Creating…' : '↺ Run'}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
