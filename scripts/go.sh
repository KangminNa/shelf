#!/bin/sh
# 로컬에 Go를 설치하지 않고 Docker 안의 Go로 빌드·테스트한다.
#   scripts/go.sh test ./...
#   scripts/go.sh vet ./...
# 모듈과 빌드 캐시는 이름 있는 볼륨에 남겨 두 번째부터 빠르게 돈다.
set -e
cd "$(dirname "$0")/.."
exec docker run --rm \
  -v "$PWD":/src -w /src \
  -v naru-gomod:/go/pkg/mod \
  -v naru-gobuild:/root/.cache/go-build \
  -e CGO_ENABLED=0 -e GOOS -e GOARCH \
  golang:1.27-alpine go "$@"
