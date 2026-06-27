import type { Project } from '@/lib/api'

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
  intervalSeconds: number
  times: string[]
  catchUp: boolean
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

export function scheduleError(v: ScheduleValue): string {
  if (v.kind === 'daily' && normaliseTimes(v.times).length === 0) {
    return 'Add at least one time for a daily schedule'
  }
  return ''
}

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
