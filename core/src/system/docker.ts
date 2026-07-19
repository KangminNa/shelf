import { execFile } from 'node:child_process'
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
  volumes?: string[] // "host:container" 형식
  restart?: string // 기본 unless-stopped
}

export class DockerError extends Error {
  constructor(message: string, public readonly stderr: string = '') {
    super(message)
  }
}

/**
 * Docker CLI 래퍼.
 * Shelf가 관리하는 컨테이너는 "shelf-{app}" 이름 규칙을 따른다.
 */
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

  /** 이미지 빌드. 빌드 로그 전체를 반환 */
  async build(tag: string, contextDir: string): Promise<string> {
    const { output } = await this.run(['build', '-t', tag, contextDir], DockerService.BUILD_TIMEOUT_MS)
    return output
  }

  async pull(image: string): Promise<string> {
    const { output } = await this.run(['pull', image], DockerService.BUILD_TIMEOUT_MS)
    return output
  }

  /** 기존 동명 컨테이너를 제거하고 새로 실행 */
  async runContainer(opts: RunOptions): Promise<string> {
    await this.removeContainer(opts.name).catch(() => {})

    const args = ['run', '-d', '--name', opts.name, '--restart', opts.restart || 'unless-stopped']
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
    return output.trim() // container id
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

  /** 컨테이너 상태: running / stopped(정상 종료) / crashed(비정상 종료) / none(없음) */
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

  // --- 내부 ---

  private async run(args: string[], timeout = DockerService.CMD_TIMEOUT_MS): Promise<{ output: string }> {
    try {
      const { stdout, stderr } = await exec('docker', args, { timeout, maxBuffer: 10 * 1024 * 1024 })
      // docker build/pull은 진행 로그를 stderr로 출력한다
      return { output: [stdout, stderr].filter(Boolean).join('\n') }
    } catch (err: any) {
      const stderr = (err.stderr || '').toString()
      throw new DockerError(stderr.trim().split('\n').pop() || err.message, stderr)
    }
  }
}
