package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/KangminNa/naru/internal/model"
)

// serviceForm은 서비스 추가·설정 폼에서 받은 글자 그대로다. 틀렸을 때 다시 그리는 데 쓴다.
type serviceForm struct {
	Kind       string
	Name       string
	Source     string
	Branch     string
	BuildPath  string
	Folder     string
	Upstream   string
	Port       string
	Domain     string
	Token      string
	ClearToken bool
	Env        string
	Volumes    string
	AutoDeploy bool
}

func readServiceForm(r *http.Request) serviceForm {
	return serviceForm{
		Kind:       r.FormValue("kind"),
		Name:       strings.TrimSpace(r.FormValue("name")),
		Source:     strings.TrimSpace(r.FormValue("source")),
		Branch:     strings.TrimSpace(r.FormValue("branch")),
		BuildPath:  strings.TrimSpace(r.FormValue("build_path")),
		Folder:     strings.TrimSpace(r.FormValue("folder")),
		Upstream:   strings.TrimSpace(r.FormValue("upstream")),
		Port:       strings.TrimSpace(r.FormValue("port")),
		Domain:     strings.TrimSpace(r.FormValue("domain")),
		Token:      strings.TrimSpace(r.FormValue("token")),
		ClearToken: r.FormValue("clear_token") == "1",
		Env:        strings.ReplaceAll(r.FormValue("env"), "\r\n", "\n"),
		Volumes:    strings.ReplaceAll(r.FormValue("volumes"), "\r\n", "\n"),
		AutoDeploy: r.FormValue("auto_deploy") == "1",
	}
}

// formFrom은 저장된 값으로 설정 폼을 채운다.
func formFrom(f model.ServiceForm) serviceForm {
	port := ""
	if f.Port != 0 {
		port = strconv.Itoa(int(f.Port))
	}
	return serviceForm{
		Source: f.Source, Branch: f.Branch, BuildPath: f.BuildPath, Folder: f.Folder, Upstream: f.External,
		Port: port, AutoDeploy: f.AutoDeploy, Env: f.EnvText, Volumes: f.Volumes,
	}
}

// input은 글자들을 값 객체로 바꾼다. 모양이 틀린 칸이 있으면 그 InputError를 돌려준다.
// 어느 칸이 이 종류에 필요한지는 화면이 아니라 종류 담당자(InputChecker)가 본다 —
// "source"는 저장소 주소로도, 이미지 이름으로도 읽어 두고 종류가 고른다.
func (f serviceForm) input(kind model.KindName) (model.ServiceInput, error) {
	in := model.ServiceInput{Kind: kind, Token: f.Token, ClearToken: f.ClearToken, AutoDeploy: f.AutoDeploy}
	var err error
	if f.Name != "" {
		if in.Name, err = model.ParseServiceName(f.Name); err != nil {
			return in, err
		}
	}
	// 둘 다 아니면 둘 다 비어 있게 된다 — 그러면 종류 담당자가 자기에게 필요한 칸(저장소 주소·이미지 이름)이 틀렸다고 알려준다.
	in.Repo, _ = model.ParseRepoURL(f.Source)
	in.Image, _ = model.ParseImageRef(f.Source)
	if in.Branch, err = model.ParseBranch(f.Branch); err != nil {
		return in, err
	}
	if f.Upstream != "" {
		if in.External, err = model.ParseExternalAddress(f.Upstream); err != nil {
			return in, model.InputError{Field: "upstream", Code: "upstream"}
		}
	}
	if in.BuildPath, err = model.ParseFolderPath(f.BuildPath); err != nil {
		return in, err
	}
	if in.Folder, err = model.ParseFolderPath(f.Folder); err != nil {
		return in, err
	}
	if in.Port, err = model.ParsePort(f.Port); err != nil {
		return in, err
	}
	if in.Env, err = model.ParseEnvVars(f.Env); err != nil {
		return in, err
	}
	if in.Volumes, err = model.ParseVolumes(f.Volumes); err != nil {
		return in, err
	}
	if in.Domain, err = model.ParseOptionalDomainName(f.Domain); err != nil {
		return in, err
	}
	return in, nil
}

// errKeyOf는 실패를 화면 문구 키로 바꾼다. 모르는 실패는 "처리하지 못했어요".
func errKeyOf(err error) string {
	if ie, ok := model.AsInputError(err); ok {
		return "err." + ie.Code
	}
	return "err.internal"
}
