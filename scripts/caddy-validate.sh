#!/bin/sh
# 렌더러가 그린 골든 설정을 실제 Caddy가 받아들이는지 확인한다.
#   scripts/caddy-validate.sh
# 골든 파일은 internal/engine/testdata/ — `scripts/go.sh test ./internal/engine -update`로 다시 만든다.
set -e
cd "$(dirname "$0")/.."
status=0
for f in internal/engine/testdata/*.json; do
  name=$(basename "$f")
  if out=$(docker run --rm -v "$PWD/internal/engine/testdata:/t:ro" caddy:2 caddy validate --config "/t/$name" 2>&1); then
    echo "ok    $name"
  else
    echo "FAIL  $name"; echo "$out" | tail -5; status=1
  fi
done
exit $status
