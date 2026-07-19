const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')

const code = (s: string) => `<pre style="font-family:var(--font-mono); font-size:12.5px; line-height:1.6; background:var(--bg-tertiary); padding:14px 16px; border-radius:var(--radius); overflow-x:auto; margin:0;">${esc(s)}</pre>`

const EXAMPLE_STRUCTURE = `my-app/
├── Dockerfile          # 필수 — 이게 앱의 계약 전부다
└── (나머지는 완전 자유: 언어, 프레임워크, DB 뭐든)`

const EXAMPLE_DOCKERFILE = `FROM node:20-alpine
WORKDIR /app
COPY package*.json ./
RUN npm install --omit=dev
COPY . .
EXPOSE 3000
CMD ["node", "server.js"]`

const EXAMPLE_FLOW = `git push
  → GitHub webhook → Shelf(:9100)
  → git pull → docker build → 컨테이너 재생성 (shelf-{app})
  → 도메인 설정 시 프록시 자동 연결 (80/443, SSL)`

export function appGuidePage(): string {
  return `
    <div style="max-width:820px;">
      <div class="shelf-section-header">
        <h2 class="shelf-section-title">앱 배포 가이드</h2>
        <a href="/admin/deploy" class="shelf-btn shelf-btn-ghost shelf-btn-sm">&larr; Apps</a>
      </div>

      <div class="shelf-card" style="margin-bottom:16px;">
        <p style="font-size:14px; line-height:1.7; color:var(--text-secondary);">
          Shelf의 앱은 <b style="color:var(--text);">Docker 컨테이너</b>입니다.
          계약은 단 하나 — <b style="color:var(--text);">저장소 루트에 Dockerfile이 있고, 컨테이너가 포트 하나로 HTTP를 서빙한다</b>.
          언어·프레임워크·DB는 완전히 자유입니다 (컨테이너 안에서 SQLite, Postgres, Redis 뭐든).
          빌드·실행·재시작·도메인·SSL·CI/CD는 Shelf가 처리합니다.
        </p>
      </div>

      <div class="shelf-card" style="margin-bottom:16px;">
        <div style="font-size:14px; font-weight:600; margin-bottom:10px;">1. 저장소 구조</div>
        ${code(EXAMPLE_STRUCTURE)}
      </div>

      <div class="shelf-card" style="margin-bottom:16px;">
        <div style="font-size:14px; font-weight:600; margin-bottom:10px;">2. Dockerfile 예시 (Node.js)</div>
        ${code(EXAMPLE_DOCKERFILE)}
        <p style="font-size:13px; color:var(--text-muted); margin-top:10px;">
          앱 등록 시 <b>Container port</b>에 EXPOSE 포트(여기선 3000)를, <b>Host port</b>에 서버에서 노출할 포트를 넣으면 됩니다.
        </p>
      </div>

      <div class="shelf-card" style="margin-bottom:16px;">
        <div style="font-size:14px; font-weight:600; margin-bottom:10px;">3. 배포 방식 2가지</div>
        <ul style="font-size:13px; line-height:1.9; color:var(--text-secondary); padding-left:18px;">
          <li><b style="color:var(--text);">Git repository</b> — 저장소 URL 등록 → Shelf가 clone & <code>docker build</code> → 실행. webhook을 등록하면 push마다 자동 재배포 (CI/CD)</li>
          <li><b style="color:var(--text);">Docker image</b> — <code>nginx:alpine</code>, <code>ghcr.io/...</code> 등 기존 이미지를 그대로 실행. Docker Hub의 어떤 앱이든 설치 가능</li>
        </ul>
      </div>

      <div class="shelf-card" style="margin-bottom:16px;">
        <div style="font-size:14px; font-weight:600; margin-bottom:10px;">4. CI/CD 파이프라인</div>
        ${code(EXAMPLE_FLOW)}
        <p style="font-size:13px; color:var(--text-muted); margin-top:10px;">
          webhook URL과 secret은 앱 상세 페이지에 표시됩니다. GitHub 저장소 Settings &rarr; Webhooks에 등록하세요.
        </p>
      </div>

      <div class="shelf-card">
        <div style="font-size:14px; font-weight:600; margin-bottom:10px;">5. 데이터 & 설정</div>
        <ul style="font-size:13px; line-height:1.9; color:var(--text-secondary); padding-left:18px;">
          <li><b style="color:var(--text);">환경변수</b> — 앱 등록 시 KEY=VALUE로 입력, 컨테이너에 주입됨</li>
          <li><b style="color:var(--text);">볼륨</b> — <code>host:container</code> 형식. DB 파일 등 영속 데이터는 볼륨으로 보존</li>
          <li><b style="color:var(--text);">도메인</b> — 입력하면 <a href="/admin/proxy" style="color:var(--accent);">프록시</a>에 자동 등록, SSL은 프록시에서 원클릭 발급</li>
          <li><b style="color:var(--text);">재시작 정책</b> — <code>unless-stopped</code>: 서버 재부팅에도 컨테이너 자동 복구</li>
        </ul>
      </div>
    </div>`
}
