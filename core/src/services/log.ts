/**
 * 스코프가 붙는 구조화 로거.
 * scope('sub')로 하위 스코프 로거를 만들 수 있다.
 */
export class Logger {
  constructor(private readonly scopeName: string) {}

  info(message: string, data?: unknown): void {
    this.write('INFO', message, data)
  }

  warn(message: string, data?: unknown): void {
    this.write('WARN', message, data)
  }

  error(message: string, data?: unknown): void {
    this.write('ERROR', message, data)
  }

  debug(message: string, data?: unknown): void {
    if (process.env.NODE_ENV === 'development') {
      this.write('DEBUG', message, data)
    }
  }

  /** 하위 스코프 로거 생성 (예: proxy → proxy:ssl) */
  scope(sub: string): Logger {
    return new Logger(`${this.scopeName}:${sub}`)
  }

  private write(level: string, message: string, data?: unknown): void {
    const timestamp = new Date().toISOString().slice(0, 19).replace('T', ' ')
    const suffix = data !== undefined ? ` ${JSON.stringify(data)}` : ''
    console.log(`[${timestamp}] [${this.scopeName}] ${level} ${message}${suffix}`)
  }
}

