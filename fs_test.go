package fs

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func generateData(t *testing.T, n int) []byte {
	buf := make([]byte, n)

	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	return buf
}

func tmpdir(t *testing.T) string {
	dir, err := os.MkdirTemp("", t.Name())

	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func Test_ReadFile(t *testing.T) {
	buf := generateData(t, 50<<20)

	f, err := ReadFile(t.Name(), bytes.NewReader(buf))

	if err != nil {
		t.Fatal(err)
	}

	osf, ok := f.(*os.File)

	if !ok {
		t.Fatalf("unexpected type, expected=%T, got=%T\n", &os.File{}, f)
	}

	dir := filepath.Dir(osf.Name())

	if err := Cleanup(f); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(dir); err != nil {
		if !errors.Is(err, ErrNotExist) {
			t.Fatalf("unexpected error, expected=%q, got=%T(%q)\n", ErrNotExist, err, err)
		}
	}
}

func Test_Hash(t *testing.T) {
	sizes := [...]int{
		32 << 20,
		50 << 20,
	}

	dir := tmpdir(t)
	defer os.RemoveAll(dir)

	store := Hash(New(dir), sha256.New)

	for i, size := range sizes {
		func(i, size int) {
			buf := generateData(t, size)
			h := sha256.New()

			f, err := ReadFile(t.Name(), io.TeeReader(bytes.NewReader(buf), h))

			if err != nil {
				t.Fatal(err)
			}

			defer Cleanup(f)

			expected := hex.EncodeToString(h.Sum(nil))

			hashed, err := store.Put(f)

			if err != nil {
				t.Fatal(err)
			}

			info, err := hashed.Stat()

			if err != nil {
				t.Fatal(err)
			}

			if info.Name() != expected {
				t.Fatalf("tests[%d] - unexpected name, expected=%q, got=%q\n", i, expected, info.Name())
			}
		}(i, size)
	}
}

func Test_Limit(t *testing.T) {
	dir := tmpdir(t)
	defer os.RemoveAll(dir)

	store := Limit(New(dir), 32<<20)

	buf := generateData(t, 50<<20)

	f, err := ReadFile(t.Name(), bytes.NewReader(buf))

	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Put(f); err != nil {
		expected := SizeError{Size: 32 << 20}
		err = errors.Unwrap(err)

		if !errors.Is(err, expected) {
			t.Fatalf("unexpected error, expected=%T, got=%T(%q)\n", expected, err, err)
		}
		return
	}
	t.Fatal("expected LimitStore.Put to error, it did not")
}

func Test_WriteOnly(t *testing.T) {
	dir := tmpdir(t)
	defer os.RemoveAll(dir)

	store := WriteOnly(New(dir))

	buf := generateData(t, 32<<20)

	f, err := ReadFile(t.Name(), bytes.NewReader(buf))

	if err != nil {
		t.Fatal(err)
	}

	info, err := f.Stat()

	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Put(f); err != nil {
		t.Fatal(err)
	}

	funcs := [...]func(string) error{
		func(name string) error {
			_, err := store.Open(name)
			return err
		},
		func(name string) error {
			_, err := store.Stat(name)
			return err
		},
		store.Remove,
	}

	name := info.Name()

	for _, fn := range funcs {
		if err := fn(name); err != nil {
			err = errors.Unwrap(err)

			if !errors.Is(err, ErrPermission) {
				t.Fatalf("unexpected error, expected=%T, got=%T(%q)\n", ErrPermission, err, err)
			}
			continue
		}
		t.Fatal("expected function to error, it did not")
	}

	store, err = store.Sub("subdir")

	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Open(name); err != nil {
		err = errors.Unwrap(err)

		if !errors.Is(err, ErrPermission) {
			t.Fatalf("unexpected error, expected=%T, got=%T(%q)\n", ErrPermission, err, err)
		}
	}
}

func Test_ReadOnly(t *testing.T) {
	dir := tmpdir(t)
	defer os.RemoveAll(dir)

	store := ReadOnly(New(dir))

	buf := generateData(t, 32<<20)

	f, err := ReadFile(t.Name(), bytes.NewReader(buf))

	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Put(f); err != nil {
		err = errors.Unwrap(err)

		if !errors.Is(err, ErrPermission) {
			t.Fatalf("unexpected error, expected=%T, got=%T(%q)\n", ErrPermission, err, err)
		}
		return
	}
	t.Fatal("expected ReadOnlyStore.Put to error, it did not")
}

func Test_Unique(t *testing.T) {
	dir := tmpdir(t)
	defer os.RemoveAll(dir)

	store := Unique(New(dir))

	buf := generateData(t, 32<<20)

	f, err := ReadFile(t.Name(), bytes.NewReader(buf))

	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Put(f); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Put(f); err != nil {
		if !errors.Is(err, ErrExist) {
			t.Fatalf("unexpected error, expected=%q, got=%q\n", ErrExist, err)
		}
		return
	}
	t.Fatal("expected subsequent call to store.Put to error, it did not")
}
