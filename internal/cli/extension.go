package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/riftbane/veduta/v2/internal/update"
)

// ExtensionReport is the result of extension.
type ExtensionReport struct {
	OK        bool   `json:"ok"`
	Version   string `json:"version"`   // the release: this tool's
	Extension string `json:"extension"` // ExtensionVersion
	File      string `json:"file"`
	SHA256    string `json:"sha256"`
	Installed bool   `json:"installed"` // installed into VS Code with code --install-extension
}

// Human says where the extension is, or that VS Code has it.
func (r *ExtensionReport) Human() string {
	if r.Installed {
		return fmt.Sprintf("installed the Veduta extension %s (of veduta %s) into VS Code: reload its windows\n", r.Extension, r.Version)
	}
	return fmt.Sprintf("the Veduta extension %s (of veduta %s, sha256 %s) is %s: in VS Code, Extensions → … → Install from VSIX\n", r.Extension, r.Version, r.SHA256, r.File)
}

// codeProgram finds VS Code's command-line program: code on PATH, else where the Windows
// installers put it.
var codeProgram = func() string {
	if p, err := exec.LookPath("code"); err == nil {
		return p
	}
	for _, dir := range []string{os.Getenv("LOCALAPPDATA") + `\Programs\Microsoft VS Code\bin`, os.Getenv("ProgramFiles") + `\Microsoft VS Code\bin`} {
		p := filepath.Join(dir, "code.cmd")
		if _, err := os.Stat(p); err == nil && !strings.HasPrefix(dir, `\`) {
			return p
		}
	}
	return ""
}

// Extension fetches the VS Code extension of this tool's release, verified against the
// release's checksums, into out (default: the temporary directory), and with install puts
// it into VS Code. The extension and the tool then match: tool and extension are released
// together, but updated apart (VS Code keeps its extensions itself).
func Extension(ctx context.Context, env *Env, out string, install bool) (*ExtensionReport, error) {
	if !update.IsVersion(env.Version) {
		return nil, fmt.Errorf("extension: this is a development build (%s), which has no release: package editors/vscode with npx vsce package", env.Version)
	}
	code := ""
	if install {
		if code = codeProgram(); code == "" {
			return nil, fmt.Errorf("extension: VS Code's code program was not found (install VS Code, or run without --install and install the file from VS Code)")
		}
	}
	client := &http.Client{Timeout: 120 * time.Second}
	rel, err := update.ByTag(ctx, client, env.Version)
	if err != nil {
		return nil, err
	}
	data, sum, err := update.Extension(ctx, client, rel)
	if err != nil {
		return nil, err
	}
	if out == "" {
		out = filepath.Join(os.TempDir(), "veduta-vscode-"+env.Version+".vsix")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return nil, err
	}
	r := &ExtensionReport{OK: true, Version: env.Version, Extension: ExtensionVersion, File: out, SHA256: sum}
	if install {
		cmd := exec.CommandContext(ctx, code, "--install-extension", out, "--force")
		if b, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("extension: code --install-extension: %v: %s", err, strings.TrimSpace(string(b)))
		}
		r.Installed = true
	}
	return r, nil
}

func init() {
	register(command{
		name: "extension", usage: "extension [--out FILE] [--install]", summary: "fetch the VS Code extension released with this tool (checksum verified) and, with --install, put it into VS Code",
		run: func(env *Env, _ *Session, args []string) (any, error) {
			fs := newFlags("extension", env.Stderr)
			out := fs.String("out", "", "where to write the .vsix (default: the temporary directory)")
			install := fs.Bool("install", false, "install it with VS Code's code program")
			if err := parseFlags(fs, args); err != nil {
				return nil, err
			}
			return Extension(context.Background(), env, *out, *install)
		},
	})
}
