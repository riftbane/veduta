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
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
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
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func TestApplyReplacesAtomically(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tool self-update is out of scope on Windows")
	}
	srv := fakeRelease(t, "v0.2.0", "v0.2.0", false)
	useServer(t, srv)
	rel, err := Latest(context.Background(), srv.Client())
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
			rel, _ := Latest(context.Background(), srv.Client())
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
	st := Check(context.Background(), "v0.1.0", 24*time.Hour, now)
	if !st.Available || st.Latest != "v0.3.0" || st.Cached || st.Error != "" {
		t.Fatalf("first check %+v", st)
	}
	srv.Close() // offline from now on
	st = Check(context.Background(), "v0.3.0", 24*time.Hour, now.Add(time.Hour))
	if !st.Cached || st.Available {
		t.Fatalf("cached check %+v", st)
	}
	st = Check(context.Background(), "v0.3.0", 24*time.Hour, now.Add(48*time.Hour))
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
}
