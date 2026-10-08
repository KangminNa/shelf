// Package dnscheck는 도메인이 이 서버를 가리키는지 확인한다.
// HTTPS 인증서를 받기 전에 먼저 확인해야 발급 실패와 발급 한도 소진을 막을 수 있다.
package dnscheck

import (
	"context"
	"net"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Resolver는 이름을 IP 목록으로 바꾼다. 테스트에서 바꿔 끼운다.
type Resolver func(ctx context.Context, host string) ([]string, error)

// System은 운영체제의 DNS를 쓴다.
func System(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

type Result struct {
	Domain   string
	IPs      []string
	Resolves bool
	Expected string // 이 서버의 IP. 모르면 비어 있다
	Matches  bool
}

// IPList는 화면에 보여줄 IP 목록이다.
func (r Result) IPList() string { return strings.Join(r.IPs, ", ") }

// Check는 domain을 찾아 expected(이 서버의 IP)와 견준다. expected를 모르면 Matches는 거짓이다.
func Check(ctx context.Context, lookup Resolver, domain, expected string) Result {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r := Result{Domain: domain, Expected: expected}
	ips, err := lookup(ctx, domain)
	if err != nil || len(ips) == 0 {
		return r
	}
	slices.Sort(ips)
	r.IPs = slices.Compact(ips)
	r.Resolves = true
	r.Matches = expected != "" && slices.Contains(r.IPs, expected)
	return r
}

var label = `[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?`
var domainPattern = regexp.MustCompile(`^(` + label + `\.)+[a-z]{2,63}$`)

// Normalize는 사람이 흔히 붙여 넣는 모양(https://, 끝의 / 나 .)을 걷어낸다.
func Normalize(input string) string {
	d := strings.ToLower(strings.TrimSpace(input))
	d = strings.TrimPrefix(d, "https://")
	d = strings.TrimPrefix(d, "http://")
	d, _, _ = strings.Cut(d, "/")
	return strings.TrimSuffix(d, ".")
}

// Valid는 공개 DNS 이름 모양인가. IP와 localhost, 포트는 받지 않는다.
func Valid(domain string) bool {
	return len(domain) <= 253 && domainPattern.MatchString(domain)
}
