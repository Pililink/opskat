package extension_svc

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opskat/opskat/internal/model/entity/extension_state_entity"
	"github.com/opskat/opskat/pkg/extension"
)

func TestActiveCommandReviewerRequiresLoadedReviewer(t *testing.T) {
	dir := t.TempDir()
	extDir := filepath.Join(dir, "rev")
	require.NoError(t, os.MkdirAll(extDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(extDir, "main.wasm"), []byte("wasm"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(extDir, "manifest.json"), []byte(`{"name":"rev"}`), 0o600))
	sum, err := extension.PackageSHA256(extDir, "main.wasm")
	require.NoError(t, err)

	mgr := extension.NewManager(dir, func(string) extension.HostProvider { return nil }, nopLogger())
	mgr.PutForTest(&extension.Extension{
		Name: "rev",
		Dir:  extDir,
		Manifest: &extension.Manifest{
			Name:    "rev",
			Version: "1.0.0",
			Backend: extension.ManifestBackend{Binary: "main.wasm"},
			Capabilities: extension.Capabilities{CommandReview: true},
			Reviewer: &extension.ReviewerDef{
				Questions:     []extension.ReviewQuestion{{ID: "destructive", I18n: extension.I18nNames{Name: map[string]string{"en": "Destroys data"}}}},
				MaxItems:      4,
				MaxCommandLen: 100,
			},
		},
		Plugin: &extension.Plugin{},
	})
	repo := &memStateRepo{rows: map[string]*extension_state_entity.ExtensionState{
		"rev": {Name: "rev", Enabled: true, Source: InstallOriginStore, PackageSHA256: sum},
	}}
	svc := &Service{manager: mgr, stateRepo: repo, logger: nopLogger()}

	got, err := svc.ActiveCommandReviewer(context.Background(), "rev")
	require.NoError(t, err)
	require.Equal(t, "rev", got.Name)
	require.Equal(t, 4, got.MaxItems)

	repo.rows["rev"].PackageSHA256 = "dead"
	_, err = svc.ActiveCommandReviewer(context.Background(), "rev")
	require.Error(t, err)
}
