package sftp

import (
	"errors"
	"io"

	"github.com/andrewpillar/fs"

	"github.com/pkg/sftp"
)

type FS struct {
	cli *sftp.Client
	dir string
}

var _ fs.FS = (*FS)(nil)

// New returns a new FS for storing files over an SFTP connection.
func New(cli *sftp.Client, dir string) *FS {
	return &FS{
		cli: cli,
		dir: dir,
	}
}

func (s *FS) path(name string) string {
	return s.cli.Join(s.dir, name)
}

func (s *FS) Open(name string) (fs.File, error) {
	f, err := s.cli.Open(s.path(name))

	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: errors.Unwrap(err)}
	}
	return f, nil
}

func (s *FS) Sub(dir string) (fs.FS, error) {
	subdir := s.path(dir)

	if err := s.cli.MkdirAll(subdir); err != nil {
		return nil, &fs.PathError{Op: "sub", Path: dir, Err: errors.Unwrap(err)}
	}
	return New(s.cli, subdir), nil
}

func (s *FS) Stat(name string) (fs.FileInfo, error) {
	info, err := s.cli.Stat(s.path(name))

	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: errors.Unwrap(err)}
	}
	return info, nil
}

func (s *FS) Put(f fs.File) (fs.File, error) {
	info, err := f.Stat()

	if err != nil {
		return nil, err
	}

	name := info.Name()

	dst, err := s.cli.Create(s.path(name))

	if err != nil {
		return nil, &fs.PathError{Op: "put", Path: name, Err: errors.Unwrap(err)}
	}

	if _, err := io.Copy(dst, f); err != nil {
		return nil, &fs.PathError{Op: "put", Path: name, Err: errors.Unwrap(err)}
	}

	if _, err := dst.Seek(0, io.SeekStart); err != nil {
		return nil, &fs.PathError{Op: "put", Path: name, Err: errors.Unwrap(err)}
	}
	return dst, nil
}

func (s *FS) Remove(name string) error {
	if err := s.cli.Remove(s.path(name)); err != nil {
		return &fs.PathError{Op: "remove", Path: name, Err: errors.Unwrap(err)}
	}
	return nil
}
