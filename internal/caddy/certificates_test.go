package caddy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// writeCert는 Caddy 저장소와 같은 자리에 인증서를 만든다 (뒤에 중간 인증서처럼 하나 더 붙인다).
func writeCert(t *testing.T, root, issuer, folder, org string, names []string, notAfter time.Time) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: names[0], Organization: []string{org}}, // 스스로 서명했으니 발급자 = 주체
		DNSNames:  names,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, issuer, folder)
	os.MkdirAll(dir, 0o755)
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	os.WriteFile(filepath.Join(dir, folder+".crt"), append(block, block...), 0o644)
	// 키는 Caddy만 읽는다 — 읽으려 하면 실패하게 둔다
	os.WriteFile(filepath.Join(dir, folder+".key"), []byte("-----BEGIN EC PRIVATE KEY-----\nsecret\n"), 0o000)
}

func TestCertificateFilesReadsWhatCaddyStored(t *testing.T) {
	root := t.TempDir()
	later := time.Now().Add(60 * 24 * time.Hour)
	writeCert(t, root, "acme-v02.api.letsencrypt.org-directory", "blog.example.com", "Let's Encrypt", []string{"blog.example.com"}, later)
	writeCert(t, root, "acme-v02.api.letsencrypt.org-directory", "wildcard_.apps.example.com", "Let's Encrypt", []string{"*.apps.example.com"}, later)
	writeCert(t, root, "local", "site.localhost", "Caddy Local Authority", []string{"site.localhost"}, time.Now().Add(12*time.Hour))
	os.MkdirAll(filepath.Join(root, "local", "broken.localhost"), 0o755)
	os.WriteFile(filepath.Join(root, "local", "broken.localhost", "broken.localhost.crt"), []byte("not a certificate"), 0o644)
	os.WriteFile(filepath.Join(root, "stray-file"), []byte("x"), 0o644)

	certs, err := NewCertificateFiles(root).Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range certs {
		got = append(got, strings.Join(c.Names, ",")+" "+c.Issuer)
	}
	sort.Strings(got)
	want := "*.apps.example.com Let's Encrypt|blog.example.com Let's Encrypt|site.localhost Caddy Local Authority"
	if strings.Join(got, "|") != want {
		t.Fatalf("got %v", got)
	}
}

func TestCertificateFilesBeforeAnythingWasIssued(t *testing.T) {
	certs, err := NewCertificateFiles(filepath.Join(t.TempDir(), "nope")).Read(context.Background())
	if err != nil || len(certs) != 0 {
		t.Fatalf("no storage yet means no certificates, not an error: %v %v", certs, err)
	}
}
