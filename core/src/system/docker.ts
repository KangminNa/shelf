import { execFile, spawn } from 'node:child_process'
import { promisify } from 'node:util'
import type { Logger } from '../services/log.js'

const exec = promisify(execFile)

export type ContainerStatus = 'running' | 'stopped' | 'crashed' | 'none'

export interface RunOptions {
  name: string
  image: string
  hostPort?: number | null
  containerPort?: number | null
  env?: Record<string, string>
  volumes?: string[]
  restart?: string
  network?: string
}

export class DockerError extends Error {
  constructor(message: string, public readonly stderr: string = '') {
    super(message)
  }
}

export class DockerService {
  private static readonly BUILD_TIMEOUT_MS = 15 * 60 * 1000
  private static readonly CMD_TIMEOUT_MS = 60 * 1000

  constructor(private readonly log: Logger) {}

  async available(): Promise<boolean> {
    try {
      await this.run(['version', '--format', '{{.Server.Version}}'])
      return true
    } catch {
      return false
    }
  }

  async build(tag: string, contextDir: string): Promise<string> {
    const { output } = await this.run(['build', '-t', tag, contextDir], DockerService.BUILD_TIMEOUT_MS)
    return output
  }

  async pull(image: string): Promise<string> {
    const { output } = await this.run(['pull', image], DockerService.BUILD_TIMEOUT_MS)
    return output
  }

  async runContainer(opts: RunOptions): Promise<string> {
    await this.removeContainer(opts.name).catch(() => {})

    const args = ['run', '-d', '--name', opts.name, '--restart', opts.restart || 'unless-stopped']
    if (opts.network) {
      args.push('--network', opts.network)
    }
    if (opts.hostPort && opts.containerPort) {
      args.push('-p', `${opts.hostPort}:${opts.containerPort}`)
    }
    for (const [key, value] of Object.entries(opts.env || {})) {
      args.push('-e', `${key}=${value}`)
    }
    for (const volume of opts.volumes || []) {
      args.push('-v', volume)
    }
    args.push(opts.image)

    const { output } = await this.run(args)
    this.log.info(`container ${opts.name} started (${opts.image})`)
    return output.trim()
  }

  async ensureNetwork(name: string): Promise<void> {
    try {
      await this.run(['network', 'inspect', name])
    } catch {
      await this.run(['network', 'create', name])
      this.log.info(`created docker network ${name}`)
    }
  }

  async connectNetwork(network: string, container: string): Promise<void> {
    await this.run(['network', 'connect', network, container]).catch(() => {})
  }

  async stats(namePrefix: string): Promise<Map<string, { cpu: number | null; memory: number | null; memoryLimit: number | null }>> {
    const result = new Map<string, { cpu: number | null; memory: number | null; memoryLimit: number | null }>()
    try {
      const names = await this.listContainers(namePrefix)
      if (!names.length) return result
      const { output } = await this.run(['stats', '--no-stream', '--format', '{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}', ...names])
      for (const line of output.split('\n')) {
        const [name, cpu, mem] = line.split('\t')
        if (!name) continue
        const [used, limit] = (mem || '').split('/')
        result.set(name.trim(), {
          cpu: DockerService.percent(cpu),
          memory: DockerService.size(used),
          memoryLimit: DockerService.size(limit),
        })
      }
    } catch {
      return result
    }
    return result
  }

  private static percent(text?: string): number | null {
    const value = Number((text || '').replace('%', '').trim())
    return Number.isFinite(value) ? value / 100 : null
  }

  private static size(text?: string): number | null {
    const match = /([\d.]+)\s*([KMGT]?i?B)/i.exec(text || '')
    if (!match) return null
    const scale: Record<string, number> = { b: 1, kib: 1024, kb: 1000, mib: 1024 ** 2, mb: 1000 ** 2, gib: 1024 ** 3, gb: 1000 ** 3, tib: 1024 ** 4, tb: 1000 ** 4 }
    return Number(match[1]) * (scale[match[2].toLowerCase()] ?? 1)
  }

  async listContainers(namePrefix: string): Promise<string[]> {
    try {
      const { output } = await this.run(['ps', '-a', '--filter', `name=${namePrefix}`, '--format', '{{.Names}}'])
      return output.split('\n').map((line) => line.trim()).filter(Boolean)
    } catch {
      return []
    }
  }

  async currentContainerId(): Promise<string | null> {
    try {
      const { output } = await this.run(['ps', '-q', '--filter', `id=${process.env.HOSTNAME || ''}`])
      return output.trim() || null
    } catch {
      return null
    }
  }

  async startContainer(name: string): Promise<void> {
    await this.run(['start', name])
  }

  async stopContainer(name: string): Promise<void> {
    await this.run(['stop', name]).catch(() => {})
  }

  async removeContainer(name: string): Promise<void> {
    await this.run(['rm', '-f', name])
  }

  async removeImage(tag: string): Promise<void> {
    await this.run(['rmi', '-f', tag]).catch(() => {})
  }

  async statuses(names: string[]): Promise<Map<string, ContainerStatus>> {
    const result = new Map<string, ContainerStatus>()
    if (!names.length) return result

    const output = await this.readOnly(['inspect', '-f', '{{.Name}} {{.State.Status}} {{.State.ExitCode}}', ...names])
    for (const line of output.split('\n')) {
      const [name, state, exitCode] = line.trim().split(' ')
      if (!name.startsWith('/')) continue
      result.set(name.slice(1), DockerService.toStatus(state, exitCode))
    }
    return result
  }

  private static toStatus(state?: string, exitCode?: string): ContainerStatus {
    if (state === 'running') return 'running'
    if (state === 'exited') return exitCode === '0' ? 'stopped' : 'crashed'
    return 'stopped'
  }

  async status(name: string): Promise<ContainerStatus> {
    try {
      const { output } = await this.run(['inspect', '-f', '{{.State.Status}} {{.State.ExitCode}}', name])
      const [state, exitCode] = output.trim().split(' ')
      return DockerService.toStatus(state, exitCode)
    } catch {
      return 'none'
    }
  }

  async logs(name: string, tail = 200): Promise<string[]> {
    try {
      const { output } = await this.run(['logs', '--tail', String(tail), name])
      return output.split('\n').filter((l) => l.trim())
    } catch {
      return []
    }
  }

  spawnDetached(image: string, script: string, mounts: string[]): void {
    const args = ['run', '-d', '--rm']
    for (const mount of mounts) args.push('-v', mount)
    args.push(image, 'sh', '-c', script)
    const child = spawn('docker', args, { detached: true, stdio: 'ignore' })
    child.unref()
    this.log.info(`spawned detached helper container (${image})`)
  }

  private async readOnly(args: string[]): Promise<string> {
    try {
      const { output } = await this.run(args)
      return output
    } catch (err: any) {
      return (err.output || '').toString()
    }
  }

  private async run(args: string[], timeout = DockerService.CMD_TIMEOUT_MS): Promise<{ output: string }> {
    try {
      const { stdout, stderr } = await exec('docker', args, { timeout, maxBuffer: 10 * 1024 * 1024 })
      return { output: [stdout, stderr].filter(Boolean).join('\n') }
    } catch (err: any) {
      const stderr = (err.stderr || '').toString()
      const failure = new DockerError(stderr.trim().split('\n').pop() || err.message, stderr)
      ;(failure as any).output = (err.stdout || '').toString()
      throw failure
    }
  }
}
