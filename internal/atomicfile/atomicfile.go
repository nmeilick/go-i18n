package atomicfile

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// WriteFile atomically replaces path with data. The temporary file is created
// in the target directory so the final rename stays on the same filesystem.
func WriteFile(path string, data []byte, perm fs.FileMode) error {
	return WriteFiles(map[string][]byte{path: data}, perm)
}

// WriteFileIfChanged atomically replaces path only when bytes differ.
func WriteFileIfChanged(path string, data []byte, perm fs.FileMode) error {
	same, err := FileContentEqual(path, data)
	if err == nil && same {
		return nil
	}
	return WriteFile(path, data, perm)
}

// FileContentEqual reports whether path exists and has exactly data's content.
// It compares size first, then streams the existing file in fixed-size chunks.
func FileContentEqual(path string, data []byte) (bool, error) {
	// #nosec G304 -- this helper intentionally compares a caller-selected local file.
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() != int64(len(data)) {
		return false, nil
	}
	r := bytes.NewReader(data)
	bufFile := make([]byte, 32*1024)
	bufData := make([]byte, 32*1024)
	for {
		nf, ef := f.Read(bufFile)
		nd, ed := r.Read(bufData)
		if nf != nd || !bytes.Equal(bufFile[:nf], bufData[:nd]) {
			return false, nil
		}
		if errors.Is(ef, io.EOF) && errors.Is(ed, io.EOF) {
			return true, nil
		}
		if ef != nil && !errors.Is(ef, io.EOF) {
			return false, ef
		}
		if ed != nil && !errors.Is(ed, io.EOF) {
			return false, ed
		}
	}
}

// WriteFiles stages all target files before renaming any of them into place.
func WriteFiles(files map[string][]byte, perm fs.FileMode) error {
	if len(files) == 0 {
		return nil
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	type staged struct {
		tmp  string
		path string
	}
	stagedFiles := make([]staged, 0, len(paths))
	cleanup := true
	defer func() {
		if !cleanup {
			return
		}
		for _, file := range stagedFiles {
			_ = os.Remove(file.tmp)
		}
	}()
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return err
		}
		tmp, err := writeTemp(filepath.Dir(path), filepath.Base(path), files[path], perm)
		if err != nil {
			return err
		}
		stagedFiles = append(stagedFiles, staged{tmp: tmp, path: path})
	}
	for _, file := range stagedFiles {
		if err := os.Rename(file.tmp, file.path); err != nil {
			return err
		}
		_ = syncDir(filepath.Dir(file.path))
	}
	cleanup = false
	return nil
}

func writeTemp(dir, base string, data []byte, perm fs.FileMode) (string, error) {
	f, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	defer func() {
		_ = f.Close()
	}()
	if err := f.Chmod(perm); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err := f.Sync(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func syncDir(dir string) error {
	// #nosec G304 -- syncing the target directory is part of atomic local file replacement.
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return err
	}
	return nil
}
