import * as os from 'node:os'
import { readFileSync, statfsSync } from 'node:fs'

interface CpuSample {
  idle: number
  total: number
}

export interface HostSnapshot {
  cpu: { cores: number; busy: number | null; load: number }
  memory: { total: number; used: number } | null
  disk: { total: number; used: number } | null
  uptime: number
}

export class HostMetrics {
  private previous = HostMetrics.sampleCpu()

  constructor(private readonly diskPath: string) {}

  snapshot(): HostSnapshot {
    return {
      cpu: { cores: os.cpus().length, busy: this.cpuBusy(), load: os.loadavg()[0] },
      memory: this.memory(),
      disk: this.disk(),
      uptime: os.uptime(),
    }
  }

  private cpuBusy(): number | null {
    const current = HostMetrics.sampleCpu()
    const idle = current.idle - this.previous.idle
    const total = current.total - this.previous.total
    this.previous = current
    return total > 0 ? 1 - idle / total : null
  }

  private memory(): { total: number; used: number } | null {
    const available = HostMetrics.readMemAvailable()
    const total = os.totalmem()
    if (!total) return null
    return { total, used: total - (available ?? os.freemem()) }
  }

  private disk(): { total: number; used: number } | null {
    try {
      const stat = statfsSync(this.diskPath)
      const total = Number(stat.blocks) * Number(stat.bsize)
      const free = Number(stat.bavail) * Number(stat.bsize)
      return total > 0 ? { total, used: total - free } : null
    } catch {
      return null
    }
  }

  private static sampleCpu(): CpuSample {
    let idle = 0
    let total = 0
    for (const cpu of os.cpus()) {
      for (const [kind, ms] of Object.entries(cpu.times)) {
        total += ms
        if (kind === 'idle') idle += ms
      }
    }
    return { idle, total }
  }

  private static readMemAvailable(): number | null {
    try {
      const line = readFileSync('/proc/meminfo', 'utf-8').split('\n').find((l) => l.startsWith('MemAvailable:'))
      const kb = line && Number(line.replace(/\D+/g, ''))
      return kb ? kb * 1024 : null
    } catch {
      return null
    }
  }
}
