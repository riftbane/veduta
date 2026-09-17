package veduta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
)

// TestDirSaves: the player's saves are files that survive a write cut short.
func TestDirSaves(t *testing.T) {
	d := dirSaves(filepath.Join(t.TempDir(), "saves"))
	if names, err := d.names(); err != nil || len(names) != 0 {
		t.Fatalf("no directory yet: %v %v", names, err)
	}
	if err := d.write("slot1", []byte(`{"gold":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := d.write("slot1", []byte(`{"gold":2}`)); err != nil {
		t.Fatal(err)
	}
	if b, ok, err := d.read("slot1"); !ok || err != nil || string(b) != `{"gold":2}` {
		t.Fatalf("read %s %v %v", b, ok, err)
	}
	entries, _ := os.ReadDir(string(d))
	if len(entries) != 1 {
		t.Fatalf("left behind %v", entries)
	}

	// Cut off after the old save became the backup and before the new one was in place.
	os.Rename(d.path("slot1", saveExt), d.path("slot1", saveBackup))
	os.WriteFile(d.path("slot1", saveTemp), []byte(`{"gold":`), 0o644)
	if b, ok, _ := d.read("slot1"); !ok || string(b) != `{"gold":2}` {
		t.Fatalf("after a cut write: %s %v", b, ok)
	}
	// A save cut short in its own file is not a save.
	os.WriteFile(d.path("slot2", saveExt), []byte(`{"gold":3`), 0o644)
	if _, ok, _ := d.read("slot2"); ok {
		t.Fatal("a truncated save was read")
	}
	if names, _ := d.names(); strings.Join(names, ",") != "slot1" {
		t.Fatalf("names %v", names)
	}
	if err := d.remove("slot1"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := d.read("slot1"); ok {
		t.Fatal("removed save still read")
	}
	if checkSave("slot3", []byte(`"text"`)) == nil || checkSave("Slot", []byte(`{}`)) == nil || checkSave("big", make([]byte, asset.MaxSaveBytes+1)) == nil {
		t.Fatal("checkSave accepted what a save cannot be")
	}
}
