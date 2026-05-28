import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'
import type { Task } from './api'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function taskStatusVariant(status: Task['status']) {
  switch (status) {
    case 'completed': return 'success'
    case 'running': return 'info'
    case 'queued': return 'info'
    case 'failed': return 'danger'
    case 'awaiting_approval': return 'warning'
    default: return 'muted'
  }
}

export function taskStatusLabel(status: Task['status']) {
  switch (status) {
    case 'awaiting_approval': return 'Needs Approval'
    default: return status.charAt(0).toUpperCase() + status.slice(1)
  }
}

export function parseOutput(output: string): string {
  try {
    const parsed = JSON.parse(output)
    return parsed.text || parsed.error || JSON.stringify(parsed, null, 2)
  } catch {
    return output
  }
}

export function formatCost(cost: number): string {
  if (cost === 0) return '$0.00'
  if (cost < 0.01) return `$${cost.toFixed(4)}`
  return `$${cost.toFixed(2)}`
}

export function timeAgo(dateStr: string): string {
  const date = new Date(dateStr)
  const seconds = Math.floor((Date.now() - date.getTime()) / 1000)
  if (seconds < 60) return 'just now'
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`
  return `${Math.floor(seconds / 86400)}d ago`
}
