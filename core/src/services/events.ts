export class EventBus {
  private readonly handlers = new Map<string, Set<Function>>()
  private readonly wildcardHandlers = new Set<Function>()

  emit(event: string, payload?: unknown): void {
    this.handlers.get(event)?.forEach((fn) => {
      try {
        fn(payload)
      } catch (err) {
        console.error(`[events] handler error on "${event}":`, err)
      }
    })
    this.wildcardHandlers.forEach((fn) => {
      try {
        fn(event, payload)
      } catch (err) {
        console.error(`[events] wildcard handler error:`, err)
      }
    })
  }

  on(event: string, handler: (payload: any) => void): void {
    if (event === '*') {
      this.wildcardHandlers.add(handler)
      return
    }
    if (!this.handlers.has(event)) this.handlers.set(event, new Set())
    this.handlers.get(event)!.add(handler)
  }

  off(event: string, handler: Function): void {
    if (event === '*') {
      this.wildcardHandlers.delete(handler)
      return
    }
    this.handlers.get(event)?.delete(handler)
  }
}

