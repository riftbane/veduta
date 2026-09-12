// Package update finds Veduta releases on GitHub, checks for newer versions of the tool
// (rate-limited through a cache, offline-safe) and replaces the tool binary atomically
// after verifying the archive's SHA-256 against the release's checksums.txt (spec §13).
package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository releases come from.
const Repo = "riftbane/veduta"

// APIBase is the GitHub API root (replaced in tests).
var APIBase = "https://api.github.com"

// CheckTimeout bounds background update checks (spec §13.3).
const CheckTimeout = 3 * time.Second

// Release channels. The stable channel takes the release GitHub marks as the latest one;
// the beta channel takes the newest of every published release, pre-releases included, so
// it is a superset that serves a stable release whenever that is the newer one.
const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"
)

// releaseListSize is how many of the newest releases the beta channel looks at.
const releaseListSize = 30

// ValidChannel reports whether s names a release channel.
func ValidChannel(s string) bool { return s == ChannelStable || s == ChannelBeta }

// normChannel maps the empty channel to stable, so a zero Config and a cache file written
// before channels existed both mean stable.
func normChannel(s string) string {
	if s == "" {
		return ChannelStable
	}
	return s
}

// Config is ~/.config/veduta/config.json.
type Config struct {
	AutoUpdate         string `json:"auto_update"` // check (default), auto, off
	CheckIntervalHours int    `json:"check_interval_hours"`
	Channel            string `json:"channel"` // stable (default), beta
}

// DefaultConfig is used when the file is missing.
var DefaultConfig = Config{AutoUpdate: "check", CheckIntervalHours: 24, Channel: ChannelStable}

// ConfigPath returns the path of the configuration file.
func ConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "veduta", "config.json"), nil
}

// LoadConfig reads the configuration; a missing file gives DefaultConfig, invalid values
// are errors.
func LoadConfig() (Config, error) {
	cfg := DefaultConfig
	p, err := ConfigPath()
	if err != nil {
		return cfg, nil
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return DefaultConfig, fmt.Errorf("%s: %w", p, err)
	}
	return validate(cfg, p)
}

// validate rejects invalid values and gives every field left at its zero value its
// default. p names the file in the error messages.
func validate(cfg Config, p string) (Config, error) {
	switch cfg.AutoUpdate {
	case "check", "auto", "off":
	default:
		return DefaultConfig, fmt.Errorf("%s: auto_update %q (want check, auto or off)", p, cfg.AutoUpdate)
	}
	switch cfg.Channel {
	case "": // absent or empty: the default channel, like every other zero value
		cfg.Channel = DefaultConfig.Channel
	case ChannelStable, ChannelBeta:
	default:
		return DefaultConfig, fmt.Errorf("%s: channel %q (want stable or beta)", p, cfg.Channel)
	}
	if cfg.CheckIntervalHours <= 0 {
		cfg.CheckIntervalHours = DefaultConfig.CheckIntervalHours
	}
	return cfg, nil
}

// SaveConfig writes the configuration, creating its directory. It is written to a
// temporary file next to its destination and renamed, so an interrupted write never
// leaves half a configuration behind. It returns the path written.
func SaveConfig(cfg Config) (string, error) {
	p, err := ConfigPath()
	if err != nil {
		return "", err
	}
	if cfg, err = validate(cfg, p); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".config-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	fail := func(err error) (string, error) {
		tmp.Close()
		os.Remove(name)
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	if err := os.Rename(name, p); err != nil {
		os.Remove(name)
		return "", err
	}
	return p, nil
}

// Release is a published release.
type Release struct {
	Tag    string            `json:"tag"`
	Assets map[string]string `json:"assets"` // file name → download URL
}

// Latest asks GitHub for the newest release of a channel. An empty channel means stable.
func Latest(ctx context.Context, client *http.Client, channel string) (*Release, error) {
	switch normChannel(channel) {
	case ChannelStable:
		return latestStable(ctx, client)
	case ChannelBeta:
		return latestBeta(ctx, client)
	}
	return nil, fmt.Errorf("latest release: channel %q (want stable or beta)", channel)
}

// releaseBody is the part of a GitHub release the tool reads. Decoding stays lenient: the
// rest of the fields are none of its business.
type releaseBody struct {
	TagName string `json:"tag_name"`
	Draft   bool   `json:"draft"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (b releaseBody) release() *Release {
	r := &Release{Tag: b.TagName, Assets: map[string]string{}}
	for _, a := range b.Assets {
		r.Assets[a.Name] = a.URL
	}
	return r
}

// apiGet reads a GitHub API path into v.
func apiGet(ctx context.Context, client *http.Client, path string, limit int64, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, APIBase+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("latest release: GitHub answered %s", resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, limit)).Decode(v); err != nil {
		return fmt.Errorf("latest release: %w", err)
	}
	return nil
}

// latestStable takes the release GitHub marks as the latest one, which never is a
// pre-release.
func latestStable(ctx context.Context, client *http.Client) (*Release, error) {
	var body releaseBody
	if err := apiGet(ctx, client, "/repos/"+Repo+"/releases/latest", 4<<20, &body); err != nil {
		return nil, err
	}
	if !IsVersion(body.TagName) {
		return nil, fmt.Errorf("latest release: tag %q is not a version", body.TagName)
	}
	return body.release(), nil
}

// latestBeta takes the highest version among the newest published releases. GitHub returns
// them in publication order, which is not version order (a patch of an old line can be
// published after a candidate of a new one), so the order is never trusted: every usable
// tag is compared.
func latestBeta(ctx context.Context, client *http.Client) (*Release, error) {
	var list []releaseBody
	path := fmt.Sprintf("/repos/%s/releases?per_page=%d", Repo, releaseListSize)
	if err := apiGet(ctx, client, path, 8<<20, &list); err != nil {
		return nil, err
	}
	var best *Release
	for _, b := range list {
		if b.Draft || !IsVersion(b.TagName) {
			continue
		}
		if best == nil || Compare(b.TagName, best.Tag) > 0 {
			best = b.release()
		}
	}
	if best == nil {
		return nil, fmt.Errorf("latest release: no published release with a version tag among the newest %d", releaseListSize)
	}
	return best, nil
}

var versionRe = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$`)

// IsVersion reports whether s is a release version vX.Y.Z[-pre].
func IsVersion(s string) bool { return versionRe.MatchString(s) }

// Compare orders two versions: -1, 0 or 1. A pre-release sorts before its release.
// Non-versions (such as "dev") sort before every version.
func Compare(a, b string) int {
	ma, mb := versionRe.FindStringSubmatch(a), versionRe.FindStringSubmatch(b)
	switch {
	case ma == nil && mb == nil:
		return strings.Compare(a, b)
	case ma == nil:
		return -1
	case mb == nil:
		return 1
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(ma[i])
		y, _ := strconv.Atoi(mb[i])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case ma[4] == mb[4]:
		return 0
	case ma[4] == "":
		return 1
	case mb[4] == "":
		return -1
	}
	return comparePre(ma[4], mb[4])
}

// comparePre orders two pre-release suffixes as SemVer §11.4 does: identifier by
// identifier, numeric identifiers as numbers and below alphanumeric ones, and a suffix
// that runs out of identifiers first sorts lower ("rc" before "rc.1").
func comparePre(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, y := as[i], bs[i]
		if x == y {
			continue
		}
		switch {
		case isNum(x) && isNum(y):
			// Equal in value but not as text ("007" and "7"): keep going, the next
			// identifier decides.
			if c := compareNum(x, y); c != 0 {
				return c
			}
			continue
		case isNum(x):
			return -1
		case isNum(y):
			return 1
		}
		return strings.Compare(x, y)
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	}
	return 0
}

// isNum reports whether s is a non-empty run of digits.
func isNum(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// compareNum orders two digit strings by value. They are compared as strings, so an
// identifier too large for an int cannot overflow.
func compareNum(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

// ArchiveName is the release archive of the tool for goos/goarch (spec §13.1).
func ArchiveName(tag, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("veduta_%s_%s_%s%s", tag, goos, goarch, ext)
}

// Status is the result of an update check.
type Status struct {
	Current   string    `json:"current"`
	Channel   string    `json:"channel"`
	Latest    string    `json:"latest,omitempty"`
	Available bool      `json:"available"`
	Downgrade bool      `json:"downgrade"` // the channel's newest release is older than this build
	CheckedAt time.Time `json:"checked_at,omitempty"`
	Cached    bool      `json:"cached"`
	Error     string    `json:"error,omitempty"`
}

// setDirection fills Available and Downgrade from Current and Latest.
func (st *Status) setDirection() {
	c := Compare(st.Current, st.Latest)
	st.Available, st.Downgrade = c < 0, c > 0
}

type cacheFile struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
	Channel   string    `json:"channel"`
}

// CachePath is ~/.cache/veduta/update.json.
func CachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "veduta", "update.json"), nil
}

// Check reports whether a newer release than current exists. It asks GitHub at most once
// per interval (the answer is cached), never takes longer than CheckTimeout, and turns
// network problems into Status.Error instead of failing.
func Check(ctx context.Context, current, channel string, interval time.Duration, now time.Time) Status {
	ch := normChannel(channel)
	st := Status{Current: current, Channel: ch}
	cp, _ := CachePath()
	if cp != "" && interval > 0 {
		if data, err := os.ReadFile(cp); err == nil {
			var c cacheFile
			// An answer from another channel is a miss, not a stale hit; a file written
			// before channels existed holds a stable answer.
			if json.Unmarshal(data, &c) == nil && normChannel(c.Channel) == ch && now.Sub(c.CheckedAt) < interval && IsVersion(c.Latest) {
				st.Latest, st.CheckedAt, st.Cached = c.Latest, c.CheckedAt, true
				st.setDirection()
				return st
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()
	rel, err := Latest(ctx, &http.Client{Timeout: CheckTimeout}, ch)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Latest, st.CheckedAt = rel.Tag, now
	st.setDirection()
	if cp != "" {
		if data, err := json.Marshal(cacheFile{CheckedAt: now, Latest: rel.Tag, Channel: ch}); err == nil {
			_ = os.MkdirAll(filepath.Dir(cp), 0o755)
			_ = os.WriteFile(cp, data, 0o644)
		}
	}
	return st
}

// Result describes an applied update.
type Result struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Binary   string `json:"binary"`
	Archive  string `json:"archive"`
	SHA256   string `json:"sha256"`
	Verified bool   `json:"verified"`
}

// Apply downloads the release archive for this platform, verifies its SHA-256 against
// checksums.txt, extracts the tool, checks that it runs and reports rel.Tag, and replaces
// exe atomically (temporary file next to it, then rename). exe is untouched on any error.
func Apply(ctx context.Context, client *http.Client, rel *Release, exe, current string) (*Result, error) {
	if runtime.GOOS == "windows" {
		return nil, errors.New("update: replacing the tool on Windows is out of scope in v0.1.0; download the new archive from GitHub Releases")
	}
	name := ArchiveName(rel.Tag, runtime.GOOS, runtime.GOARCH)
	url, ok := rel.Assets[name]
	if !ok {
		return nil, fmt.Errorf("update: release %s has no %s", rel.Tag, name)
	}
	sumsURL, ok := rel.Assets["checksums.txt"]
	if !ok {
		return nil, fmt.Errorf("update: release %s has no checksums.txt", rel.Tag)
	}
	sums, err := download(ctx, client, sumsURL, 1<<20)
	if err != nil {
		return nil, err
	}
	want, err := checksumFor(sums, name)
	if err != nil {
		return nil, err
	}
	archive, err := download(ctx, client, url, 512<<20)
	if err != nil {
		return nil, err
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("update: %s checksum mismatch (got %x, want %s); nothing was changed", name, got, want)
	}
	bin, err := extract(archive, name)
	if err != nil {
		return nil, err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".veduta-update-*")
	if err != nil {
		return nil, fmt.Errorf("update: cannot write next to %s: %w", exe, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { os.Remove(tmpName) }
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		cleanup()
		return nil, err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		cleanup()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return nil, err
	}
	out, err := exec.CommandContext(ctx, tmpName, "version", "--json").Output()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("update: the downloaded tool does not run: %w", err)
	}
	var v struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &v) != nil || v.Version != rel.Tag {
		cleanup()
		return nil, fmt.Errorf("update: the downloaded tool reports version %q, want %s", v.Version, rel.Tag)
	}
	if err := os.Rename(tmpName, exe); err != nil {
		cleanup()
		return nil, fmt.Errorf("update: replace %s: %w", exe, err)
	}
	return &Result{From: current, To: rel.Tag, Binary: exe, Archive: name, SHA256: want, Verified: true}, nil
}

func download(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download %s: larger than %d bytes", url, limit)
	}
	return data, nil
}

// checksumFor finds name in a sha256sum-format file.
func checksumFor(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("update: checksums.txt has no entry for %s", name)
}

// extract returns the veduta binary from a release archive.
func extract(archive []byte, name string) ([]byte, error) {
	if strings.HasSuffix(name, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) == "veduta.exe" {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(io.LimitReader(rc, 512<<20))
			}
		}
		return nil, fmt.Errorf("update: %s has no veduta.exe", name)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("update: %s: %w", name, err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("update: %s has no veduta binary", name)
		}
		if err != nil {
			return nil, fmt.Errorf("update: %s: %w", name, err)
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == "veduta" {
			return io.ReadAll(io.LimitReader(tr, 512<<20))
		}
	}
}
