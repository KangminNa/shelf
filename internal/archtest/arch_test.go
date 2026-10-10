// Package archtest는 docs/design/v2-objects.md §10의 규칙(R1~R11)을 소스로 확인한다.
// 규칙을 어기면 go test가 실패한다 — 설계 문서와 코드가 어긋나지 않게.
package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/KangminNa/naru/internal/"

// 패키지의 자리. 자리마다 할 수 있는 일이 다르다.
var (
	core      = set("model", "contract")
	tools     = set("docker", "git", "files", "caddy", "netcheck", "stats", "events", "system")
	storage   = set("store")
	behaviors = set("access", "settings", "services", "kinds", "deploy", "webhook", "webserver", "views")
	entries   = set("web", "cli")
	root      = set("app")
)

func set(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

type file struct {
	pkg  string // internal 아래 패키지 이름
	path string
	ast  *ast.File
}

var parsed []file

func sources(t *testing.T) []file {
	t.Helper()
	if parsed != nil {
		return parsed
	}
	fset := token.NewFileSet()
	dirs, err := os.ReadDir("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dirs {
		if !d.IsDir() || d.Name() == "archtest" {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join("..", d.Name(), "*.go"))
		for _, m := range matches {
			if strings.HasSuffix(m, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, m, nil, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			parsed = append(parsed, file{pkg: d.Name(), path: m, ast: f})
		}
	}
	return parsed
}

func known(t *testing.T) {
	t.Helper()
	for _, f := range sources(t) {
		if !core[f.pkg] && !tools[f.pkg] && !storage[f.pkg] && !behaviors[f.pkg] && !entries[f.pkg] && !root[f.pkg] {
			t.Errorf("R0: package %q has no place in the design — add it to §4 and to archtest", f.pkg)
		}
	}
}

func TestEveryPackageHasAPlace(t *testing.T) { known(t) }

func imports(f file) []string {
	var out []string
	for _, im := range f.ast.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		out = append(out, p)
	}
	return out
}

// R1: app을 뺀 내부 패키지는 model·contract만 import한다.
func TestR1OnlyModelAndContract(t *testing.T) {
	for _, f := range sources(t) {
		if root[f.pkg] {
			continue
		}
		for _, p := range imports(f) {
			inner, ok := strings.CutPrefix(p, module)
			if !ok {
				continue
			}
			if inner != "model" && inner != "contract" {
				t.Errorf("R1: %s imports %s — only model and contract", f.path, p)
			}
			if f.pkg == "model" || (f.pkg == "contract" && inner != "model") {
				t.Errorf("R1/R3: %s imports %s", f.path, p)
			}
		}
	}
}

// R2: 다른 객체를 담는 필드의 타입은 contract의 인터페이스다 (또는 인터페이스만 담은 contract의 묶음 — KindTools).
func TestR2RelationsAreInterfaces(t *testing.T) {
	ifaces := contractInterfaces(t)
	bundles := contractBundles(t)
	for _, f := range sources(t) {
		ast.Inspect(f.ast, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range st.Fields.List {
				sel := selectorOf(field.Type)
				if sel == nil {
					continue
				}
				pkg := sel.X.(*ast.Ident).Name
				if pkg == "contract" && !ifaces[sel.Sel.Name] && !bundles[sel.Sel.Name] {
					t.Errorf("R2: %s holds contract.%s, which is not an interface", f.path, sel.Sel.Name)
				}
			}
			if f.pkg == "contract" {
				for _, field := range st.Fields.List {
					id, ok := field.Type.(*ast.Ident)
					if !ok || !ifaces[id.Name] {
						t.Errorf("R2: contract struct fields must be interfaces (%s)", f.path)
					}
				}
			}
			return true
		})
	}
}

// selectorOf는 pkg.Type, *pkg.Type, []pkg.Type의 pkg.Type이다.
func selectorOf(e ast.Expr) *ast.SelectorExpr {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		if _, ok := x.X.(*ast.Ident); ok {
			return x
		}
	case *ast.StarExpr:
		return selectorOf(x.X)
	case *ast.ArrayType:
		return selectorOf(x.Elt)
	}
	return nil
}

func contractBundles(t *testing.T) map[string]bool {
	out := map[string]bool{}
	for _, f := range sources(t) {
		if f.pkg != "contract" {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			if ts, ok := n.(*ast.TypeSpec); ok {
				if _, ok := ts.Type.(*ast.StructType); ok {
					out[ts.Name.Name] = true
				}
			}
			return true
		})
	}
	return out
}

func contractInterfaces(t *testing.T) map[string]bool {
	out := map[string]bool{}
	for _, f := range sources(t) {
		if f.pkg != "contract" {
			continue
		}
		for _, d := range f.ast.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, s := range g.Specs {
				if ts, ok := s.(*ast.TypeSpec); ok {
					if _, ok := ts.Type.(*ast.InterfaceType); ok {
						out[ts.Name.Name] = true
					}
				}
			}
		}
	}
	return out
}

// R3: model은 표준 라이브러리만 쓰고, 인터페이스·함수 필드를 갖지 않는다 (데이터만).
func TestR3ModelIsData(t *testing.T) {
	for _, f := range sources(t) {
		if f.pkg != "model" {
			continue
		}
		for _, p := range imports(f) {
			if strings.Contains(p, ".") {
				t.Errorf("R3: model imports %s — standard library only", p)
			}
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			if st, ok := n.(*ast.StructType); ok {
				for _, field := range st.Fields.List {
					switch field.Type.(type) {
					case *ast.InterfaceType, *ast.FuncType:
						t.Errorf("R3: %s has an interface or func field — model holds data only", f.path)
					}
					if id, ok := field.Type.(*ast.Ident); ok && id.Name == "Event" {
						t.Errorf("R3: %s holds an Event interface", f.path)
					}
				}
			}
			return true
		})
	}
}

// R4: 인터페이스는 메서드 다섯 개 이하.
func TestR4SmallInterfaces(t *testing.T) {
	for _, f := range sources(t) {
		ast.Inspect(f.ast, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			if it, ok := ts.Type.(*ast.InterfaceType); ok && len(it.Methods.List) > 5 {
				t.Errorf("R4: %s.%s has %d methods — split it by what it does", f.pkg, ts.Name.Name, len(it.Methods.List))
			}
			return true
		})
	}
}

// R5: contract의 인터페이스와 model의 타입은 모두 설계 문서에 이름이 있다.
func TestR5EveryNameIsInTheDesign(t *testing.T) {
	doc, err := os.ReadFile("../../docs/design/v2-objects.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	for _, f := range sources(t) {
		if !core[f.pkg] {
			continue
		}
		for _, d := range f.ast.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.TYPE {
				continue
			}
			for _, s := range g.Specs {
				ts := s.(*ast.TypeSpec)
				if !ts.Name.IsExported() {
					continue
				}
				if !strings.Contains(text, "`"+ts.Name.Name+"`") {
					t.Errorf("R5: %s.%s is not described in docs/design/v2-objects.md", f.pkg, ts.Name.Name)
				}
			}
		}
	}
}

// R6: 뜻이 넓은 말은 타입 이름에 쓰지 않는다.
func TestR6NoVagueNames(t *testing.T) {
	banned := []string{"Handler", "Processor", "Helper", "Util", "Info", "Data", "Plan", "Ledger", "Registry", "Roles"}
	for _, f := range sources(t) {
		ast.Inspect(f.ast, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			name := ts.Name.Name
			for _, b := range banned {
				if strings.Contains(name, b) || strings.HasPrefix(name, strings.ToLower(b[:1])+b[1:]) {
					t.Errorf("R6: type %s.%s uses %q — name it by what it does", f.pkg, name, b)
				}
			}
			return true
		})
	}
}

// R7: SQL은 store에만.
func TestR7SQLOnlyInStore(t *testing.T) {
	keywords := []string{"SELECT ", "INSERT INTO", "UPDATE ", "DELETE FROM", "CREATE TABLE"}
	for _, f := range sources(t) {
		if storage[f.pkg] {
			continue
		}
		for _, p := range imports(f) {
			if p == "database/sql" || strings.Contains(p, "sqlite") {
				t.Errorf("R7: %s imports %s", f.path, p)
			}
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				for _, k := range keywords {
					if strings.Contains(lit.Value, k) {
						t.Errorf("R7: %s has SQL %s", f.path, lit.Value)
					}
				}
			}
			return true
		})
	}
}

// R8: 바깥 세계에 닿는 것(프로세스·소켓·파일 쓰기)은 도구·저장·조립에만.
func TestR8OutsideOnlyInTools(t *testing.T) {
	bannedImports := set("os/exec", "syscall", "archive/tar", "database/sql")
	bannedCalls := map[string]map[string]bool{
		"os":   set("WriteFile", "MkdirAll", "Mkdir", "RemoveAll", "Remove", "Create", "OpenFile", "Rename", "Symlink", "Chmod", "ReadFile", "ReadDir", "Open", "Stat"),
		"net":  set("Dial", "DialTimeout", "Dialer", "Listen", "ListenPacket", "DefaultResolver", "LookupHost", "LookupIP", "Resolver"),
		"http": set("Get", "Post", "Head", "DefaultClient", "Client", "NewRequest", "NewRequestWithContext", "Transport"),
	}
	for _, f := range sources(t) {
		if tools[f.pkg] || storage[f.pkg] || root[f.pkg] {
			continue
		}
		for _, p := range imports(f) {
			if bannedImports[p] {
				t.Errorf("R8: %s imports %s — reach the outside through a tool", f.path, p)
			}
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && bannedCalls[id.Name][sel.Sel.Name] {
				t.Errorf("R8: %s uses %s.%s — reach the outside through a tool", f.path, id.Name, sel.Sel.Name)
			}
			return true
		})
	}
}

// R9: 종류 이름으로 분기하는 코드는 kinds에만. (v1 옮기기는 v1의 칸을 종류로 바꾸는 곳이라 예외 — store/v1.go)
func TestR9KindsBranchOnlyInKinds(t *testing.T) {
	kindNames := set("KindRepo", "KindImage", "KindStatic", "KindExternal")
	for _, f := range sources(t) {
		if f.pkg == "kinds" || f.pkg == "model" || filepath.Base(f.path) == "v1.go" && f.pkg == "store" {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && kindNames[sel.Sel.Name] {
				t.Errorf("R9: %s names model.%s — ask KindLookup instead", f.path, sel.Sel.Name)
			}
			return true
		})
	}
}

// R10: 화면·셸 명령은 비밀(ServiceSecrets)과 비밀번호 해시(PasswordHash)를 쓰지 않는다 —
// 직접 쓰지도 않고, 그것을 주고받는 인터페이스를 갖지도 않는다.
func TestR10NoSecretsOnScreen(t *testing.T) {
	secret := set("ServiceSecrets", "PasswordHash")
	carries := map[string]bool{} // 비밀을 주고받는 contract 인터페이스
	for _, f := range sources(t) {
		if f.pkg != "contract" {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			ast.Inspect(ts.Type, func(m ast.Node) bool {
				if sel, ok := m.(*ast.SelectorExpr); ok && secret[sel.Sel.Name] {
					carries[ts.Name.Name] = true
				}
				return true
			})
			return true
		})
	}
	for _, f := range sources(t) {
		if !entries[f.pkg] {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok {
				if id.Name == "model" && secret[sel.Sel.Name] {
					t.Errorf("R10: %s uses model.%s", f.path, sel.Sel.Name)
				}
				if id.Name == "contract" && carries[sel.Sel.Name] {
					t.Errorf("R10: %s holds contract.%s, which hands out secrets", f.path, sel.Sel.Name)
				}
			}
			return true
		})
	}
}

// R11: 검사용 정규식은 model에만 — 값의 모양은 값 객체가 지킨다.
func TestR11RegexpOnlyInModel(t *testing.T) {
	for _, f := range sources(t) {
		if f.pkg == "model" {
			continue
		}
		for _, p := range imports(f) {
			if p == "regexp" {
				t.Errorf("R11: %s imports regexp — put the shape check in a model value", f.path)
			}
		}
	}
}

// 문서의 표(§7)와 조립이 어긋나지 않았는지 사람이 볼 수 있게, 패키지별 import를 남긴다 (-v로 본다).
func TestPrintTheGraph(t *testing.T) {
	graph := map[string]map[string]bool{}
	for _, f := range sources(t) {
		for _, p := range imports(f) {
			if inner, ok := strings.CutPrefix(p, module); ok {
				if graph[f.pkg] == nil {
					graph[f.pkg] = map[string]bool{}
				}
				graph[f.pkg][inner] = true
			}
		}
	}
	var names []string
	for n := range graph {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		var deps []string
		for d := range graph[n] {
			deps = append(deps, d)
		}
		sort.Strings(deps)
		t.Logf("%-10s → %s", n, strings.Join(deps, ", "))
	}
}
