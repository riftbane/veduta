package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtensionVersion: the version the tool reports for its extension is the one the
// extension is released with.
func TestExtensionVersion(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "editors", "vscode", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.Version != ExtensionVersion {
		t.Fatalf("editors/vscode is %s, ExtensionVersion %s", p.Version, ExtensionVersion)
	}
	if v := versionInfo(&Env{Version: "dev"}); v.Extension != ExtensionVersion {
		t.Fatalf("version reports extension %q", v.Extension)
	}
}

// TestExtensionRefusals: a dev build has no release, and --install needs VS Code; neither
// goes to the network.
func TestExtensionRefusals(t *testing.T) {
	if _, err := Extension(context.Background(), &Env{Version: "dev"}, "", false); err == nil || !strings.Contains(err.Error(), "development build") {
		t.Fatalf("dev build: %v", err)
	}
	old := codeProgram
	codeProgram = func() string { return "" }
	t.Cleanup(func() { codeProgram = old })
	if _, err := Extension(context.Background(), &Env{Version: "v2.0.0-rc.10"}, "", true); err == nil || !strings.Contains(err.Error(), "code program was not found") {
		t.Fatalf("no VS Code: %v", err)
	}
}
