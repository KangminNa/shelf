package caddy

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"

	"github.com/KangminNa/naru/internal/model"
)

// CertificateFiles는 Caddy 저장소에서 받아 둔 인증서를 읽는다.
//
//	<root>/<발급자>/<이름>/<이름>.crt   예: acme-v02.api.letsencrypt.org-directory/blog.example.com/blog.example.com.crt
//	                                    local/site.test/site.test.crt · …/wildcard_.example.com/wildcard_.example.com.crt
//
// 공개 인증서(.crt)만 연다. 같은 폴더의 .key는 열지 않는다.
// Caddy 관리 API는 받은 인증서 목록을 주지 않아서 파일을 읽는다 — 저장소가 진짜다.
type CertificateFiles struct{ root string }

func NewCertificateFiles(root string) CertificateFiles { return CertificateFiles{root: root} }

func (f CertificateFiles) Read(ctx context.Context) (model.Certificates, error) {
	issuers, err := os.ReadDir(f.root)
	if os.IsNotExist(err) {
		return nil, nil // 아직 하나도 받지 않았다
	}
	if err != nil {
		return nil, err
	}
	var out model.Certificates
	for _, issuer := range issuers {
		if !issuer.IsDir() {
			continue
		}
		names, err := os.ReadDir(filepath.Join(f.root, issuer.Name()))
		if err != nil {
			continue
		}
		for _, name := range names {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			if !name.IsDir() {
				continue
			}
			path := filepath.Join(f.root, issuer.Name(), name.Name(), name.Name()+".crt")
			if c, ok := readCertificate(path); ok {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

// readCertificate는 PEM의 첫 인증서(받은 인증서 자신 — 뒤는 중간 인증서)를 읽는다. 깨졌으면 건너뛴다.
func readCertificate(path string) (model.Certificate, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return model.Certificate{}, false
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return model.Certificate{}, false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil || len(cert.DNSNames) == 0 {
		return model.Certificate{}, false
	}
	return model.Certificate{
		Names: cert.DNSNames, Issuer: issuerName(cert), NotBefore: cert.NotBefore, NotAfter: cert.NotAfter,
	}, true
}

// issuerName은 화면에 보일 발급자 이름이다 — "Let's Encrypt", "ZeroSSL", Caddy 내부 CA …
func issuerName(c *x509.Certificate) string {
	if len(c.Issuer.Organization) > 0 && strings.TrimSpace(c.Issuer.Organization[0]) != "" {
		return c.Issuer.Organization[0]
	}
	return c.Issuer.CommonName
}
