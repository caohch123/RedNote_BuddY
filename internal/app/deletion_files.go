package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"xhs-workbench/internal/store"
)

type deletionFileMove struct {
	Original string `json:"original"`
	Staged   string `json:"staged"`
}
type deletionStaging struct {
	ID    string             `json:"id"`
	Files []deletionFileMove `json:"files"`
}

func deletionPathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func safeDeletionID(id string) bool {
	return id != "" && id != "." && id != ".." && filepath.Base(id) == id && !strings.ContainsAny(id, `/\:`)
}

// Reject every symlink/junction component, including an existing ancestor of
// a missing file. Lexical containment alone is insufficient for deletion.
func (a *App) checkedDeletionPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(a.Data, path)
	}
	path, err := filepath.Abs(path)
	if err != nil || !within(a.Data, path) || deletionPathKey(path) == deletionPathKey(a.Data) {
		return "", errUnsafeDeletionPath
	}
	rel, err := filepath.Rel(a.Data, path)
	if err != nil {
		return "", errUnsafeDeletionPath
	}
	current := a.Data
	components := append([]string{""}, strings.Split(rel, string(filepath.Separator))...)
	for _, component := range components {
		if component != "" {
			current = filepath.Join(current, component)
		}
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || deletionReparsePoint(current, info) {
			return "", errUnsafeDeletionPath
		}
		real, realErr := filepath.EvalSymlinks(current)
		if realErr != nil || !within(a.Data, real) {
			return "", errUnsafeDeletionPath
		}
	}
	return filepath.Clean(path), nil
}

func (a *App) checkedMediaDeletionPath(path string) (string, error) {
	checked, err := a.checkedDeletionPath(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(a.Data, checked)
	if err != nil {
		return "", errUnsafeDeletionPath
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 || (parts[0] != "assets" && parts[0] != "renders" && parts[0] != "logs") {
		return "", errUnsafeDeletionPath
	}
	return checked, nil
}

func (a *App) deletionFiles(snapshot *deletionSnapshot, plan *deletionPlan) ([]string, error) {
	protected := map[string]bool{}
	for id, asset := range snapshot.assets {
		if plan.ids["assets"][id] || asset.LocalPath == "" {
			continue
		}
		// Unrelated invalid metadata does not authorize touching its path.
		path := asset.LocalPath
		if !filepath.IsAbs(path) {
			path = filepath.Join(a.Data, path)
		}
		if abs, err := filepath.Abs(path); err == nil {
			protected[deletionPathKey(abs)] = true
		}
	}
	files := map[string]string{}
	add := func(path string) error {
		checked, err := a.checkedMediaDeletionPath(path)
		if err != nil {
			return err
		}
		if protected[deletionPathKey(checked)] {
			return nil
		}
		info, err := os.Lstat(checked)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil || !info.Mode().IsRegular() {
			return errUnsafeDeletionPath
		}
		files[deletionPathKey(checked)] = checked
		return nil
	}
	for id := range plan.ids["assets"] {
		if path := snapshot.assets[id].LocalPath; path != "" {
			if err := add(path); err != nil {
				return nil, err
			}
		}
	}
	for id := range plan.ids["videos"] {
		if !safeDeletionID(id) {
			return nil, errUnsafeDeletionPath
		}
		folder, err := a.checkedMediaDeletionPath(filepath.Join(a.Data, "renders", id))
		if err != nil {
			return nil, err
		}
		err = filepath.WalkDir(folder, func(path string, entry fs.DirEntry, walkErr error) error {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if _, err := a.checkedMediaDeletionPath(path); err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			return add(path)
		})
		if err != nil {
			return nil, errUnsafeDeletionPath
		}
	}
	for id := range plan.ids["jobs"] {
		if !safeDeletionID(id) {
			return nil, errUnsafeDeletionPath
		}
		if err := add(filepath.Join(a.Data, "logs", id+".log")); err != nil {
			return nil, err
		}
	}
	paths := make([]string, 0, len(files))
	for _, path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func (a *App) deletionStageDirectory(id string) (string, error) {
	if !safeDeletionID(id) {
		return "", errUnsafeDeletionPath
	}
	return a.checkedDeletionPath(filepath.Join(a.Data, ".deletions", id))
}

// A manifest plus a record committed with the cascade distinguish interrupted
// pre-commit staging (restore) from committed staging (finish removing files).
func (a *App) stageDeletionFiles(paths []string) (*deletionStaging, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	staged := &deletionStaging{ID: ID(), Files: []deletionFileMove{}}
	dir, err := a.deletionStageDirectory(staged.ID)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	for i, path := range paths {
		rel, err := filepath.Rel(a.Data, path)
		if err != nil {
			return nil, err
		}
		staged.Files = append(staged.Files, deletionFileMove{Original: rel, Staged: fmt.Sprintf("%06d", i)})
	}
	manifest, err := json.Marshal(staged)
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0600); err != nil {
		return nil, err
	}
	for _, file := range staged.Files {
		from, err := a.checkedMediaDeletionPath(file.Original)
		if err == nil {
			_, err = a.checkedDeletionPath(filepath.Join(dir, file.Staged))
		}
		if err == nil {
			err = os.Rename(from, filepath.Join(dir, file.Staged))
		}
		if err != nil {
			// Return the journal even on failure so the caller can restore every
			// already-moved file before returning the transaction error.
			return staged, err
		}
	}
	return staged, nil
}

func (a *App) validateDeletionStaging(staged *deletionStaging) (string, error) {
	dir, err := a.deletionStageDirectory(staged.ID)
	if err != nil {
		return "", err
	}
	seen := map[string]bool{}
	for _, file := range staged.Files {
		if !safeDeletionID(file.Staged) || file.Staged == "manifest.json" || seen[file.Staged] {
			return "", errUnsafeDeletionPath
		}
		seen[file.Staged] = true
		if _, err = a.checkedMediaDeletionPath(file.Original); err != nil {
			return "", err
		}
		if _, err = a.checkedDeletionPath(filepath.Join(dir, file.Staged)); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func (a *App) rollbackDeletionFiles(staged *deletionStaging) error {
	dir, err := a.validateDeletionStaging(staged)
	if err != nil {
		return err
	}
	for i := len(staged.Files) - 1; i >= 0; i-- {
		file := staged.Files[i]
		from := filepath.Join(dir, file.Staged)
		if _, err = os.Lstat(from); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		to, err := a.checkedMediaDeletionPath(file.Original)
		if err != nil {
			return err
		}
		if _, err = os.Lstat(to); !errors.Is(err, os.ErrNotExist) {
			return errors.New("deletion rollback target is occupied")
		}
		if err = os.MkdirAll(filepath.Dir(to), 0700); err != nil {
			return err
		}
		if err = os.Rename(from, to); err != nil {
			return err
		}
	}
	return a.removeDeletionManifest(dir)
}

func (a *App) removeDeletionManifest(dir string) error {
	path, err := a.checkedDeletionPath(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (a *App) finishDeletionFiles(staged *deletionStaging) (int, error) {
	dir, err := a.validateDeletionStaging(staged)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, file := range staged.Files {
		path := filepath.Join(dir, file.Staged)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return count, errUnsafeDeletionPath
		}
		if err = os.Remove(path); err != nil {
			return count, err
		}
		count++
	}
	if err = a.removeDeletionManifest(dir); err != nil {
		return count, err
	}
	if err = a.Store.Delete("deletion_cleanup", staged.ID); err != nil {
		return count, err
	}
	// Only remove empty parent directories after individual file deletion.
	// Shared render assets leave their containing directories intact.
	for _, file := range staged.Files {
		path := filepath.Dir(filepath.Join(a.Data, file.Original))
		for within(a.Data, path) && path != a.Data && filepath.Dir(path) != a.Data {
			if _, err := a.checkedMediaDeletionPath(path); err != nil {
				break
			}
			if err := os.Remove(path); err != nil {
				break
			}
			path = filepath.Dir(path)
		}
	}
	return count, nil
}

// Called once during startup before workers can claim jobs. The same method
// can safely be called again to retry committed cleanup after a file lock ends.
func (a *App) recoverDeletionCleanup() error {
	root, err := a.checkedDeletionPath(filepath.Join(a.Data, ".deletions"))
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return errUnsafeDeletionPath
		}
		dir, err := a.deletionStageDirectory(entry.Name())
		if err != nil {
			return err
		}
		path, err := a.checkedDeletionPath(filepath.Join(dir, "manifest.json"))
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			// A crash can occur after mkdir and before writing the manifest.
			// Removing an empty directory is safe; occupied directories fail.
			if err = os.Remove(dir); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		var staged deletionStaging
		if json.Unmarshal(raw, &staged) != nil || staged.ID != entry.Name() {
			return errUnsafeDeletionPath
		}
		var committed deletionStaging
		err = a.Store.Get("deletion_cleanup", staged.ID, &committed)
		if errors.Is(err, store.ErrNotFound) {
			if err = a.rollbackDeletionFiles(&staged); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if _, err = a.finishDeletionFiles(&committed); err != nil {
				return err
			}
		}
	}
	// A crash after removing the journal directory but before the record delete
	// leaves an empty ledger entry. Finish it using the committed record too.
	rows, err := a.Store.List("deletion_cleanup")
	if err != nil {
		return err
	}
	items, err := store.Decode[deletionStaging](rows)
	if err != nil {
		return err
	}
	for i := range items {
		if _, err = a.finishDeletionFiles(&items[i]); err != nil {
			return err
		}
	}
	return nil
}
