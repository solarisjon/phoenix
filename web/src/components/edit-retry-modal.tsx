import { useState } from 'react'
import { api, type Task } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input, Label, Textarea } from '@/components/ui/input'
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
        <Label htmlFor="er-desc">Description</Label>
        <Textarea
          id="er-desc"
          value={description}
          onChange={e => setDescription(e.target.value)}
          rows={6}
        />
      </div>
      {error && <p className="text-sm text-red-400">{error}</p>}
      <div className="flex gap-3 justify-end">
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button onClick={submit} disabled={submitting}>
          {submitting ? 'Creating…' : '↺ Run'}
        </Button>
      </div>
    </div>
  )
}
