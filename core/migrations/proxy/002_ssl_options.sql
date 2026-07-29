-- NPM 수준 SSL 옵션
ALTER TABLE proxy_hosts ADD COLUMN hsts_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE proxy_hosts ADD COLUMN hsts_subdomains INTEGER NOT NULL DEFAULT 0;

-- SAN(여러 도메인)/와일드카드 인증서 + DNS-01 갱신용 자격증명
ALTER TABLE ssl_certs ADD COLUMN domains TEXT DEFAULT '';       -- 줄 단위 도메인 목록 (비면 domain 단일)
ALTER TABLE ssl_certs ADD COLUMN dns_provider TEXT DEFAULT '';  -- '' | cloudflare
ALTER TABLE ssl_certs ADD COLUMN dns_token TEXT DEFAULT '';     -- API 응답에 노출 금지
