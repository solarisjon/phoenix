import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { api, type Project } from '@/lib/api'
import { Card, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Label } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { timeAgo } from '@/lib/utils'

function ProjectForm({ initial, onSave, onClose }: {
  initial?: Project; onSave: () => void; onClose: () => void
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [description, setDescription] = useState(initial?.description ?? '')
  const [workingDir, setWorkingDir] = useState(initial?.working_dir ?? '')
  const [kind, setKind] = useState<'project' | 'monitor'>(initial?.kind ?? 'project')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const save = async () => {
    if (!name.trim()) { setError('Name is required'); return }
    setSaving(true)
    try {
      if (initial) await api.projects.update(initial.id, { name, description, working_dir: workingDir, kind })
      else await api.projects.create({ name, description, working_dir: workingDir, kind })
      onSave()
    } catch (e: any) { setError(e.message) }
    finally { setSaving(false) }
  }

  return (
    <div className="space-y-4">
      <div>
        <Label htmlFor="name">Project Name</Label>
        <Input id="name" value={name} onChange={e => setName(e.target.value)} placeholder="e.g. Build OKRs for Q3" />
      </div>
      <div>
        <Label htmlFor="desc">Description</Label>
        <Textarea id="desc" value={description} onChange={e => setDescription(e.target.value)} rows={3}
          placeholder="What is this project trying to achieve?" />
      </div>
      <div>
        <Label htmlFor="wdir">Working Directory <span className="text-slate-500 font-normal">(optional)</span></Label>
        <Input id="wdir" value={workingDir} onChange={e => setWorkingDir(e.target.value)}
          placeholder="/path/to/project — passed to coding agents as their working directory" />
        <p className="text-xs text-slate-500 mt-1">Leave blank to use the coding agent's default directory.</p>
      </div>
      {initial && (
        <div>
          <Label>Type</Label>
          <div className="flex gap-2 mt-1">
            {(['project', 'monitor'] as const).map(k => (
              <button key={k} type="button"
                onClick={() => setKind(k)}
                className={`px-3 py-1.5 rounded-lg text-sm border transition-colors ${
                  kind === k
                    ? 'bg-violet-600/20 border-violet-500 text-violet-300'
                    : 'border-slate-700 text-slate-400 hover:text-white'
                }`}>
                {k === 'project' ? '⊞ Project' : '⟳ Monitor'}
              </button>
            ))}
          </div>
          <p className="text-xs text-slate-500 mt-1">Change type to move this to the Monitors list.</p>
        </div>
      )}
      {error && <p className="text-sm text-red-400">{error}</p>}
      <div className="flex gap-3 justify-end pt-2">
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button onClick={save} disabled={saving}>{saving ? 'Saving…' : 'Save Project'}</Button>
      </div>
    </div>
  )
}

export function ProjectsPage() {
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState<Project | undefined>()

  const load = async () => {
    try { setProjects(await api.projects.list('project')) }
    finally { setLoading(false) }
  }

  useEffect(() => { load() }, [])

  const remove = async (id: string) => {
    if (!confirm('Delete this project and all its tasks?')) return
    await api.projects.delete(id)
    load()
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-white">Projects</h1>
          <p className="text-slate-400 text-sm mt-1">Workspaces where agents collaborate on tasks</p>
        </div>
        <Button onClick={() => { setEditing(undefined); setShowForm(true) }}>+ New Project</Button>
      </div>

      {loading ? (
        <div className="text-slate-500 text-sm">Loading…</div>
      ) : projects.length === 0 ? (
        <EmptyState icon="⊞" title="No projects yet"
          description="Create your first project and assign agents to start orchestrating work."
          action={<Button onClick={() => setShowForm(true)}>New Project</Button>} />
      ) : (
        <div className="grid gap-4">
          {projects.map(p => (
            <Card key={p.id} className="hover:border-slate-700 transition-colors">
              <CardBody className="flex items-start justify-between gap-4">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-3 mb-1">
                    <h3 className="font-medium text-white">{p.name}</h3>
                    <Badge variant={p.status === 'active' ? 'success' : 'muted'}>{p.status}</Badge>
                  </div>
                  {p.description && <p className="text-sm text-slate-400 line-clamp-1">{p.description}</p>}
                  {p.working_dir && (
                    <p className="text-xs text-slate-500 font-mono mt-0.5 truncate" title={p.working_dir}>
                      📁 {p.working_dir}
                    </p>
                  )}
                  <p className="text-xs text-slate-600 mt-2">Created {timeAgo(p.created_at)}</p>
                </div>
                <div className="flex gap-2 flex-shrink-0">
                  <Link to={`/projects/${p.id}`}>
                    <Button variant="secondary" size="sm">Open</Button>
                  </Link>
                  <Button variant="ghost" size="sm" onClick={() => { setEditing(p); setShowForm(true) }}>Edit</Button>
                  <Button variant="danger" size="sm" onClick={() => remove(p.id)}>Delete</Button>
                </div>
              </CardBody>
            </Card>
          ))}
        </div>
      )}

      {showForm && (
        <Modal title={editing ? 'Edit Project' : 'New Project'} onClose={() => setShowForm(false)}>
          <ProjectForm initial={editing} onSave={() => { setShowForm(false); load() }} onClose={() => setShowForm(false)} />
        </Modal>
      )}
    </div>
  )
}
