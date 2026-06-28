import { diffLines } from 'diff'
import { Modal } from '@/components/ui/modal'
import { parseOutput, timeAgo } from '@/lib/utils'
import type { Task } from '@/lib/api'

interface TaskDiffModalProps {
  older: Task
  newer: Task
  onClose: () => void
}

export function TaskDiffModal({ older, newer, onClose }: TaskDiffModalProps) {
  const oldText = parseOutput(older.output) ?? ''
  const newText = parseOutput(newer.output) ?? ''
  const changes = diffLines(oldText, newText)

  return (
    <Modal title="Compare runs" onClose={onClose} className="max-w-4xl w-full">
      <div className="flex gap-4 text-xs text-slate-400 mb-3">
        <span>← {timeAgo(older.created_at)} · {older.title}</span>
        <span className="mx-auto">→</span>
        <span>{newer.title} · {timeAgo(newer.created_at)} →</span>
      </div>
      <div className="font-mono text-xs overflow-y-auto max-h-[60vh] rounded border border-slate-700 bg-slate-900">
        {changes.map((part, i) => {
          if (part.added) {
            return (
              <div key={i} className="bg-green-900/30 text-green-300 whitespace-pre-wrap px-3 py-0.5 border-l-2 border-green-500">
                {part.value}
              </div>
            )
          }
          if (part.removed) {
            return (
              <div key={i} className="bg-red-900/30 text-red-300 whitespace-pre-wrap px-3 py-0.5 border-l-2 border-red-500 line-through opacity-70">
                {part.value}
              </div>
            )
          }
          return (
            <div key={i} className="text-slate-400 whitespace-pre-wrap px-3 py-0.5">
              {part.value}
            </div>
          )
        })}
        {changes.length === 0 && (
          <p className="text-slate-500 px-3 py-4 text-center">Outputs are identical.</p>
        )}
      </div>
    </Modal>
  )
}
