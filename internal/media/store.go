package media

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	VariantFull  = ""
	VariantThumb = "thumb"

	hashLen = sha256.Size * 2
)

var ErrNotFound = errors.New("blob not found")

type Store struct {
	root string
}

func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func NewStore(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("media: upload dir is not configured")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("media: resolving upload dir: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("media: creating upload dir: %w", err)
	}
	return &Store{root: abs}, nil
}

func (s *Store) Put(hash, variant string, data []byte) error {
	path, err := s.path(hash, variant)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o640); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (s *Store) Open(hash, variant string) (io.ReadSeekCloser, time.Time, error) {
	path, err := s.path(hash, variant)
	if err != nil {
		return nil, time.Time{}, err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, time.Time{}, ErrNotFound
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, time.Time{}, err
	}
	return f, info.ModTime(), nil
}

func (s *Store) Remove(hash, variant string) error {
	path, err := s.path(hash, variant)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) path(hash, variant string) (string, error) {
	if !validHash(hash) {
		return "", fmt.Errorf("media: invalid blob key %q", hash)
	}
	name := hash
	switch variant {
	case VariantFull:
	case VariantThumb:
		name += ".thumb"
	default:
		return "", fmt.Errorf("media: unknown variant %q", variant)
	}
	return filepath.Join(s.root, hash[0:2], hash[2:4], name), nil
}

func validHash(h string) bool {
	if len(h) != hashLen {
		return false
	}
	for i := range len(h) {
		c := h[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
