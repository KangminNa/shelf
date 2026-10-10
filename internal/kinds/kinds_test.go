package kinds

import (
	"errors"
	"testing"

	"github.com/KangminNa/naru/internal/model"
)

var lookup = NewLookup(Tools{SitesShown: "/srv/sites"})

func TestEveryKindHasItsTools(t *testing.T) {
	for _, k := range model.AllKinds() {
		tl, ok := lookup.Find(k)
		if !ok || tl.Input == nil || tl.Destination == nil || tl.Status == nil {
			t.Errorf("%s is incomplete", k)
		}
		if (tl.Builder == nil) != (tl.Swapper == nil) {
			t.Errorf("%s: a kind that builds also swaps", k)
		}
	}
	if tl, _ := lookup.Find(model.KindExternal); tl.Builder != nil {
		t.Fatal("external services have nothing to deploy")
	}
	if _, ok := lookup.Find("nonsense"); ok {
		t.Fatal("unknown kinds have no tools")
	}
}

func TestDestinations(t *testing.T) {
	name, _ := model.ParseServiceName("landing")
	cases := []struct {
		s    model.Service
		want model.Destination
	}{
		{model.Service{Kind: model.KindImage, Port: 8080, Live: model.LiveState{Alias: "naru-api"}}, model.Destination{Address: "naru-api:8080", Container: true}},
		{model.Service{Kind: model.KindRepo, Live: model.LiveState{Alias: "shelf-x"}}, model.Destination{Container: true}},
		{model.Service{Kind: model.KindExternal, External: "host.docker.internal:5000"}, model.Destination{Address: "host.docker.internal:5000"}},
		{model.Service{Kind: model.KindStatic, Name: name}, model.Destination{}},
		{model.Service{Kind: model.KindStatic, Name: name, Live: model.LiveState{Release: "12"}}, model.Destination{Folder: "/srv/sites/landing/12"}},
	}
	for _, c := range cases {
		tl, _ := lookup.Find(c.s.Kind)
		if got := tl.Destination.Find(c.s); got != c.want {
			t.Errorf("%+v → %+v, want %+v", c.s, got, c.want)
		}
	}
}

func TestStatuses(t *testing.T) {
	containers := model.ContainerStates{
		"naru-a-1": {State: "running", Status: "Up 3 hours"},
		"naru-b-1": {State: "exited"},
		"naru-c-1": {State: "restarting"},
	}
	img, _ := lookup.Find(model.KindImage)
	read := func(l model.LiveState, all model.ContainerStates) string {
		return img.Status.Read(model.Service{Live: l}, all).Key
	}
	cases := map[string]string{
		read(model.LiveState{Instance: "naru-a-1"}, containers):                "running",
		read(model.LiveState{Instance: "naru-b-1"}, containers):                "crashed",
		read(model.LiveState{Instance: "naru-b-1", Stopped: true}, containers): "stopped",
		read(model.LiveState{Instance: "naru-c-1"}, containers):                "restarting",
		read(model.LiveState{Alias: "shelf-gone"}, containers):                 "missing",
		read(model.LiveState{}, containers):                                    "notdeployed",
		read(model.LiveState{Instance: "naru-a-1"}, nil):                       "unknown",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	st, _ := lookup.Find(model.KindStatic)
	if st.Status.Read(model.Service{Live: model.LiveState{Release: "3"}}, nil).Key != "static" {
		t.Fatal("static")
	}
	ext, _ := lookup.Find(model.KindExternal)
	if ext.Status.Read(model.Service{}, nil).Key != "routed" {
		t.Fatal("routed")
	}
}

func TestInputs(t *testing.T) {
	repo, _ := model.ParseRepoURL("https://github.com/me/blog")
	img, _ := model.ParseImageRef("me/app")
	ext, _ := model.ParseExternalAddress("localhost:5000")

	r, _ := lookup.Find(model.KindRepo)
	in, err := r.Input.Check(model.ServiceInput{Repo: repo, Image: img})
	if err != nil || in.Branch.String() != "main" || !in.Image.IsZero() {
		t.Fatalf("repos default to main and drop what they do not use: %+v %v", in, err)
	}
	var ie model.InputError
	if _, err := r.Input.Check(model.ServiceInput{Image: img}); !errors.As(err, &ie) || ie.Code != "repo" {
		t.Fatalf("a repo needs a repository: %v", err)
	}
	i, _ := lookup.Find(model.KindImage)
	if _, err := i.Input.Check(model.ServiceInput{}); !errors.As(err, &ie) || ie.Code != "image" {
		t.Fatalf("an image needs an image: %v", err)
	}
	e, _ := lookup.Find(model.KindExternal)
	if _, err := e.Input.Check(model.ServiceInput{}); !errors.As(err, &ie) || ie.Code != "upstream" {
		t.Fatalf("an external service needs an address: %v", err)
	}
	if in, err := e.Input.Check(model.ServiceInput{External: ext, Port: 80, Repo: repo}); err != nil || in.Port != 0 || !in.Repo.IsZero() {
		t.Fatalf("%+v %v", in, err)
	}
}

func TestDestinationsByInstallMode(t *testing.T) {
	s := model.Service{Kind: model.KindImage, Port: 8080, Live: model.LiveState{Alias: "naru-api", Instance: "naru-api-3", InstanceIP: "172.18.0.5"}}
	if got := (ContainerAliasDestination{}).Find(s); got != (model.Destination{Address: "naru-api:8080", Container: true}) {
		t.Errorf("docker: by alias, got %+v", got)
	}
	if got := (ContainerIPDestination{}).Find(s); got != (model.Destination{Address: "172.18.0.5:8080", Container: true}) {
		t.Errorf("host: by IP, got %+v", got)
	}
	s.Live.InstanceIP = ""
	if got := (ContainerIPDestination{}).Find(s); got.Address != "" || !got.Container {
		t.Errorf("an unknown IP goes nowhere (the web server answers 502), got %+v", got)
	}

	host := ExternalDestination{ThisServer: "127.0.0.1"}
	cases := map[string]string{
		"host.docker.internal:5000":         "127.0.0.1:5000",
		"https://host.docker.internal:8443": "https://127.0.0.1:8443",
		"192.168.0.20:80":                   "192.168.0.20:80",
	}
	for in, want := range cases {
		if got := host.Find(model.Service{External: in}).Address; got != want {
			t.Errorf("host mode: %s → %s, want %s", in, got, want)
		}
		if got := (ExternalDestination{}).Find(model.Service{External: in}).Address; got != in {
			t.Errorf("docker mode keeps %s, got %s", in, got)
		}
	}

	l := NewLookup(Tools{ContainerDestination: ContainerIPDestination{}, ExternalDestination: host})
	repo, _ := l.Find(model.KindRepo)
	s.Live.InstanceIP = "172.18.0.7"
	if repo.Destination.Find(s).Address != "172.18.0.7:8080" {
		t.Fatal("the lookup uses the destination the composition root chose")
	}
	ext, _ := l.Find(model.KindExternal)
	if ext.Destination.Find(model.Service{External: "host.docker.internal:1"}).Address != "127.0.0.1:1" {
		t.Fatal("external too")
	}
}
