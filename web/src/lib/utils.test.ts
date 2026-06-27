import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  cn,
  taskStatusVariant,
  taskStatusLabel,
  parseOutput,
  formatCost,
  timeAgo,
} from '../lib/utils'

describe('cn', () => {
  it('merges class names', () => {
    expect(cn('a', 'b')).toBe('a b')
  })

  it('deduplicates tailwind classes', () => {
    expect(cn('p-2', 'p-4')).toBe('p-4')
  })

  it('handles conditional classes', () => {
    const show = false
    expect(cn('base', show && 'ignored', 'added')).toBe('base added')
  })
})

describe('taskStatusVariant', () => {
  it.each([
    ['completed', 'success'],
    ['running', 'info'],
    ['queued', 'info'],
    ['failed', 'danger'],
    ['awaiting_approval', 'warning'],
    ['pending', 'muted'],
  ] as const)('maps %s → %s', (status, variant) => {
    expect(taskStatusVariant(status)).toBe(variant)
  })
})

describe('taskStatusLabel', () => {
  it('capitalises normal statuses', () => {
    expect(taskStatusLabel('completed')).toBe('Completed')
    expect(taskStatusLabel('running')).toBe('Running')
    expect(taskStatusLabel('failed')).toBe('Failed')
  })

  it('maps awaiting_approval to "Needs You"', () => {
    expect(taskStatusLabel('awaiting_approval')).toBe('Needs You')
  })
})

describe('parseOutput', () => {
  it('returns empty string for blank output', () => {
    expect(parseOutput('')).toBe('')
    expect(parseOutput('  ')).toBe('')
    expect(parseOutput('{}')).toBe('')
  })

  it('extracts text field from JSON', () => {
    expect(parseOutput(JSON.stringify({ text: 'hello' }))).toBe('hello')
  })

  it('extracts error field from JSON', () => {
    expect(parseOutput(JSON.stringify({ error: 'oops' }))).toBe('oops')
  })

  it('returns raw string for non-JSON', () => {
    expect(parseOutput('plain text')).toBe('plain text')
  })
})

describe('formatCost', () => {
  it('formats zero as $0.00', () => {
    expect(formatCost(0)).toBe('$0.00')
  })

  it('formats small amounts with 4 decimal places', () => {
    expect(formatCost(0.001)).toBe('$0.0010')
  })

  it('formats normal amounts with 2 decimal places', () => {
    expect(formatCost(1.5)).toBe('$1.50')
    expect(formatCost(0.05)).toBe('$0.05')
  })
})

describe('timeAgo', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2024-01-01T12:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns "just now" for recent times', () => {
    expect(timeAgo('2024-01-01T11:59:50Z')).toBe('just now')
  })

  it('returns minutes ago', () => {
    expect(timeAgo('2024-01-01T11:55:00Z')).toBe('5m ago')
  })

  it('returns hours ago', () => {
    expect(timeAgo('2024-01-01T09:00:00Z')).toBe('3h ago')
  })

  it('returns days ago', () => {
    expect(timeAgo('2023-12-30T12:00:00Z')).toBe('2d ago')
  })
})
