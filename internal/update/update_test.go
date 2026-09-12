package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.1.0", "v0.1.0", 0}, {"v0.1.0", "v0.1.1", -1}, {"v0.2.0", "v0.10.0", -1},
		{"v1.0.0", "v0.99.99", 1}, {"v0.1.0-rc.1", "v0.1.0", -1}, {"dev", "v0.0.1", -1},
		{"v0.1.0-rc.1", "v0.1.0-rc.2", -1},
		// Pre-release suffixes follow SemVer §11.4, not string order: the tenth
		// candidate comes after the second.
		{"v0.2.0-rc.2", "v0.2.0-rc.10", -1}, {"v0.2.0-beta.10", "v0.2.0-beta.9", 1},
		{"v0.2.0-alpha.1", "v0.2.0-beta.1", -1}, {"v0.2.0-rc", "v0.2.0-rc.1", -1},
		{"v0.2.0-rc.1", "v0.2.0-rc.1.1", -1}, {"v0.2.0-1", "v0.2.0-alpha", -1},
		{"v0.2.0-rc.1", "v0.2.0-rc.1", 0}, {"v0.2.0-rc.007", "v0.2.0-rc.7", 0},
		// Identifiers equal in value but not as text must not end the comparison.
		{"v0.2.0-rc.007.1", "v0.2.0-rc.7.2", -1}, {"v0.2.0-rc.7.2", "v0.2.0-rc.007.10", -1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := Compare(c.b, c.a); got != -c.want {
			t.Errorf("Compare(%s, %s) = %d, want %d (the order must be symmetric)", c.b, c.a, got, -c.want)
		}
	}
	if ArchiveName("v0.1.0", "linux", "amd64") != "veduta_v0.1.0_linux_amd64.tar.gz" || ArchiveName("v0.1.0", "windows", "amd64") != "veduta_v0.1.0_windows_amd64.zip" {
		t.Fatal("archive names differ from the release workflow")
	}
}

// fakeRelease serves a release whose archive holds a shell-script "veduta" that prints
// the given version.
func fakeRelease(t *testing.T, tag, reportVersion string, corrupt bool) *httptest.Server {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\necho '{\"version\":\"%s\"}'\n", reportVersion)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "veduta", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg})
	tw.Write([]byte(script))
	tw.WriteHeader(&tar.Header{Name: "LICENSE", Mode: 0o644, Size: 3, Typeflag: tar.TypeReg})
	tw.Write([]byte("MIT"))
	tw.Close()
	gz.Close()
	archive := buf.Bytes()
	name := ArchiveName(tag, runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(archive)
	sums := hex.EncodeToString(sum[:]) + "  " + name + "\n"
	if corrupt {
		archive = append([]byte(nil), archive...)
		archive[len(archive)/2] ^= 0xff
	}
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "assets": []map[string]string{
			{"name": name, "browser_download_url": srv.URL + "/dl/" + name},
			{"name": "checksums.txt", "browser_download_url": srv.URL + "/dl/checksums.txt"},
		}})
	})
	mux.HandleFunc("/repos/"+Repo+"/releases", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{{"tag_name": tag, "draft": false, "assets": []map[string]string{
			{"name": name, "browser_download_url": srv.URL + "/dl/" + name},
			{"name": "checksums.txt", "browser_download_url": srv.URL + "/dl/checksums.txt"},
		}}})
	})
	mux.HandleFunc("/dl/"+name, func(w http.ResponseWriter, r *http.Request) { w.Write(archive) })
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(sums)) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func useServer(t *testing.T, srv *httptest.Server) {
	old := APIBase
	APIBase = srv.URL
	t.Cleanup(func() { APIBase = old })
	// Redirect every platform's config and cache directory, so a test never reads or
	// writes the real one (Windows takes APPDATA and LOCALAPPDATA, macOS takes HOME).
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("APPDATA", filepath.Join(dir, "config"))
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "cache"))
	t.Setenv("HOME", dir)
}

// fakeCatalog serves the two release routes for the given tags and nothing else: its
// callers exercise which release a channel picks, not downloading. Tags are served in the
// order given — callers pass them oldest first, so an implementation that takes the first
// answer instead of the highest version fails. A tag written "draft:vX.Y.Z" is served as a
// draft; /releases/latest answers with the highest tag without a pre-release suffix.
func fakeCatalog(t *testing.T, tags ...string) *httptest.Server {
	t.Helper()
	list := []map[string]any{}
	stable := ""
	for _, tag := range tags {
		draft := strings.HasPrefix(tag, "draft:")
		tag = strings.TrimPrefix(tag, "draft:")
		list = append(list, map[string]any{"tag_name": tag, "draft": draft, "assets": []map[string]string{}})
		if !draft && IsVersion(tag) && !strings.Contains(tag, "-") && (stable == "" || Compare(tag, stable) > 0) {
			stable = tag
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+Repo+"/releases", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(list)
	})
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if stable == "" {
			http.Error(w, "no stable release", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": stable, "assets": []map[string]string{}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestApplyReplacesAtomically(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tool self-update is out of scope on Windows")
	}
	srv := fakeRelease(t, "v0.2.0", "v0.2.0", false)
	useServer(t, srv)
	rel, err := Latest(context.Background(), srv.Client(), ChannelStable)
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "veduta")
	os.WriteFile(exe, []byte("old"), 0o755)
	res, err := Apply(context.Background(), srv.Client(), rel, exe, "v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Verified || res.To != "v0.2.0" {
		t.Fatalf("result %+v", res)
	}
	data, _ := os.ReadFile(exe)
	if !strings.Contains(string(data), `"version":"v0.2.0"`) {
		t.Fatalf("binary not replaced: %q", data)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".veduta-update-*")); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

func TestApplyRejectsBadArchives(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tool self-update is out of scope on Windows")
	}
	for _, c := range []struct {
		name            string
		report          string
		corrupt         bool
		wantErrContains string
	}{
		{"checksum", "v0.2.0", true, "checksum mismatch"},
		{"wrong version", "v0.1.5", false, "reports version"},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := fakeRelease(t, "v0.2.0", c.report, c.corrupt)
			useServer(t, srv)
			rel, _ := Latest(context.Background(), srv.Client(), ChannelStable)
			exe := filepath.Join(t.TempDir(), "veduta")
			os.WriteFile(exe, []byte("old"), 0o755)
			if _, err := Apply(context.Background(), srv.Client(), rel, exe, "v0.1.0"); err == nil || !strings.Contains(err.Error(), c.wantErrContains) {
				t.Fatalf("err = %v", err)
			}
			if data, _ := os.ReadFile(exe); string(data) != "old" {
				t.Fatal("binary changed despite the error")
			}
		})
	}
}

func TestCheckCachesAndIsOfflineSafe(t *testing.T) {
	srv := fakeRelease(t, "v0.3.0", "v0.3.0", false)
	useServer(t, srv)
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	st := Check(context.Background(), "v0.1.0", ChannelStable, 24*time.Hour, now)
	if !st.Available || st.Latest != "v0.3.0" || st.Cached || st.Error != "" {
		t.Fatalf("first check %+v", st)
	}
	srv.Close() // offline from now on
	st = Check(context.Background(), "v0.3.0", ChannelStable, 24*time.Hour, now.Add(time.Hour))
	if !st.Cached || st.Available {
		t.Fatalf("cached check %+v", st)
	}
	st = Check(context.Background(), "v0.3.0", ChannelStable, 24*time.Hour, now.Add(48*time.Hour))
	if st.Error == "" || st.Available {
		t.Fatalf("offline check should report an error, not fail: %+v", st)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir) // Linux
	t.Setenv("APPDATA", dir)         // Windows
	t.Setenv("HOME", dir)            // macOS: $HOME/Library/Application Support
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if cfg, err := LoadConfig(); err != nil || cfg != DefaultConfig {
		t.Fatalf("default: %+v %v", cfg, err)
	}
	file := filepath.Join(base, "veduta", "config.json")
	os.MkdirAll(filepath.Dir(file), 0o755)
	os.WriteFile(file, []byte(`{"auto_update":"auto","check_interval_hours":6}`), 0o644)
	if cfg, err := LoadConfig(); err != nil || cfg.AutoUpdate != "auto" || cfg.CheckIntervalHours != 6 {
		t.Fatalf("auto: %+v %v", cfg, err)
	}
	os.WriteFile(file, []byte(`{"auto_update":"sometimes"}`), 0o644)
	if _, err := LoadConfig(); err == nil {
		t.Fatal("invalid mode accepted")
	}
	os.WriteFile(file, []byte(`{"auto_update":"check","channel":"beta"}`), 0o644)
	if cfg, err := LoadConfig(); err != nil || cfg.Channel != ChannelBeta {
		t.Fatalf("beta: %+v %v", cfg, err)
	}
	// An empty channel is the default one, like every other zero value.
	os.WriteFile(file, []byte(`{"channel":""}`), 0o644)
	if cfg, err := LoadConfig(); err != nil || cfg.Channel != ChannelStable {
		t.Fatalf("empty channel: %+v %v", cfg, err)
	}
	os.WriteFile(file, []byte(`{"channel":"nightly"}`), 0o644)
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "want stable or beta") {
		t.Fatalf("invalid channel: %v", err)
	}
	// Unknown keys stay errors: a configuration written by a newer tool must not be
	// silently half-read by an older one.
	os.WriteFile(file, []byte(`{"chanel":"beta"}`), 0o644)
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown key: %v", err)
	}
}

func TestLatestChannels(t *testing.T) {
	for _, c := range []struct {
		name    string
		tags    []string
		channel string
		want    string
		wantErr string
	}{
		{"stable ignores candidates", []string{"v0.1.0", "v0.2.0-rc.1", "v0.2.0-rc.2"}, ChannelStable, "v0.1.0", ""},
		{"beta takes the newest candidate", []string{"v0.1.0", "v0.2.0-rc.1", "v0.2.0-rc.2"}, ChannelBeta, "v0.2.0-rc.2", ""},
		{"beta counts candidates, not strings", []string{"v0.2.0-rc.2", "v0.2.0-rc.10"}, ChannelBeta, "v0.2.0-rc.10", ""},
		{"beta skips drafts and other tags", []string{"v0.1.0", "nightly", "draft:v9.9.9"}, ChannelBeta, "v0.1.0", ""},
		{"an empty channel is stable", []string{"v0.1.0", "v0.2.0-rc.1"}, "", "v0.1.0", ""},
		{"beta with nothing usable", []string{"nightly"}, ChannelBeta, "", "no published release with a version tag"},
		{"unknown channel", []string{"v0.1.0"}, "nightly", "", "want stable or beta"},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := fakeCatalog(t, c.tags...)
			useServer(t, srv)
			rel, err := Latest(context.Background(), srv.Client(), c.channel)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, c.wantErr)
				}
				return
			}
			if err != nil || rel.Tag != c.want {
				t.Fatalf("Latest(%q) = %+v, %v, want %s", c.channel, rel, err, c.want)
			}
		})
	}
}

// TestLatestBetaPrefersNewerStable is the rule that makes beta safe to opt into: it is a
// superset of stable, so it can never answer with an older release than stable would.
func TestLatestBetaPrefersNewerStable(t *testing.T) {
	for _, c := range []struct {
		tags []string
		want string
	}{
		{[]string{"v0.2.0-rc.1", "v0.2.0"}, "v0.2.0"}, // the candidate was released
		{[]string{"v0.2.0-rc.3", "v0.3.0"}, "v0.3.0"}, // a later line went stable
		{[]string{"v0.2.0", "v0.2.1-rc.1"}, "v0.2.1-rc.1"},
	} {
		srv := fakeCatalog(t, c.tags...)
		useServer(t, srv)
		rel, err := Latest(context.Background(), srv.Client(), ChannelBeta)
		if err != nil || rel.Tag != c.want {
			t.Fatalf("beta over %v = %+v, %v, want %s", c.tags, rel, err, c.want)
		}
	}
}

func TestCheckIsChannelAware(t *testing.T) {
	srv := fakeCatalog(t, "v0.1.0", "v0.2.0-rc.1")
	useServer(t, srv)
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	st := Check(context.Background(), "v0.1.0", ChannelBeta, 24*time.Hour, now)
	if st.Channel != ChannelBeta || st.Latest != "v0.2.0-rc.1" || !st.Available || st.Cached {
		t.Fatalf("beta check %+v", st)
	}
	if st = Check(context.Background(), "v0.1.0", ChannelBeta, 24*time.Hour, now.Add(time.Hour)); !st.Cached {
		t.Fatalf("second beta check %+v, want a cache hit", st)
	}
	// Offline from here: only the cache could answer, and it holds a beta answer.
	srv.Close()
	st = Check(context.Background(), "v0.1.0", ChannelStable, 24*time.Hour, now.Add(2*time.Hour))
	if st.Cached || st.Available || st.Error == "" {
		t.Fatalf("stable check %+v, want a miss rather than the beta answer", st)
	}
}

func TestCheckCacheAcceptsOldFileAsStable(t *testing.T) {
	srv := fakeCatalog(t, "v0.1.0")
	useServer(t, srv)
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	cp, err := CachePath()
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(cp), 0o755)
	// A cache file written before channels existed holds a stable answer.
	os.WriteFile(cp, []byte(`{"checked_at":"`+now.Format(time.RFC3339)+`","latest":"v0.3.0"}`), 0o644)
	if st := Check(context.Background(), "v0.1.0", ChannelStable, 24*time.Hour, now.Add(time.Hour)); !st.Cached || st.Latest != "v0.3.0" {
		t.Fatalf("stable check %+v, want the old cache file", st)
	}
	if st := Check(context.Background(), "v0.1.0", ChannelBeta, 24*time.Hour, now.Add(time.Hour)); st.Cached {
		t.Fatalf("beta check %+v, want a miss", st)
	}
}

func TestCheckReportsDowngrade(t *testing.T) {
	srv := fakeCatalog(t, "v0.2.1")
	useServer(t, srv)
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	st := Check(context.Background(), "v0.3.0-rc.2", ChannelStable, 24*time.Hour, now)
	if st.Available || !st.Downgrade || st.Latest != "v0.2.1" || st.Channel != ChannelStable {
		t.Fatalf("check from ahead of stable %+v", st)
	}
	if st = Check(context.Background(), "v0.3.0-rc.2", ChannelStable, 24*time.Hour, now.Add(time.Hour)); !st.Cached || !st.Downgrade {
		t.Fatalf("cached check %+v, want Downgrade reported there too", st)
	}
	if st = Check(context.Background(), "v0.2.1", ChannelStable, 24*time.Hour, now.Add(time.Hour)); st.Available || st.Downgrade {
		t.Fatalf("check on the newest release %+v, want neither direction", st)
	}
}

func TestSaveConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("HOME", dir)
	p, err := SaveConfig(Config{AutoUpdate: "auto", CheckIntervalHours: 6, Channel: ChannelBeta})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil || cfg.Channel != ChannelBeta || cfg.AutoUpdate != "auto" || cfg.CheckIntervalHours != 6 {
		t.Fatalf("round trip %+v %v", cfg, err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o644 {
		t.Errorf("mode %v, want 0644", fi.Mode().Perm())
	}
	if _, err := SaveConfig(Config{AutoUpdate: "check", Channel: "nightly"}); err == nil {
		t.Fatal("an invalid channel was written")
	}
	if cfg, _ := LoadConfig(); cfg.Channel != ChannelBeta {
		t.Fatalf("the refused write changed the file: %+v", cfg)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(p), ".config-*")); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

// TestApplyPreRelease proves a hyphenated tag needs no special case downstream: the
// archive name, checksums.txt and the version the new binary reports all carry it.
func TestApplyPreRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tool self-update is out of scope on Windows")
	}
	srv := fakeRelease(t, "v0.3.0-rc.1", "v0.3.0-rc.1", false)
	useServer(t, srv)
	rel, err := Latest(context.Background(), srv.Client(), ChannelBeta)
	if err != nil || rel.Tag != "v0.3.0-rc.1" {
		t.Fatalf("beta latest %+v %v", rel, err)
	}
	exe := filepath.Join(t.TempDir(), "veduta")
	os.WriteFile(exe, []byte("old"), 0o755)
	res, err := Apply(context.Background(), srv.Client(), rel, exe, "v0.2.0")
	if err != nil || !res.Verified || res.To != "v0.3.0-rc.1" {
		t.Fatalf("apply %+v %v", res, err)
	}
	if data, _ := os.ReadFile(exe); !strings.Contains(string(data), `"version":"v0.3.0-rc.1"`) {
		t.Fatalf("binary not replaced: %q", data)
	}
}
