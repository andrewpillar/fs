package fs

import (
	"bytes"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

type (
	File      = fs.File
	FileInfo  = fs.FileInfo
	FileMode  = fs.FileMode
	PathError = fs.PathError
)

var (
	ErrInvalid    = fs.ErrInvalid
	ErrPermission = fs.ErrPermission
	ErrExist      = fs.ErrExist
	ErrNotExist   = fs.ErrNotExist
	ErrClosed     = fs.ErrClosed
)

// FS provides access to a hierarchical filesystem.
//
// The interface provides an implementation of the fs.FS, fs.SubFS,
// and fs.StatFS interfaces from io/fs.
type FS interface {
	// Open opens the named file. This should return *PathError with the Op set
	// to "open" on any errors.
	Open(name string) (File, error)

	// Sub returns an FS with the given name.
	Sub(dir string) (FS, error)

	// Stat returns the FileInfo for the named file.
	Stat(name string) (FileInfo, error)

	// Put puts the given file into the underlying filesystem. This should
	// return the file as it is stored in the underlying filesystem. This should
	// also return the file with the offset set to the beginning.
	Put(f File) (File, error)

	// Remove removes the named file from the filesystem.
	Remove(name string) error
}

type file struct {
	name    string
	off     int64
	data    []byte
	modTime time.Time
}

func (f *file) Stat() (FileInfo, error) { return f, nil }

func (f *file) Read(p []byte) (int, error) {
	if f.off < 0 {
		return 0, &PathError{Op: "read", Path: f.name, Err: ErrInvalid}
	}
	if f.off >= int64(len(f.data)) {
		return 0, io.EOF
	}

	n := copy(p, f.data[f.off:])
	f.off += int64(n)

	return n, nil
}

func (f *file) Close() error       { return nil }
func (f *file) Name() string       { return f.name }
func (f *file) Size() int64        { return int64(len(f.data)) }
func (f *file) Mode() FileMode     { return FileMode(0400) }
func (f *file) ModTime() time.Time { return f.modTime }
func (f *file) IsDir() bool        { return false }
func (f *file) Sys() any           { return nil }

type openFile struct {
	File

	name string
	info FileInfo
}

func (f *openFile) Name() string { return f.name }

func (f *openFile) Stat() (FileInfo, error) {
	info, err := f.File.Stat()

	if err != nil {
		return nil, err
	}

	f.info = info
	return f, nil
}

func (f *openFile) Size() int64        { return f.info.Size() }
func (f *openFile) Mode() FileMode     { return f.info.Mode() }
func (f *openFile) ModTime() time.Time { return f.info.ModTime() }
func (f *openFile) IsDir() bool        { return f.info.IsDir() }
func (f *openFile) Sys() any           { return f.info.Sys() }

// Rename returns a new File with the given name. Useful if you have already
// have something that implements File that you want to store in an FS as
// another name.
func Rename(f File, name string) File {
	return &openFile{
		File: f,
		name: name,
	}
}

// ReadFileMax reads the given reader into memory using at most maxMemory to
// store it and returns it as a File with the given name. If the number of
// bytes read from the reader exceeds maxMemory, then the contents is stored
// on disk instead of in memory.
func ReadFileMax(name string, r io.Reader, maxMemory int64) (File, error) {
	// Already exists on disk, so simply return it with the new name given.
	if f, ok := r.(*os.File); ok {
		return Rename(f, name), nil
	}

	var buf bytes.Buffer

	n, err := io.CopyN(&buf, r, maxMemory+1)

	if err != nil {
		if !errors.Is(err, io.EOF) {
			return nil, err
		}
	}

	if n > maxMemory {
		dir, err := os.MkdirTemp("", "fs-file-*")

		if err != nil {
			return nil, err
		}

		f, err := os.Create(filepath.Join(dir, name))

		if err != nil {
			return nil, err
		}

		if _, err := io.Copy(f, io.MultiReader(&buf, r)); err != nil {
			return nil, err
		}

		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		return f, nil
	}

	return &file{
		name:    name,
		data:    buf.Bytes(),
		modTime: time.Now(),
	}, nil
}

// ReadFile functions the same as ReadFileMax only using a default maxMemory of
// 32MB.
func ReadFile(name string, r io.Reader) (File, error) {
	return ReadFileMax(name, r, 32<<20)
}

var reTmpDir = regexp.MustCompile(filepath.Join(os.TempDir(), "fs-file-(.+)"))

// Cleanup deletes the given file if it exists on disk and is stored in the
// temporary directory. This would typically be deferred after a prior call to
// ReadFile.
func Cleanup(f File) error {
	if f, ok := f.(*os.File); ok {
		dir := filepath.Dir(f.Name())

		if reTmpDir.Match([]byte(dir)) {
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
		}
	}
	return nil
}

type filesystem struct {
	dir string
}

// New returns a new FS for the operating system's filesystem.
func New(dir string) FS {
	return filesystem{
		dir: dir,
	}
}

func (s filesystem) path(name string) string {
	return filepath.Join(s.dir, name)
}

func (s filesystem) Open(name string) (File, error) {
	name = s.path(name)

	f, err := os.Open(name)

	if err != nil {
		return nil, &PathError{Op: "open", Path: name, Err: errors.Unwrap(err)}
	}
	return f, nil
}

func (s filesystem) Sub(dir string) (FS, error) {
	subdir := s.path(dir)

	if err := os.MkdirAll(subdir, FileMode(0750)); err != nil {
		return nil, &PathError{Op: "sub", Path: dir, Err: errors.Unwrap(err)}
	}
	return New(subdir), nil
}

func (s filesystem) Stat(name string) (FileInfo, error) {
	info, err := os.Stat(s.path(name))

	if err != nil {
		return nil, &PathError{Op: "stat", Path: name, Err: errors.Unwrap(err)}
	}
	return info, nil
}

func (s filesystem) Put(f File) (File, error) {
	info, err := f.Stat()

	if err != nil {
		return nil, err
	}

	name := info.Name()

	dst, err := os.Create(s.path(name))

	if err != nil {
		return nil, &PathError{Op: "put", Path: name, Err: errors.Unwrap(err)}
	}

	if _, err := io.Copy(dst, f); err != nil {
		return nil, &PathError{Op: "put", Path: name, Err: errors.Unwrap(err)}
	}

	if _, err := dst.Seek(0, io.SeekStart); err != nil {
		return nil, &PathError{Op: "put", Path: name, Err: errors.Unwrap(err)}
	}
	return dst, nil
}

func (s filesystem) Remove(name string) error {
	if err := os.Remove(s.path(name)); err != nil {
		return &PathError{Op: "remove", Path: name, Err: errors.Unwrap(err)}
	}
	return nil
}

type nullFS struct{}

// Null returns a store that returns empty files. Useful for testing.
func Null() FS {
	return nullFS{}
}

func (s nullFS) Open(name string) (File, error) {
	return &file{
		name:    name,
		modTime: time.Now(),
	}, nil
}

func (s nullFS) Sub(dir string) (FS, error) {
	return s, nil
}

func (s nullFS) Stat(name string) (FileInfo, error) {
	return &file{
		name:    name,
		modTime: time.Now(),
	}, nil
}

func (s nullFS) Put(f File) (File, error) {
	info, err := f.Stat()

	if err != nil {
		return nil, err
	}

	return &file{
		name:    info.Name(),
		modTime: info.ModTime(),
	}, nil
}

func (nullFS) Remove(string) error { return nil }

type uniqueFS struct {
	FS
}

// Unique returns a filesystem that will error with ErrExist when multiple files
// with the same name are stored in it.
func Unique(s FS) FS {
	return uniqueFS{
		FS: s,
	}
}

func (s uniqueFS) Sub(dir string) (FS, error) {
	fs, err := s.FS.Sub(dir)

	if err != nil {
		return nil, err
	}
	return Unique(fs), nil
}

func (s uniqueFS) Put(f File) (File, error) {
	info, err := f.Stat()

	if err != nil {
		return nil, err
	}

	_, err = s.Stat(info.Name())

	if errors.Is(err, ErrNotExist) {
		return s.FS.Put(f)
	}

	if err != nil {
		return nil, err
	}
	return nil, ErrExist
}

type hashFS struct {
	FS

	mech func() hash.Hash
}

// Hash returns a filesystem that stores each file put in it against the hashed
// contents of the file with the given hashing mechanism. The file returned will
// be renamed to the content hash.
func Hash(s FS, mech func() hash.Hash) FS {
	return &hashFS{
		FS:   s,
		mech: mech,
	}
}

func (s *hashFS) Sub(dir string) (FS, error) {
	fs, err := s.FS.Sub(dir)

	if err != nil {
		return nil, err
	}
	return Hash(fs, s.mech), nil
}

func (s *hashFS) Put(f File) (File, error) {
	info, err := f.Stat()

	if err != nil {
		return nil, err
	}

	name := info.Name()
	h := s.mech()

	tmp, err := ReadFile("hash.Put", io.TeeReader(f, h))

	if err != nil {
		return nil, &PathError{Op: "put", Path: name, Err: errors.Unwrap(err)}
	}

	defer Cleanup(tmp)

	hash := hex.EncodeToString(h.Sum(nil))

	return s.FS.Put(Rename(tmp, hash))
}

type limit struct {
	FS

	limit int64
}

type SizeError struct {
	Size int64
}

func humanSize(n int64) string {
	units := [...]string{"B", "KB", "MB", "GB", "TB", "PB"}
	i := 0

	for ; n >= 1024; i++ {
		n /= 1024
	}
	return strconv.Itoa(int(n)) + " " + units[i]
}

func (e SizeError) Error() string {
	size := humanSize(e.Size)

	return "file too large, cannot exceed " + size
}

// Limit returns a filesystem that limits the size of files put in it to the
// given limit. If any file that is put in the filesystem exceeds the limit then
// SizeError is returned in the *PathError.
func Limit(s FS, n int64) FS {
	return limit{
		FS:    s,
		limit: n,
	}
}

func (s limit) Sub(dir string) (FS, error) {
	fs, err := s.FS.Sub(dir)

	if err != nil {
		return nil, err
	}
	return Limit(fs, s.limit), nil
}

func (s limit) Put(f File) (File, error) {
	info, err := f.Stat()

	if err != nil {
		return nil, err
	}

	if info.Size() > s.limit {
		return nil, &PathError{
			Op:   "put",
			Path: info.Name(),
			Err:  SizeError{Size: s.limit},
		}
	}
	return s.FS.Put(f)
}

type writeOnly struct {
	FS
}

// WriteOnly returns a filesystem that can only have files put in it. Any
// attempt to read a file via Open or Stat, or to modify a file via Remove
// will return ErrPermission in the *PathError.
func WriteOnly(s FS) FS {
	return writeOnly{
		FS: s,
	}
}

func (s writeOnly) Open(name string) (File, error) {
	return nil, &PathError{Op: "open", Path: name, Err: ErrPermission}
}

func (s writeOnly) Sub(dir string) (FS, error) {
	sub, err := s.FS.Sub(dir)

	if err != nil {
		return nil, err
	}
	return WriteOnly(sub), nil
}

func (s writeOnly) Stat(name string) (FileInfo, error) {
	return nil, &PathError{Op: "stat", Path: name, Err: ErrPermission}
}

func (s writeOnly) Remove(name string) error {
	return &PathError{Op: "remove", Path: name, Err: ErrPermission}
}

type readOnly struct {
	FS
}

// ReadOnly returns a filesystem that can only have a file read from it. Any
// attempt to write a file via Put or modify a file via Remove will return
// ErrPermission in the *PathError.
func ReadOnly(s FS) FS {
	return readOnly{
		FS: s,
	}
}

func (s readOnly) Sub(dir string) (FS, error) {
	fs, err := s.FS.Sub(dir)

	if err != nil {
		return nil, err
	}
	return ReadOnly(fs), nil
}

func (s readOnly) Put(f File) (File, error) {
	info, err := f.Stat()

	if err != nil {
		return nil, err
	}
	return nil, &PathError{Op: "put", Path: info.Name(), Err: ErrPermission}
}

func (s readOnly) Remove(name string) error {
	return &PathError{Op: "remove", Path: name, Err: ErrPermission}
}
