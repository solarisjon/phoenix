import { Button } from '@/components/ui/button'
import { Input, Label, Select } from '@/components/ui/input'
import type { Project } from '@/lib/api'

// Interval options: label → seconds. Shared between create and edit forms.
export const INTERVAL_OPTIONS = [
  { label: 'No schedule (manual only)', value: 0 },
  { label: 'Every 5 minutes', value: 300 },
  { label: 'Every 15 minutes', value: 900 },
  { label: 'Every 30 minutes', value: 1800 },
  { label: 'Every hour', value: 3600 },
  { label: 'Every 6 hours', value: 21600 },
  { label: 'Every 12 hours', value: 43200 },
  { label: 'Every day', value: 86400 },
]

export interface ScheduleValue {
  kind: 'interval' | 'daily'
  intervalSeconds: number // 0 = no schedule
  times: string[]         // HH:MM, daily only
  catchUp: boolean        // daily only
}

// scheduleFromProject derives editable schedule state from a project, treating
// a missing/empty schedule_kind as interval (backward compatible).
export function scheduleFromProject(p?: Project): ScheduleValue {
  if (p?.schedule_kind === 'daily') {
    return {
      kind: 'daily',
      intervalSeconds: 0,
      times: p.schedule_times ?? [],
      catchUp: p.schedule_catch_up ?? false,
    }
  }
  return {
    kind: 'interval',
    intervalSeconds: p?.schedule_interval ?? 0,
    times: [],
    catchUp: false,
  }
}

// schedulePayload builds the API fields for a schedule value.
export function schedulePayload(v: ScheduleValue): Partial<Project> {
  if (v.kind === 'daily') {
    return {
      schedule_kind: 'daily',
      schedule_times: normaliseTimes(v.times),
      schedule_catch_up: v.catchUp,
      schedule_interval: null,
    }
  }
  return {
    schedule_kind: 'interval',
    schedule_times: [],
    schedule_catch_up: false,
    schedule_interval: v.intervalSeconds > 0 ? v.intervalSeconds : null,
  }
}

// scheduleError returns a validation message, or '' if the value is valid.
export function scheduleError(v: ScheduleValue): string {
  if (v.kind === 'daily' && normaliseTimes(v.times).length === 0) {
    return 'Add at least one time for a daily schedule'
  }
  return ''
}

function normaliseTimes(times: string[]): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const t of times) {
    const v = t.trim()
    if (/^\d{2}:\d{2}$/.test(v) && !seen.has(v)) {
      seen.add(v)
      out.push(v)
    }
  }
  out.sort()
  return out
}

// scheduleSummary returns a short human label for cards/detail headers.
export function scheduleSummary(p: Project): string {
  if (p.schedule_kind === 'daily') {
    const times = p.schedule_times ?? []
    if (times.length === 0) return 'No schedule'
    const label = `Daily at ${times.join(', ')}`
    return p.schedule_catch_up ? `${label} (catch-up)` : label
  }
  const secs = p.schedule_interval
  if (!secs) return 'No schedule'
  if (secs < 60) return `Every ${secs}s`
  if (secs < 3600) return `Every ${Math.round(secs / 60)}m`
  if (secs < 86400) return `Every ${Math.round(secs / 3600)}h`
  return `Every ${Math.round(secs / 86400)}d`
}

export function ScheduleEditor({ value, onChange, idPrefix = 'sched' }: {
  value: ScheduleValue
  onChange: (v: ScheduleValue) => void
  idPrefix?: string
}) {
  const setKind = (kind: 'interval' | 'daily') => onChange({ ...value, kind })
  const setInterval = (intervalSeconds: number) => onChange({ ...value, intervalSeconds })
  const setCatchUp = (catchUp: boolean) => onChange({ ...value, catchUp })

  const setTime = (i: number, t: string) => {
    const times = [...value.times]
    times[i] = t
    onChange({ ...value, times })
  }
  const addTime = () => onChange({ ...value, times: [...value.times, '07:00'] })
  const removeTime = (i: number) =>
    onChange({ ...value, times: value.times.filter((_, idx) => idx !== i) })

  return (
    <div className="space-y-3">
      <div>
        <Label htmlFor={`${idPrefix}-kind`}>Schedule type</Label>
        <Select
          id={`${idPrefix}-kind`}
          value={value.kind}
          onChange={e => setKind(e.target.value as 'interval' | 'daily')}
        >
          <option value="interval">Interval (every N minutes/hours)</option>
          <option value="daily">Daily at set times</option>
        </Select>
      </div>

      {value.kind === 'interval' ? (
        <div>
          <Label htmlFor={`${idPrefix}-interval`}>Interval</Label>
          <Select
            id={`${idPrefix}-interval`}
            value={String(value.intervalSeconds)}
            onChange={e => setInterval(Number(e.target.value))}
          >
            {INTERVAL_OPTIONS.map(o => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </Select>
          {value.intervalSeconds === 0 && (
            <p className="text-xs text-slate-500 mt-1">You can trigger runs manually from the monitor page.</p>
          )}
        </div>
      ) : (
        <div className="space-y-2">
          <Label>Run times (server local time)</Label>
          {value.times.length === 0 && (
            <p className="text-xs text-slate-500">No times yet — add one below.</p>
          )}
          <div className="space-y-2">
            {value.times.map((t, i) => (
              <div key={i} className="flex items-center gap-2">
                <Input
                  type="time"
                  value={t}
                  onChange={e => setTime(i, e.target.value)}
                  className="w-36"
                />
                <button
                  type="button"
                  onClick={() => removeTime(i)}
                  className="text-xs text-slate-400 hover:text-red-400 transition-colors"
                >
                  Remove
                </button>
              </div>
            ))}
          </div>
          <Button variant="secondary" onClick={addTime} className="text-xs">+ Add time</Button>

          <label className="flex items-start gap-2 pt-1 cursor-pointer">
            <input
              type="checkbox"
              checked={value.catchUp}
              onChange={e => setCatchUp(e.target.checked)}
              className="mt-0.5"
            />
            <span className="text-sm text-slate-300">
              Catch up missed runs
              <span className="block text-xs text-slate-500">
                If the host was offline at the scheduled time, run once at the next opportunity
                the same day. Multi-day outages still trigger only a single run.
              </span>
            </span>
          </label>
        </div>
      )}
    </div>
  )
}
