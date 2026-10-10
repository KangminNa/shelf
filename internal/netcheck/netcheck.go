// Package netcheck는 네트워크로 확인한다 — 포트가 응답하는지, 도메인이 이 서버를 가리키는지.
package netcheck

import (
	"context"
	"net"
	"slices"
	"strconv"
	"time"

	"github.com/KangminNa/naru/internal/model"
)

// TCP는 그 주소의 포트에 연결이 되는지 본다.
type TCP struct{}

func (TCP) Answers(ctx context.Context, host string, port model.Port) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(int(port))))
	if err == nil {
		c.Close()
	}
	return err
}

// Resolver는 이름을 IP 목록으로 바꾼다.
type Resolver func(ctx context.Context, host string) ([]string, error)

// DNS는 도메인이 이 서버를 가리키는지 본다.
type DNS struct{ lookup Resolver }

// NewDNS는 lookup이 nil이면 운영체제의 DNS를 쓴다.
func NewDNS(lookup Resolver) DNS {
	if lookup == nil {
		lookup = net.DefaultResolver.LookupHost
	}
	return DNS{lookup: lookup}
}

func (d DNS) PointsHere(ctx context.Context, domain model.DomainName, thisServer string) model.DNSAnswer {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	a := model.DNSAnswer{Domain: domain.String(), Expected: thisServer}
	ips, err := d.lookup(ctx, domain.String())
	if err != nil || len(ips) == 0 {
		return a
	}
	slices.Sort(ips)
	a.IPs = slices.Compact(ips)
	a.Resolves = true
	a.Matches = thisServer != "" && slices.Contains(a.IPs, thisServer)
	return a
}
