package model

import (
	"encoding/json"
	"net/netip"
	"reflect"
	"testing"
)

func TestPathPrefixes(t *testing.T) {
	for in, want := range map[string]string{"/api": "/api", "/api/": "/api", " /v1/users ": "/v1/users", "/.well-known/x": "/.well-known/x"} {
		if p, err := ParsePathPrefix(in); err != nil || p.String() != want {
			t.Errorf("%q → %q %v, want %q", in, p, err, want)
		}
	}
	for _, bad := range []string{"", "/", "api", "/api/*", "/a b", "/a/../b", "/..", "/a//b", "/a/./b"} {
		if _, err := ParsePathPrefix(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestHeaderLines(t *testing.T) {
	got, err := ParseHeaderLines("X-Frame-Options: DENY\n# comment\n\nServer:\nCache-Control: public, max-age=60\n")
	want := []HeaderRule{{"X-Frame-Options", "DENY"}, {"Server", ""}, {"Cache-Control", "public, max-age=60"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%v %v", got, err)
	}
	for _, bad := range []string{"no colon", "Bad Name: x", ": x"} {
		if _, err := ParseHeaderLines(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestIPLines(t *testing.T) {
	got, err := ParseIPLines("10.0.0.0/8\n192.168.1.5\n2001:db8::/32\n10.1.2.3/8")
	want := []string{"10.0.0.0/8", "192.168.1.5/32", "2001:db8::/32", "10.0.0.0/8"}
	if err != nil || len(got) != 4 {
		t.Fatalf("%v %v", got, err)
	}
	for i, p := range got {
		if p.String() != want[i] {
			t.Errorf("%d: %s, want %s", i, p, want[i])
		}
	}
	if _, err := ParseIPLines("not-an-ip"); err == nil {
		t.Fatal("refused")
	}
}

func TestPathLines(t *testing.T) {
	got, err := ParsePathLines("/api api 떼기\n/admin 192.168.0.10:8080\n/old legacy strip")
	if err != nil || len(got) != 3 || got[0].Target != "api" || !got[0].StripPrefix || got[1].StripPrefix || got[1].Target != "192.168.0.10:8080" || !got[2].StripPrefix {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range []string{"/api", "/api api later", "api api", "/api a\n/api b"} {
		if _, err := ParsePathLines(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestWebSettingsSurviveStorage(t *testing.T) {
	prefix, _ := ParsePathPrefix("/api")
	ext, _ := ParseExternalAddress("localhost:9000")
	in := WebSettings{
		Headers:   []HeaderRule{{"X-A", "1"}},
		AllowFrom: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		Login:     BasicLogin{User: "me", Hash: "$2a$10$x"},
		Paths:     []PathRoute{{Prefix: prefix, Service: 3, StripPrefix: true}, {Prefix: prefix, External: ext}},
		Advanced:  "respond /health 200", Compiled: []byte(`[{"handle":[]}]`), Maintenance: true,
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out WebSettings
	if err := json.Unmarshal(raw, &out); err != nil || !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip:\n%+v\n%+v\n%v", in, out, err)
	}
	if json.Unmarshal([]byte(`{"Paths":[{"Prefix":"/../etc"}]}`), &out) == nil {
		t.Fatal("stored values are checked again when read")
	}
}
