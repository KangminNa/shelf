interface Job {
  name: string
  cron: string
  handler: () => Promise<void>
  timer: ReturnType<typeof setInterval>
}

/**
 * 모듈/시스템별 예약 작업 스케줄러.
 * 단순화된 cron 해석: 분/시/일 단위 인터벌로 변환된다.
 */
export class Scheduler {
  private readonly jobs = new Map<string, Job>()

  constructor(private readonly slug: string) {}

  register(cron: string, name: string, handler: () => Promise<void>): void {
    const fullName = `${this.slug}:${name}`
    const interval = Scheduler.cronToMs(cron)

    const timer = setInterval(async () => {
      try {
        await handler()
      } catch (err) {
        console.error(`[scheduler] ${fullName} failed:`, err)
      }
    }, interval)

    this.jobs.set(fullName, { name: fullName, cron, handler, timer })
    console.log(`[scheduler] registered ${fullName} (every ${interval / 1000}s)`)
  }

  unregister(name: string): void {
    const fullName = `${this.slug}:${name}`
    const job = this.jobs.get(fullName)
    if (job) {
      clearInterval(job.timer)
      this.jobs.delete(fullName)
    }
  }

  stopAll(): void {
    for (const job of this.jobs.values()) clearInterval(job.timer)
    this.jobs.clear()
  }

  private static cronToMs(cron: string): number {
    const parts = cron.split(' ')
    if (parts[0] === '*') return 60_000
    if (parts[1] === '*') return 60_000 * Number(parts[0] || 1)
    if (parts[0] === '0' && parts[1] === '*') return 3600_000
    if (parts[0] === '0' && parts[1] === '0') return 86400_000
    return 3600_000
  }
}

/** @deprecated 호환용 — new Scheduler(slug)를 사용 */
export function createScheduler(slug: string): Scheduler {
  return new Scheduler(slug)
}
