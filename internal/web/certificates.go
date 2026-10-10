package web

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/KangminNa/naru/internal/model"
)

// certRow는 주소 하나의 인증서 표시다.
type certRow struct {
	Ready  bool // 쓸 수 있는 인증서가 있다 — HTTP로 온 요청은 HTTPS로 넘어간다
	Soon   bool // 14일 안에 끝난다 (보통은 30일 전에 갱신되니, 갱신이 막혔다는 뜻)
	Until  time.Time
	Issuer string
	DNS    *model.DNSAnswer // 아직 없을 때만 — 발급이 안 되는 가장 흔한 이유가 DNS다
}

func certRowOf(st model.CertificateState, now time.Time) certRow {
	return certRow{Ready: st.Usable(now), Soon: st.EndsSoon(now), Until: st.NotAfter, Issuer: st.Issuer}
}

// domainRow는 서비스 상세의 주소 한 줄이다.
type domainRow struct {
	Domain model.Domain
	Cert   certRow
}

// domainRows는 주소마다 인증서 상태를 붙이고, 아직 인증서가 없는 HTTPS 주소는 DNS를 함께 본다 (한꺼번에).
func (s *Server) domainRows(r *http.Request, all []model.DomainView) []domainRow {
	now := s.d.Clock.Now()
	rows := make([]domainRow, len(all))
	var wg sync.WaitGroup
	for i, d := range all {
		rows[i] = domainRow{Domain: d.Domain, Cert: certRowOf(d.Certificate, now)}
		if d.Domain.HTTPS && !rows[i].Cert.Ready {
			wg.Add(1)
			go func(i int, name model.DomainName) {
				defer wg.Done()
				a := s.d.DNS.PointsHere(r.Context(), name, hostIP(r))
				rows[i].Cert.DNS = &a
			}(i, d.Domain.Domain)
		}
	}
	wg.Wait()
	return rows
}

// adminCert는 관리 주소의 인증서 표시다. 관리 주소가 없으면 nil.
func (s *Server) adminCert(ctx context.Context) *certRow {
	d, _ := s.d.AdminDomain.Get(ctx)
	if d.IsZero() {
		return nil
	}
	certs, _ := s.d.Certs.Read(ctx)
	now := s.d.Clock.Now()
	row := certRowOf(certs.For(d, now), now)
	return &row
}
