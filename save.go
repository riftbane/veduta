package veduta

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/riftbane/veduta/v2/asset"
)

// A game's saves are named blobs of JSON that outlive a run: progress, settings, high
// scores. Where they live depends on the run. The player keeps them in files, one per save,
// in a directory: VEDUTA_SAVE_DIR when set (the console points it at the card), else
// out/saves in the project. Headless runs keep them in memory, starting from the
// scenario's saves, so a test never reads or writes a file and plays the same everywhere.

// SaveDirEnv names the directory the player keeps a game's saves in.
const SaveDirEnv = "VEDUTA_SAVE_DIR"

// Trace events of saves.
const (
	EventSaveWrite  = "save_write"
	EventSaveRemove = "save_remove"
)

// saveStore holds a run's saves.
type saveStore interface {
	read(name string) ([]byte, bool, error)
	write(name string, data []byte) error
	remove(name string) error
	names() ([]string, error)
}

// memorySaves are the saves of a headless run.
type memorySaves map[string][]byte

func (m memorySaves) read(name string) ([]byte, bool, error) {
	b, ok := m[name]
	return bytes.Clone(b), ok, nil
}

func (m memorySaves) write(name string, data []byte) error {
	m[name] = bytes.Clone(data)
	return nil
}

func (m memorySaves) remove(name string) error {
	delete(m, name)
	return nil
}

func (m memorySaves) names() ([]string, error) {
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// dirSaves are the saves of the player: <dir>/<name>.json. A write goes to a temporary
// file first, synced, and the previous save is kept as <name>.json.bak until the new one
// is in place, so a console switched off in the middle of a write still has one of the two.
type dirSaves string

const (
	saveExt    = ".json"
	saveBackup = ".json.bak"
	saveTemp   = ".json.tmp"
)

func (d dirSaves) path(name, ext string) string { return filepath.Join(string(d), name+ext) }

func (d dirSaves) read(name string) ([]byte, bool, error) {
	for _, ext := range []string{saveExt, saveBackup} {
		b, err := os.ReadFile(d.path(name, ext))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		if len(bytes.TrimSpace(b)) > 0 && validSave(b) {
			return b, true, nil
		}
	}
	return nil, false, nil
}

func (d dirSaves) write(name string, data []byte) error {
	if err := os.MkdirAll(string(d), 0o755); err != nil {
		return err
	}
	tmp := d.path(name, saveTemp)
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	cur := d.path(name, saveExt)
	if _, err := os.Stat(cur); err == nil {
		if err := os.Rename(cur, d.path(name, saveBackup)); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, cur); err != nil {
		return err
	}
	syncDir(string(d))
	os.Remove(d.path(name, saveBackup))
	syncDir(string(d))
	return nil
}

func (d dirSaves) remove(name string) error {
	for _, ext := range []string{saveExt, saveBackup, saveTemp} {
		if err := os.Remove(d.path(name, ext)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	syncDir(string(d))
	return nil
}

func (d dirSaves) names() ([]string, error) {
	entries, err := os.ReadDir(string(d))
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []string{}
	for _, e := range entries {
		n := e.Name()
		for _, ext := range []string{saveBackup, saveExt} {
			if base, ok := strings.CutSuffix(n, ext); ok && asset.ValidName(base) == nil && !seen[base] {
				if _, ok, _ := d.read(base); ok {
					seen[base] = true
					out = append(out, base)
				}
				break
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// syncDir flushes a directory's entries, where the system allows it.
func syncDir(dir string) {
	if f, err := os.Open(dir); err == nil {
		f.Sync()
		f.Close()
	}
}

// validSave reports whether b is one complete JSON value, as a save written whole is.
func validSave(b []byte) bool {
	b = bytes.TrimSpace(b)
	return len(b) > 1 && (b[0] == '{' && b[len(b)-1] == '}' || b[0] == '[' && b[len(b)-1] == ']') && json.Valid(b)
}

// checkSave refuses a save a game may not write.
func checkSave(name string, data []byte) error {
	if err := asset.ValidName(name); err != nil {
		return fmt.Errorf("save %w", err)
	}
	if len(data) > asset.MaxSaveBytes {
		return fmt.Errorf("save %s: %d bytes, more than a save may hold (%d)", name, len(data), asset.MaxSaveBytes)
	}
	if !validSave(data) {
		return fmt.Errorf("save %s: not a JSON object or array", name)
	}
	return nil
}

// ReadSave returns the save called name, and whether there is one.
func (c *Context) ReadSave(name string) ([]byte, bool, error) {
	if err := asset.ValidName(name); err != nil {
		return nil, false, fmt.Errorf("save %w", err)
	}
	return c.eng.saves.read(name)
}

// WriteSave stores data, a JSON object or array of at most asset.MaxSaveBytes, as the save
// called name, replacing any, and records a save_write event.
func (c *Context) WriteSave(name string, data []byte) error {
	if err := checkSave(name, data); err != nil {
		return err
	}
	if err := c.eng.saves.write(name, data); err != nil {
		return fmt.Errorf("save %s: %w", name, err)
	}
	c.eng.emit(EventSaveWrite, map[string]any{"name": name, "bytes": len(data)})
	return nil
}

// RemoveSave deletes the save called name, if there is one, and records a save_remove
// event.
func (c *Context) RemoveSave(name string) error {
	if err := asset.ValidName(name); err != nil {
		return fmt.Errorf("save %w", err)
	}
	if err := c.eng.saves.remove(name); err != nil {
		return fmt.Errorf("save %s: %w", name, err)
	}
	c.eng.emit(EventSaveRemove, map[string]any{"name": name})
	return nil
}

// SaveNames lists the game's saves, sorted.
func (c *Context) SaveNames() ([]string, error) { return c.eng.saves.names() }
