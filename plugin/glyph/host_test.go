package glyph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func quiet() *zap.SugaredLogger { return zap.NewNop().Sugar() }

// serve builds the host's mux the way the router does, so a test asks the same
// thing a browser does.
func serve(t *testing.T, module string) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	if err := New("crier", module, quiet()).RegisterHTTP(mux); err != nil {
		t.Fatalf("RegisterHTTP: %v", err)
	}
	return mux
}

func TestModuleIsServedAsJavaScript(t *testing.T) {
	dir := t.TempDir()
	module := filepath.Join(dir, "glyph-module.js")
	body := "export const glyphDef = {symbol: 'x'}\nexport const render = () => {}\n"
	if err := os.WriteFile(module, []byte(body), 0o600); err != nil {
		t.Fatalf("write module: %v", err)
	}

	rec := httptest.NewRecorder()
	serve(t, module).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ModuleRoute, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	// Without this exact media type the browser refuses the import, and the
	// canvas reports a missing glyph rather than a wrong header.
	if got := rec.Header().Get("Content-Type"); got != ModuleContentType {
		t.Errorf("Content-Type = %q, want %q", got, ModuleContentType)
	}
	if rec.Body.String() != body {
		t.Errorf("body = %q, want %q", rec.Body.String(), body)
	}
}

// The file is read when it is asked for. This is what makes replacing a glyph
// a matter of replacing a file, with no restart in it.
func TestReplacingTheFileReplacesWhatIsServed(t *testing.T) {
	dir := t.TempDir()
	module := filepath.Join(dir, "glyph-module.js")
	if err := os.WriteFile(module, []byte("first"), 0o600); err != nil {
		t.Fatalf("write module: %v", err)
	}
	mux := serve(t, module)

	first := httptest.NewRecorder()
	mux.ServeHTTP(first, httptest.NewRequest(http.MethodGet, ModuleRoute, nil))
	if first.Body.String() != "first" {
		t.Fatalf("first body = %q, want %q", first.Body.String(), "first")
	}

	if err := os.WriteFile(module, []byte("second"), 0o600); err != nil {
		t.Fatalf("rewrite module: %v", err)
	}

	second := httptest.NewRecorder()
	mux.ServeHTTP(second, httptest.NewRequest(http.MethodGet, ModuleRoute, nil))
	if second.Body.String() != "second" {
		t.Errorf("second body = %q, want %q", second.Body.String(), "second")
	}
}

func TestAbsentModuleIsRefusedAndNamed(t *testing.T) {
	module := filepath.Join(t.TempDir(), "nothing-here.js")

	rec := httptest.NewRecorder()
	serve(t, module).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ModuleRoute, nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	// The path is in the answer because the canvas only ever says the import
	// failed, and the file it could not read is the whole question.
	if !strings.Contains(rec.Body.String(), module) {
		t.Errorf("body %q does not name the module %q", rec.Body.String(), module)
	}
}

func TestDirectoryIsNotAModule(t *testing.T) {
	dir := t.TempDir()

	rec := httptest.NewRecorder()
	serve(t, dir).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ModuleRoute, nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

// Health answers about the file, not about the host. The host is always
// running; whether the module is there is the only thing worth asking.
func TestHealthReadsTheFile(t *testing.T) {
	dir := t.TempDir()
	module := filepath.Join(dir, "glyph-module.js")
	if err := os.WriteFile(module, []byte("export const render = () => {}"), 0o600); err != nil {
		t.Fatalf("write module: %v", err)
	}

	host := New("crier", module, quiet())
	if got := host.Health(context.Background()); !got.Healthy {
		t.Errorf("Healthy = false with the module present: %s", got.Message)
	}

	if err := os.Remove(module); err != nil {
		t.Fatalf("remove module: %v", err)
	}
	got := host.Health(context.Background())
	if got.Healthy {
		t.Error("Healthy = true with the module gone")
	}
	if !strings.Contains(got.Message, module) {
		t.Errorf("message %q does not name the module %q", got.Message, module)
	}
}

func TestMetadataNameIsTheRoute(t *testing.T) {
	host := New("crier", "/srv/glyphs/crier.js", quiet())
	if got := host.Metadata().Name; got != "crier" {
		t.Errorf("Name = %q, want %q", got, "crier")
	}
	if got := host.Module(); got != "/srv/glyphs/crier.js" {
		t.Errorf("Module = %q, want %q", got, "/srv/glyphs/crier.js")
	}
}

