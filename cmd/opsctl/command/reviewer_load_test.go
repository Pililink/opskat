package command

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadActiveReviewerLoadsOnlyTheSelectedExtension(t *testing.T) {
	dir := t.TempDir()
	loaded := map[string]int{}
	loader := func(_ context.Context, extDir string) error {
		loaded[filepath.Base(extDir)]++
		return nil
	}
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "rev"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "other"), 0o755))

	err := loadActiveReviewer(context.Background(), dir, "rev", loader)
	require.NoError(t, err)
	require.Equal(t, map[string]int{"rev": 1}, loaded, "只实例化选中的审核器")

	err = loadActiveReviewer(context.Background(), dir, "", loader)
	require.NoError(t, err)
	require.Equal(t, 1, loaded["rev"], "没选审核器时不加载任何扩展")

	err = loadActiveReviewer(context.Background(), dir, "missing", loader)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing")
}

func TestLoadActiveReviewerReportsLoaderError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "rev"), 0o755))
	err := loadActiveReviewer(context.Background(), dir, "rev", func(context.Context, string) error {
		return os.ErrPermission
	})
	require.Error(t, err)
	require.ErrorIs(t, err, os.ErrPermission)
}
