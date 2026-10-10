package kinds

import "github.com/KangminNa/naru/internal/model"

// 모양 검사는 값 객체가 이미 했다. 여기서는 이 종류에 필요한 칸이 있는지 보고, 쓰지 않는 칸을 비운다.

// RepoInput은 저장소 서비스의 입력을 본다 — 저장소 주소가 있어야 한다.
type RepoInput struct{}

func (RepoInput) Check(in model.ServiceInput) (model.ServiceInput, error) {
	if in.Repo.IsZero() {
		return in, model.InputError{Field: "source", Code: "repo"}
	}
	if in.Branch.IsZero() {
		in.Branch = model.DefaultBranch()
	}
	in.Image, in.External, in.Folder = model.ImageRef{}, model.ExternalAddress{}, model.FolderPath{}
	return in, nil
}

// ImageInput은 이미지 서비스의 입력을 본다 — 이미지 이름이 있어야 한다.
type ImageInput struct{}

func (ImageInput) Check(in model.ServiceInput) (model.ServiceInput, error) {
	if in.Image.IsZero() {
		return in, model.InputError{Field: "source", Code: "image"}
	}
	in.Repo, in.Branch, in.External = model.RepoURL{}, model.Branch{}, model.ExternalAddress{}
	in.Folder, in.BuildPath = model.FolderPath{}, model.FolderPath{}
	return in, nil
}

// StaticInput은 정적 사이트의 입력을 본다 — 저장소 주소가 있어야 한다.
type StaticInput struct{}

func (StaticInput) Check(in model.ServiceInput) (model.ServiceInput, error) {
	if in.Repo.IsZero() {
		return in, model.InputError{Field: "source", Code: "repo"}
	}
	if in.Branch.IsZero() {
		in.Branch = model.DefaultBranch()
	}
	in.Image, in.External, in.BuildPath, in.Port = model.ImageRef{}, model.ExternalAddress{}, model.FolderPath{}, 0
	in.Env, in.Volumes = model.EnvVars{}, model.Volumes{}
	return in, nil
}

// ExternalInput은 외부 연결의 입력을 본다 — 연결할 주소가 있어야 한다.
type ExternalInput struct{}

func (ExternalInput) Check(in model.ServiceInput) (model.ServiceInput, error) {
	if in.External.IsZero() {
		return in, model.InputError{Field: "upstream", Code: "upstream"}
	}
	in.Repo, in.Branch, in.Image = model.RepoURL{}, model.Branch{}, model.ImageRef{}
	in.Folder, in.BuildPath, in.Port, in.AutoDeploy = model.FolderPath{}, model.FolderPath{}, 0, false
	in.Env, in.Volumes = model.EnvVars{}, model.Volumes{}
	return in, nil
}
