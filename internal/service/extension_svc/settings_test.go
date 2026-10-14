package extension_svc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opskat/opskat/pkg/extension"
)

type memDataRepo struct {
	rows map[string][]byte
}

func (m *memDataRepo) key(ext, k string) string { return ext + "\x00" + k }

func (m *memDataRepo) Get(_ context.Context, extName, key string) ([]byte, error) {
	v, ok := m.rows[m.key(extName, key)]
	if !ok {
		return nil, errNotFound
	}
	return append([]byte(nil), v...), nil
}

func (m *memDataRepo) Set(_ context.Context, extName, key string, value []byte) error {
	m.rows[m.key(extName, key)] = append([]byte(nil), value...)
	return nil
}

func (m *memDataRepo) Delete(context.Context, string, string) error { return nil }
func (m *memDataRepo) DeleteAll(context.Context, string) error      { return nil }

func settingsService(t *testing.T) *Service {
	t.Helper()
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"endpoint": map[string]any{"type": "string", "format": "endpoint"},
			"token":    map[string]any{"type": "string", "format": "password"},
			"model":    map[string]any{"type": "string"},
		},
		"required": []any{"endpoint"},
	}
	mgr := extension.NewManager(t.TempDir(), func(string) extension.HostProvider { return nil }, nopLogger())
	mgr.PutForTest(&extension.Extension{
		Name: "rev",
		Manifest: &extension.Manifest{
			Name:           "rev",
			SettingsSchema: schema,
		},
	})
	return &Service{
		manager:  mgr,
		dataRepo: &memDataRepo{rows: map[string][]byte{}},
		logger:   nopLogger(),
	}
}

func TestSaveExtensionSettingsEncryptsPasswords(t *testing.T) {
	svc := settingsService(t)
	err := svc.SaveExtensionSettings(context.Background(), "rev", map[string]string{
		"endpoint": "https://review.example.com",
		"token":    "s3cret",
		"model":    "m",
	}, func(plain string) (string, error) { return "enc:" + plain, nil })
	require.NoError(t, err)

	pub, err := svc.PublicSettings(context.Background(), "rev")
	require.NoError(t, err)
	assert.Equal(t, "https://review.example.com", pub["endpoint"])
	assert.Equal(t, "m", pub["model"])
	_, has := pub["token"]
	assert.False(t, has, "密钥字段不能出现在 settings_get")

	secrets, err := svc.SettingSecrets(context.Background(), "rev", []string{"token"}, func(v string) (string, error) {
		return v[len("enc:"):], nil
	})
	require.NoError(t, err)
	assert.Equal(t, "s3cret", secrets["token"])

	err = svc.SaveExtensionSettings(context.Background(), "rev", map[string]string{"model": "m"}, nil)
	assert.Error(t, err, "缺必填字段要失败")
}
