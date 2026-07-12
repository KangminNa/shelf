import type { EventBus } from '../types.js'

export function createEventBus(): EventBus {
  const handlers = new Map<string, Set<Function>>()
  const wildcardHandlers = new Set<Function>()

  return {
    emit(event: string, payload?: unknown) {
      handlers.get(event)?.forEach((fn) => {
        try {
          fn(payload)
        } catch (err) {
          console.error(`[events] handler error on "${event}":`, err)
        }
      })
      wildcardHandlers.forEach((fn) => {
        try {
          fn(event, payload)
        } catch (err) {
          console.error(`[events] wildcard handler error:`, err)
        }
      })
    },

    on(event: string, handler: (payload: any) => void) {
      if (event === '*') {
        wildcardHandlers.add(handler)
        return
      }
      if (!handlers.has(event)) handlers.set(event, new Set())
      handlers.get(event)!.add(handler)
    },

    off(event: string, handler: Function) {
      if (event === '*') {
        wildcardHandlers.delete(handler)
        return
      }
      handlers.get(event)?.delete(handler)
    },
  }
}
