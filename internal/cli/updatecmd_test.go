package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/internal/update"
)

// fakeReleases points the update package at a server offering one release on both routes,
// and redirects the configuration and cache directories to a temporary one, which it
// returns.
func fakeReleases(t *testing.T, tag string) string {
	t.Helper()
	mux := http.NewServeMux()
	body := map[string]any{"tag_name": tag, "draft": false, "assets": []map[string]string{}}
	mux.HandleFunc("/repos/"+update.Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("/repos/"+update.Repo+"/releases", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{body})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	old := update.APIBase
	update.APIBase = srv.URL
	t.Cleanup(func() { update.APIBase = old })
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "cache"))
	return dir
}

// TestUpdateKeepsAnUnreadableConfig checks that naming a channel refuses rather than
// replacing a configuration it could not read, and that a run which writes nothing still
// works and says what is wrong.
func TestUpdateKeepsAnUnreadableConfig(t *testing.T) {
	dir := fakeReleases(t, "v9.9.9")
	p := filepath.Join(dir, "veduta", "config.json")
	os.MkdirAll(filepath.Dir(p), 0o755)
	before := []byte(`{"auto_update":"off","check_interval_hours":168,"chanel":"beta"}`)
	os.WriteFile(p, before, 0o644)

	if _, err := Update(context.Background(), &Env{Version: "v0.1.0"}, "beta", false, false); err == nil {
		t.Fatal("a configuration that could not be read was accepted for rewriting")
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(after, before) {
		t.Fatalf("configuration changed:\n got %s\nwant %s", after, before)
	}
	r, err := Update(context.Background(), &Env{Version: "v0.1.0"}, "", true, false)
	if err != nil || r.Latest != "v9.9.9" || len(r.Warnings) == 0 {
		t.Fatalf("check with an unreadable configuration: %+v %v", r, err)
	}
	if after, _ := os.ReadFile(p); !bytes.Equal(after, before) {
		t.Fatalf("a check wrote to the configuration: %s", after)
	}
}

// TestUpdateSubscribesWithoutInstalling checks that naming a channel takes effect even
// when its newest release is the one already running, and that --check never does.
func TestUpdateSubscribesWithoutInstalling(t *testing.T) {
	dir := fakeReleases(t, "v9.9.9")
	p := filepath.Join(dir, "veduta", "config.json")

	r, err := Update(context.Background(), &Env{Version: "v9.9.9"}, "beta", true, false)
	if err != nil || r.Updated || r.Following != update.ChannelStable {
		t.Fatalf("preview: %+v %v", r, err)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("--check wrote the configuration")
	}
	r, err = Update(context.Background(), &Env{Version: "v9.9.9"}, "beta", false, false)
	if err != nil || r.Updated || r.Available {
		t.Fatalf("nothing to install: %+v %v", r, err)
	}
	if r.Following != update.ChannelBeta {
		t.Fatalf("report follows %q, want beta", r.Following)
	}
	cfg, err := update.LoadConfig()
	if err != nil || cfg.Channel != update.ChannelBeta {
		t.Fatalf("configuration after subscribing: %+v %v", cfg, err)
	}
}

func TestUpdateReportHuman(t *testing.T) {
	for _, c := range []struct {
		name string
		r    UpdateReport
		want string
	}{
		{
			"updated",
			UpdateReport{Current: "v0.2.0", Channel: "beta", Following: "beta", Latest: "v0.2.1-rc.1", Updated: true,
				Result: &update.Result{From: "v0.2.0", To: "v0.2.1-rc.1", Archive: "veduta_v0.2.1-rc.1_linux_amd64.tar.gz", SHA256: "38b9c3b5"}},
			"updated v0.2.0 → v0.2.1-rc.1 on the beta channel (veduta_v0.2.1-rc.1_linux_amd64.tar.gz verified, sha256 38b9c3b5)\n",
		},
		{
			"available on the channel being followed",
			UpdateReport{Current: "v0.2.0", Channel: "stable", Following: "stable", Latest: "v0.2.1", Available: true},
			"v0.2.1 is available on the stable channel (current v0.2.0): run veduta update\n",
		},
		{
			// A preview of another channel prints the command that would install it.
			"previewing another channel",
			UpdateReport{Current: "v0.2.0", Channel: "beta", Following: "stable", Latest: "v0.2.1-rc.1", Available: true},
			"v0.2.1-rc.1 is available on the beta channel (current v0.2.0): run veduta update --channel beta\n",
		},
		{
			"ahead of the channel",
			UpdateReport{Current: "v0.3.0-rc.2", Channel: "stable", Following: "beta", Latest: "v0.2.1", Downgrade: true},
			"v0.2.1 is the newest stable release, older than this build (v0.3.0-rc.2): run veduta update --channel stable --force to go back\n",
		},
		{
			"up to date",
			UpdateReport{Current: "v0.2.0", Channel: "stable", Following: "stable", Latest: "v0.2.0"},
			"up to date (v0.2.0, newest stable release v0.2.0)\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.r.Human(); got != c.want {
				t.Errorf("Human() = %q\n     want   %q", got, c.want)
			}
		})
	}
}

// TestGoModRequires checks the test that decides whether upgrade must run go get: a
// release candidate of the version, or a longer version it is a prefix of, is not it.
func TestGoModRequires(t *testing.T) {
	for _, c := range []struct {
		mod, version string
		want         bool
	}{
		{"require github.com/riftbane/veduta v1.0.0\n", "v1.0.0", true},
		{"require github.com/riftbane/veduta v1.0.0", "v1.0.0", true},
		{"require (\n\tgithub.com/riftbane/veduta v1.0.0 // indirect\n)\n", "v1.0.0", true},
		{"require github.com/riftbane/veduta v1.0.0\r\n", "v1.0.0", true},
		{"require github.com/riftbane/veduta v1.0.0-rc.1\n", "v1.0.0", false},
		{"require github.com/riftbane/veduta v1.0.0-rc.10\n", "v1.0.0-rc.1", false},
		{"require github.com/riftbane/veduta v1.0.0-rc.1.2\n", "v1.0.0-rc.1", false},
		{"require github.com/riftbane/veduta v0.2.0\n", "v1.0.0", false},
		{"require github.com/riftbane/vedutax v1.0.0\n", "v1.0.0", false},
		{"require github.com/riftbane/veduta/v2 v2.0.0\n", "v2.0.0", true},
		{"require github.com/riftbane/veduta v2.0.0\n", "v2.0.0", false},
		{"require github.com/riftbane/veduta/v2 v1.0.0\n", "v1.0.0", false},
		{"", "v1.0.0", false},
	} {
		if got := goModRequires([]byte(c.mod), c.version); got != c.want {
			t.Errorf("goModRequires(%q, %s) = %v, want %v", c.mod, c.version, got, c.want)
		}
	}
}

// TestUpdateChannelFlag checks that an unknown channel is a usage error (exit 2) refused
// before anything is asked of the network: APIBase points at a port nothing listens on,
// so a request would fail with a connection error instead.
func TestUpdateChannelFlag(t *testing.T) {
	old := update.APIBase
	update.APIBase = "http://127.0.0.1:1"
	t.Cleanup(func() { update.APIBase = old })
	var out, errb bytes.Buffer
	code := Main([]string{"--json", "update", "--channel", "nightly"}, Env{Version: "dev", Stdout: &out, Stderr: &errb})
	printed := out.String() + errb.String()
	if code != 2 || !strings.Contains(printed, "want stable or beta") {
		t.Fatalf("exit %d, printed %q", code, printed)
	}
	if strings.Contains(printed, "connect") || strings.Contains(printed, "127.0.0.1") {
		t.Fatalf("the channel was validated after a network call: %q", printed)
	}
}

// TestRewriteEngineImports checks that an upgrade across a major version moves every engine
// import, and nothing else, to the new module path.
func TestRewriteEngineImports(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"game/game.go":     "package game\n\nimport (\n\t\"github.com/riftbane/veduta\"\n\t\"github.com/riftbane/veduta/gmath\"\n\t\"github.com/riftbane/veduta-extra/x\"\n)\n",
		"cmd/game/main.go": "package main\n\nimport \"github.com/riftbane/veduta/v2/sim\"\n",
		"out/gen.go":       "package out\n\nimport \"github.com/riftbane/veduta\"\n",
		"notes.txt":        "\"github.com/riftbane/veduta\"\n",
	}
	for name, src := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	changed, err := rewriteEngineImports(dir, "github.com/riftbane/veduta/v2")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changed, []string{"game/game.go"}) {
		t.Fatalf("changed %v, want [game/game.go]", changed)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "game", "game.go"))
	want := "package game\n\nimport (\n\t\"github.com/riftbane/veduta/v2\"\n\t\"github.com/riftbane/veduta/v2/gmath\"\n\t\"github.com/riftbane/veduta-extra/x\"\n)\n"
	if string(got) != want {
		t.Fatalf("game.go:\n%s\nwant\n%s", got, want)
	}
	for _, name := range []string{"out/gen.go", "notes.txt"} {
		if got, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name))); string(got) != files[name] {
			t.Errorf("%s changed:\n%s", name, got)
		}
	}
	if engineModule("v1.4.1") != "github.com/riftbane/veduta" || engineModule("v2.0.0-rc.2") != "github.com/riftbane/veduta/v2" || engineModule("v10.1.0") != "github.com/riftbane/veduta/v10" {
		t.Error("engineModule maps versions to the wrong module paths")
	}
}
