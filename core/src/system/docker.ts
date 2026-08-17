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

  async status(name: string): Promise<ContainerStatus> {
    try {
      const { output } = await this.run(['inspect', '-f', '{{.State.Status}} {{.State.ExitCode}}', name])
      const [state, exitCode] = output.trim().split(' ')
      if (state === 'running') return 'running'
      if (state === 'exited') return exitCode === '0' ? 'stopped' : 'crashed'
      return 'stopped'
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

  private async run(args: string[], timeout = DockerService.CMD_TIMEOUT_MS): Promise<{ output: string }> {
    try {
      const { stdout, stderr } = await exec('docker', args, { timeout, maxBuffer: 10 * 1024 * 1024 })
      return { output: [stdout, stderr].filter(Boolean).join('\n') }
    } catch (err: any) {
      const stderr = (err.stderr || '').toString()
      throw new DockerError(stderr.trim().split('\n').pop() || err.message, stderr)
    }
  }
}
