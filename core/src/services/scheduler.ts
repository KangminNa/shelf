import type { Scheduler } from '../types.js'

interface Job {
  name: string
  cron: string
  handler: () => Promise<void>
  timer?: ReturnType<typeof setInterval>
}

export function createScheduler(slug: string): Scheduler {
  const jobs = new Map<string, Job>()

  function parseCronToMs(cron: string): number {
    // Simplified cron: supports basic intervals
    // "* * * * *" = every minute
    // "0 * * * *" = every hour
    // "0 0 * * *" = every day
    const parts = cron.split(' ')
    if (parts[0] === '*') return 60_000
    if (parts[1] === '*') return 60_000 * Number(parts[0] || 1)
    if (parts[0] === '0' && parts[1] === '*') return 3600_000
    if (parts[0] === '0' && parts[1] === '0') return 86400_000
    return 3600_000 // default hourly
  }

  return {
    register(cron, name, handler) {
      const fullName = `${slug}:${name}`
      const interval = parseCronToMs(cron)

      const timer = setInterval(async () => {
        try {
          await handler()
        } catch (err) {
          console.error(`[scheduler] ${fullName} failed:`, err)
        }
      }, interval)

      jobs.set(fullName, { name: fullName, cron, handler, timer })
      console.log(`[scheduler] registered ${fullName} (every ${interval / 1000}s)`)
    },

    unregister(name) {
      const fullName = `${slug}:${name}`
      const job = jobs.get(fullName)
      if (job?.timer) {
        clearInterval(job.timer)
        jobs.delete(fullName)
      }
    },
  }
}
