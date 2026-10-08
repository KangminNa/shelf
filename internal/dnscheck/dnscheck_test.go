package dnscheck

import (
	"context"
	"errors"
	"testing"
)

func fixed(ips ...string) Resolver {
	return func(context.Context, string) ([]string, error) {
		if ips == nil {
			return nil, errors.New("no such host")
		}
		return ips, nil
	}
}

func TestCheck(t *testing.T) {
	ctx := context.Background()
	if r := Check(ctx, fixed("198.51.100.24"), "naru.example.com", "198.51.100.24"); !r.Matches || !r.Resolves {
		t.Fatalf("%+v", r)
	}
	if r := Check(ctx, fixed("203.0.113.9"), "naru.example.com", "198.51.100.24"); r.Matches || !r.Resolves || r.IPList() != "203.0.113.9" {
		t.Fatalf("pointing elsewhere is not a match: %+v", r)
	}
	if r := Check(ctx, fixed(), "naru.example.com", "198.51.100.24"); r.Resolves || r.Matches {
		t.Fatalf("unresolvable: %+v", r)
	}
	if r := Check(ctx, fixed("198.51.100.24"), "naru.example.com", ""); r.Matches {
		t.Fatal("without knowing our own IP we never claim a match")
	}
	if r := Check(ctx, fixed("b", "a", "b"), "x.example.com", ""); r.IPList() != "a, b" {
		t.Fatalf("IPs are sorted and unique: %q", r.IPList())
	}
}

func TestNormalizeAndValid(t *testing.T) {
	cases := map[string]string{
		" HTTPS://Naru.Example.com/admin ": "naru.example.com",
		"naru.example.com.":                "naru.example.com",
		"http://a.b.co":                    "a.b.co",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
	for _, ok := range []string{"naru.example.com", "a.co", "my-app.nakangmin.duckdns.org"} {
		if !Valid(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "localhost", "198.51.100.24", "-a.example.com", "a_b.example.com", "naru.example.com:8080", "a..b.com"} {
		if Valid(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}
