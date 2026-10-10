package web

// 화면 흐름 전체는 app의 인수 테스트가 본다. 여기서는 템플릿과 문구만 본다.

import (
	"regexp"
	"strings"
	"testing"
)

// 문구 키는 두 언어에 모두 있어야 하고, 템플릿이 쓰는 키는 모두 있어야 한다.
func TestMessagesAreComplete(t *testing.T) {
	for key := range messages["ko"] {
		if _, ok := messages["en"][key]; !ok {
			t.Errorf("en is missing %q", key)
		}
	}
	for key := range messages["en"] {
		if _, ok := messages["ko"][key]; !ok {
			t.Errorf("ko is missing %q", key)
		}
	}
	used := regexp.MustCompile(`\{\{-?\s*t "([a-z0-9.]+)"`)
	entries, _ := templateFS.ReadDir("templates")
	for _, e := range entries {
		body, _ := templateFS.ReadFile("templates/" + e.Name())
		for _, m := range used.FindAllStringSubmatch(string(body), -1) {
			if _, ok := messages["ko"][m[1]]; !ok {
				t.Errorf("%s uses unknown key %q", e.Name(), m[1])
			}
		}
	}
	for _, key := range okKeys {
		if _, ok := messages["ko"][key]; !ok {
			t.Errorf("flash key %q has no message", key)
		}
	}
	for _, key := range errKeys {
		if _, ok := messages["ko"][key]; !ok {
			t.Errorf("error key %q has no message", key)
		}
	}
}

// 값 객체·종류 담당자가 내는 InputError 코드는 모두 문구가 있어야 한다.
func TestInputErrorCodesHaveMessages(t *testing.T) {
	for _, code := range []string{"name", "nametaken", "domain", "domaintaken", "admindomain", "repo", "branch", "image",
		"upstream", "path", "port", "env", "volumes", "kind", "email", "username"} {
		if _, ok := messages["ko"]["err."+code]; !ok {
			t.Errorf("err.%s has no message", code)
		}
	}
}

// CSP가 인라인 style을 막으므로 템플릿에 style 속성이 있으면 화면이 조용히 깨진다.
func TestTemplatesHaveNoInlineStyles(t *testing.T) {
	entries, _ := templateFS.ReadDir("templates")
	for _, e := range entries {
		body, _ := templateFS.ReadFile("templates/" + e.Name())
		if strings.Contains(string(body), "style=") {
			t.Errorf("%s has an inline style attribute — CSP blocks it; use a class", e.Name())
		}
	}
}
