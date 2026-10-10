package netcheck

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"

	"github.com/KangminNa/naru/internal/model"
)

func fixed(ips ...string) Resolver {
	return func(context.Context, string) ([]string, error) {
		if ips == nil {
			return nil, errors.New("no such host")
		}
		return ips, nil
	}
}

func TestPointsHere(t *testing.T) {
	ctx := context.Background()
	d, _ := model.ParseDomainName("naru.example.com")
	if r := NewDNS(fixed("198.51.100.24")).PointsHere(ctx, d, "198.51.100.24"); !r.Matches || !r.Resolves {
		t.Fatalf("%+v", r)
	}
	if r := NewDNS(fixed("203.0.113.9")).PointsHere(ctx, d, "198.51.100.24"); r.Matches || !r.Resolves || r.IPList() != "203.0.113.9" {
		t.Fatalf("pointing elsewhere is not a match: %+v", r)
	}
	if r := NewDNS(fixed()).PointsHere(ctx, d, "198.51.100.24"); r.Resolves || r.Matches {
		t.Fatalf("unresolvable: %+v", r)
	}
	if r := NewDNS(fixed("198.51.100.24")).PointsHere(ctx, d, ""); r.Matches {
		t.Fatal("without knowing our own IP we never claim a match")
	}
	if r := NewDNS(fixed("b", "a", "b")).PointsHere(ctx, d, ""); r.IPList() != "a, b" {
		t.Fatalf("IPs are sorted and unique: %q", r.IPList())
	}
}

func TestPortAnswers(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := (TCP{}).Answers(context.Background(), "127.0.0.1", model.Port(port)); err != nil {
		t.Fatal(err)
	}
	ln.Close()
	if err := (TCP{}).Answers(context.Background(), "127.0.0.1", model.Port(port)); err == nil {
		t.Fatal("a closed port does not answer: " + strconv.Itoa(port))
	}
}
