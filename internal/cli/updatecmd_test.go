package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/riftbane/veduta/internal/update"
)

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
