# Shelf — self-hosted app platform
# 컨테이너 안에서 호스트의 Docker 데몬을 사용한다 (docker.sock 마운트 필요)
FROM node:20-alpine

# docker CLI(빌드/실행) + git(저장소 클론) + openssl(자체서명 인증서)
RUN apk add --no-cache docker-cli git openssl

WORKDIR /app

COPY package*.json ./
COPY core/package*.json ./core/
RUN npm install --omit=dev && npm install tsx

COPY tsconfig.base.json ./
COPY core ./core

# 81: admin UI · 80/443: reverse proxy · 9100: CI/CD webhooks
EXPOSE 81 80 443 9100

ENV NODE_ENV=production PORT=81

# 관리 UI의 /health로 생존 확인 (compose가 unhealthy 시 재시작 판단에 사용)
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:81/health || exit 1

CMD ["npx", "tsx", "core/src/index.ts"]
