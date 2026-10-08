package web

import (
	"net"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/KangminNa/naru/internal/deploy"
	"github.com/KangminNa/naru/internal/dnscheck"
	"github.com/KangminNa/naru/internal/service"
	"github.com/KangminNa/naru/internal/source"
)

// serviceForm은 서비스 추가·설정 폼에서 받은 값이다. 틀리면 Err에 문구 키가 들어간다.
type serviceForm struct {
	Kind       string
	Name       string
	Source     string
	Branch     string
	BuildPath  string
	Folder     string
	Upstream   string
	Port       int
	Domain     string
	Token      string
	ClearToken bool
	Env        string
	Volumes    string
	AutoDeploy bool
}

var (
	namePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)
	imagePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:@-]{0,254}$`)
	hostPattern  = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9.-]{0,251}[a-zA-Z0-9])?$`)
	nonName      = regexp.MustCompile(`[^a-z0-9-]+`)
)

func readServiceForm(r *http.Request) serviceForm {
	port, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("port")))
	return serviceForm{
		Kind:       r.FormValue("kind"),
		Name:       strings.ToLower(strings.TrimSpace(r.FormValue("name"))),
		Source:     strings.TrimSpace(r.FormValue("source")),
		Branch:     strings.TrimSpace(r.FormValue("branch")),
		BuildPath:  strings.TrimSpace(r.FormValue("build_path")),
		Folder:     strings.TrimSpace(r.FormValue("folder")),
		Upstream:   strings.TrimSpace(r.FormValue("upstream")),
		Port:       port,
		Domain:     dnscheck.Normalize(r.FormValue("domain")),
		Token:      strings.TrimSpace(r.FormValue("token")),
		ClearToken: r.FormValue("clear_token") == "1",
		Env:        strings.ReplaceAll(r.FormValue("env"), "\r\n", "\n"),
		Volumes:    strings.ReplaceAll(r.FormValue("volumes"), "\r\n", "\n"),
		AutoDeploy: r.FormValue("auto_deploy") == "1",
	}
}

// check는 종류에 맞는 값만 검사하고 정리한다. 문제가 있으면 문구 키를 돌려준다.
func (f *serviceForm) check() string {
	kind := service.Kind(f.Kind)
	if !kind.Valid() {
		return "err.kind"
	}
	if f.Branch == "" {
		f.Branch = "main"
	}
	switch kind {
	case service.KindRepo, service.KindStatic:
		if source.ValidRepoURL(f.Source) != nil {
			return "err.repo"
		}
		if source.ValidBranch(f.Branch) != nil {
			return "err.branch"
		}
	case service.KindImage:
		if !imagePattern.MatchString(f.Source) || strings.Contains(f.Source, "..") {
			return "err.image"
		}
	case service.KindExternal:
		up, ok := normalizeUpstream(f.Upstream)
		if !ok {
			return "err.upstream"
		}
		f.Upstream = up
	}
	var err error
	if f.BuildPath, err = source.CleanRel(f.BuildPath); err != nil {
		return "err.path"
	}
	if f.Folder, err = source.CleanRel(f.Folder); err != nil {
		return "err.path"
	}
	if f.Port < 0 || f.Port > 65535 {
		return "err.port"
	}
	for _, line := range strings.Split(f.Env, "\n") {
		if t := strings.TrimSpace(line); t != "" && !strings.HasPrefix(t, "#") && !deploy.ValidEnvLine(line) {
			return "err.env"
		}
	}
	for _, line := range strings.Split(f.Volumes, "\n") {
		if t := strings.TrimSpace(line); t != "" && !deploy.ValidVolume(t) {
			return "err.volumes"
		}
	}
	if f.Domain != "" && !dnscheck.Valid(f.Domain) {
		return "err.domain"
	}
	return ""
}

// normalizeUpstream은 "192.168.0.20:5000", "https://router.lan:443", "localhost:9000" 같은 값을 받는다.
// 이 서버 자신(localhost)은 웹서버 컨테이너 안에서 host.docker.internal이다.
func normalizeUpstream(raw string) (string, bool) {
	scheme := ""
	if rest, ok := strings.CutPrefix(raw, "https://"); ok {
		scheme, raw = "https://", rest
	} else {
		raw = strings.TrimPrefix(raw, "http://")
	}
	raw = strings.TrimSuffix(raw, "/")
	host, portStr, err := net.SplitHostPort(raw)
	if err != nil {
		return "", false
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return "", false
	}
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		host = "host.docker.internal"
	}
	if net.ParseIP(host) == nil && !hostPattern.MatchString(host) {
		return "", false
	}
	return scheme + net.JoinHostPort(host, portStr), true
}

// suggestName은 이름을 비워 두면 주소·저장소·이미지에서 이름을 지어 준다.
func suggestName(f serviceForm) string {
	var base string
	switch {
	case f.Domain != "":
		base, _, _ = strings.Cut(f.Domain, ".")
	case f.Source != "":
		s := strings.TrimSuffix(strings.TrimSuffix(f.Source, "/"), ".git")
		s, _, _ = strings.Cut(s, "@")
		base = path.Base(s)
		if i := strings.LastIndex(base, ":"); i > 0 && f.Kind == string(service.KindImage) {
			base = base[:i]
		}
	case f.Upstream != "":
		host, _, _ := net.SplitHostPort(strings.TrimPrefix(f.Upstream, "https://"))
		base, _, _ = strings.Cut(host, ".")
	}
	name := strings.Trim(nonName.ReplaceAllString(strings.ToLower(base), "-"), "-")
	if len(name) > 31 {
		name = strings.Trim(name[:31], "-")
	}
	if name == "" {
		name = "service"
	}
	return name
}
