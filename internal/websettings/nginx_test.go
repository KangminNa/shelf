package websettings

import (
	"strings"
	"testing"
)

// NPM의 "Advanced" 칸이나 손으로 쓰던 nginx server 블록 — 흔히 보는 모양.
const sample = `server {
    listen 80;
    server_name blog.example.com;   # 주소는 서비스에서 붙인다
    gzip on;
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header 'Cache-Control' 'public, max-age=60';
    allow 10.0.0.0/8;
    allow 192.168.1.5;
    deny all;
    auth_basic "Restricted";
    auth_basic_user_file /etc/nginx/.htpasswd;
    client_max_body_size 50m;

    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_set_header Host $host;
    }
    location /api/ {
        proxy_pass http://api:8080/;
    }
    location /static {
        proxy_pass http://files;
    }
    location /secure {
        proxy_pass https://vault.lan;
    }
    location ~ \.php$ {
        fastcgi_pass unix:/run/php.sock;
    }
    rewrite ^/old /new permanent;
}
`

func TestTranslateTypicalServerBlock(t *testing.T) {
	got := NginxReader{}.Translate(sample)
	in := got.Input

	if len(in.Headers) != 2 || in.Headers[0].Name != "X-Frame-Options" || in.Headers[0].Value != "SAMEORIGIN" || in.Headers[1].Value != "public, max-age=60" {
		t.Errorf("headers: %+v", in.Headers)
	}
	if len(in.AllowFrom) != 2 || in.AllowFrom[0].String() != "10.0.0.0/8" || in.AllowFrom[1].String() != "192.168.1.5/32" {
		t.Errorf("allow: %v", in.AllowFrom)
	}
	paths := map[string]string{}
	for _, p := range in.Paths {
		s := p.Target
		if p.StripPrefix {
			s += " 떼기"
		}
		paths[p.Prefix.String()] = s
	}
	want := map[string]string{"/api": "api:8080 떼기", "/static": "files:80", "/secure": "https://vault.lan:443"}
	for k, v := range want {
		if paths[k] != v {
			t.Errorf("path %s → %q, want %q (all: %v)", k, paths[k], v, paths)
		}
	}
	if len(paths) != 3 {
		t.Errorf("only prefix locations with proxy_pass become paths: %v", paths)
	}

	skipped := map[string]string{}
	for _, s := range got.Skipped {
		skipped[strings.Fields(s.Text)[0]] = s.Why
		if s.Line == 0 || s.Why == "" {
			t.Errorf("every skipped line says where and why: %+v", s)
		}
	}
	for _, d := range []string{"listen", "server_name", "gzip", "auth_basic", "auth_basic_user_file", "client_max_body_size", "location", "rewrite"} {
		if _, ok := skipped[d]; !ok {
			t.Errorf("%s should be listed as not carried over (skipped: %v)", d, skipped)
		}
	}
	if !strings.Contains(skipped["auth_basic_user_file"], "새로") {
		t.Errorf("passwords cannot be carried over — the user sets a new one: %q", skipped["auth_basic_user_file"])
	}
}

func TestTranslateBareDirectivesAndMaintenance(t *testing.T) {
	got := NginxReader{}.Translate("# NPM Advanced 칸\nadd_header X-Robots-Tag noindex;\nreturn 503;\n")
	if !got.Input.Maintenance || len(got.Input.Headers) != 1 || len(got.Skipped) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestAllowWithoutDenyAllDoesNotRestrict(t *testing.T) {
	got := NginxReader{}.Translate("allow 10.0.0.0/8;")
	if len(got.Input.AllowFrom) != 0 || len(got.Skipped) != 1 {
		t.Fatalf("in nginx, allow without deny all lets everyone in — nothing to carry over: %+v", got)
	}
}

func TestTranslateNeverPanicsOnBrokenText(t *testing.T) {
	for _, text := range []string{"", "server {", "}", "location /a { proxy_pass", `add_header "unterminated`, "{{{{", ";;;"} {
		NginxReader{}.Translate(text)
	}
}
