package media

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStoreRoundTrip(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)

	payload := []byte("sanitized pixels")
	hash := Hash(payload)
	require.NoError(t, s.Put(hash, VariantFull, payload))

	body, _, err := s.Open(hash, VariantFull)
	require.NoError(t, err)
	defer body.Close()

	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, payload, got)
}

func TestStoreFansOutByHashPrefix(t *testing.T) {
	root := t.TempDir()
	s, err := NewStore(root)
	require.NoError(t, err)

	payload := []byte("x")
	hash := Hash(payload)
	require.NoError(t, s.Put(hash, VariantFull, payload))
	require.NoError(t, s.Put(hash, VariantThumb, []byte("thumb")))

	dir := filepath.Join(root, hash[0:2], hash[2:4])
	require.FileExists(t, filepath.Join(dir, hash))
	require.FileExists(t, filepath.Join(dir, hash+".thumb"))
}

func TestStorePutIsIdempotent(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)

	payload := []byte("same content, same hash")
	hash := Hash(payload)
	require.NoError(t, s.Put(hash, VariantFull, payload))
	require.NoError(t, s.Put(hash, VariantFull, payload))

	body, _, err := s.Open(hash, VariantFull)
	require.NoError(t, err)
	defer body.Close()
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, payload, got)
}

func TestStorePutRewritesThumbs(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)

	payload := []byte("same content, same hash")
	hash := Hash(payload)
	require.NoError(t, s.Put(hash, VariantThumb, []byte("old thumb")))
	require.NoError(t, s.Put(hash, VariantThumb, []byte("new thumb")))

	body, _, err := s.Open(hash, VariantThumb)
	require.NoError(t, err)
	defer body.Close()
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, []byte("new thumb"), got)
}

func TestStoreLeavesNoTempFilesBehind(t *testing.T) {
	root := t.TempDir()
	s, err := NewStore(root)
	require.NoError(t, err)

	hash := Hash([]byte("a"))
	require.NoError(t, s.Put(hash, VariantFull, []byte("a")))

	var temps int
	require.NoError(t, filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasPrefix(d.Name(), ".tmp-") {
			temps++
		}
		return nil
	}))
	require.Zero(t, temps)
}

func TestStoreRejectsKeysThatAreNotHashes(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)

	for _, key := range []string{
		"../../etc/passwd",
		strings.Repeat("a", hashLen-1),
		strings.Repeat("A", hashLen),
		strings.Repeat("z", hashLen),
		"",
	} {
		require.Error(t, s.Put(key, VariantFull, []byte("x")), "key %q", key)
		_, _, err := s.Open(key, VariantFull)
		require.Error(t, err, "key %q", key)
	}
}

func TestStoreRejectsUnknownVariant(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	require.Error(t, s.Put(Hash([]byte("a")), "preview", []byte("x")))
}

func TestStoreOpenMissingBlob(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)

	_, _, err = s.Open(Hash([]byte("never stored")), VariantFull)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestNewStoreRequiresADir(t *testing.T) {
	_, err := NewStore("")
	require.Error(t, err)
}
