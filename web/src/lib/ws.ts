// WebSocket client with auto-reconnect and typed event dispatch

import type { AgentDraft, Task } from './api'

export type EventType =
  | 'connected'
  | 'task.status_changed'
  | 'task.output_stream'
  | 'agent.status_changed'
  | 'inbox.new_item'
  | 'agent_draft.created'
  | 'memo.created'
  | 'budget.exceeded'

export interface TaskStatusChangedPayload {
  task_id: string
  agent_id: string
  project_id: string
  status: Task['status']
  cost_usd: number
  title: string
}

export interface TaskOutputStreamPayload {
  task_id: string
  agent_id: string
  chunk: string
}

export interface AgentStatusChangedPayload {
  agent_id: string
  status: string
}

export interface InboxNewItemPayload {
  task_id: string
  agent_id: string
  project_id: string
  title: string
}

export interface MemoCreatedPayload {
  memo_id: string
  title: string
}

export interface BudgetExceededPayload {
  project_id: string
  spent_usd: number
  budget_usd: number
  period: string
}

export type WSEvent =
  | { type: 'connected'; payload: null }
  | { type: 'task.status_changed'; payload: TaskStatusChangedPayload }
  | { type: 'task.output_stream'; payload: TaskOutputStreamPayload }
  | { type: 'agent.status_changed'; payload: AgentStatusChangedPayload }
  | { type: 'inbox.new_item'; payload: InboxNewItemPayload }
  | { type: 'agent_draft.created'; payload: AgentDraft }
  | { type: 'memo.created'; payload: MemoCreatedPayload }
  | { type: 'budget.exceeded'; payload: BudgetExceededPayload }

type Handler = (event: WSEvent) => void

class PhoenixWS {
  private ws: WebSocket | null = null
  private handlers = new Set<Handler>()
  private reconnectDelay = 1000
  private stopped = false

  connect() {
    if (this.ws?.readyState === WebSocket.OPEN) return
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = `${proto}//${window.location.host}/api/ws`
    this.ws = new WebSocket(url)

    this.ws.onopen = () => {
      console.log('[Phoenix WS] connected')
      this.reconnectDelay = 1000
    }

    this.ws.onmessage = (e) => {
      try {
        const event = JSON.parse(e.data) as WSEvent
        this.handlers.forEach(h => h(event))
      } catch (err) {
        console.warn('[Phoenix WS] bad message', err)
      }
    }

    this.ws.onclose = () => {
      if (this.stopped) return
      console.log(`[Phoenix WS] disconnected, reconnecting in ${this.reconnectDelay}ms`)
      setTimeout(() => this.connect(), this.reconnectDelay)
      this.reconnectDelay = Math.min(this.reconnectDelay * 2, 30000)
    }

    this.ws.onerror = (e) => console.warn('[Phoenix WS] error', e)
  }

  on(handler: Handler): () => void {
    this.handlers.add(handler)
    return () => { this.handlers.delete(handler) }
  }

  disconnect() {
    this.stopped = true
    this.ws?.close()
  }
}

export const phoenixWS = new PhoenixWS()
