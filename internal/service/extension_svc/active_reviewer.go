package extension_svc

import (
	"context"
	"fmt"

	"github.com/opskat/opskat/pkg/extension"
)

// ActiveReviewer 是当前选中、且通过来源与摘要检查的审核器。
type ActiveReviewer struct {
	Name          string
	Version       string
	SettingsHash  string
	MaxItems      int
	MaxCommandLen int
	Questions     []string
	Plugin        *extension.Plugin
}

// ActiveCommandReviewer 返回名为 name 的已加载审核器。
// 不是审核器、未加载、或来源/摘要不合格时返回错误。name 为空表示没有选中。
func (s *Service) ActiveCommandReviewer(ctx context.Context, name string) (*ActiveReviewer, error) {
	if name == "" {
		return nil, fmt.Errorf("no command reviewer selected")
	}
	ext := s.manager.GetExtension(name)
	if ext == nil || ext.Plugin == nil || !ext.Manifest.IsCommandReviewer() {
		return nil, fmt.Errorf("extension %q is not a loaded command reviewer", name)
	}
	if err := s.CheckReviewerProvenance(ctx, name, ext.Dir, ext.Manifest.Backend.Binary); err != nil {
		return nil, err
	}
	hash, err := s.SettingsHash(ctx, name)
	if err != nil {
		return nil, err
	}
	rev := ext.Manifest.Reviewer
	ids := make([]string, len(rev.Questions))
	for i, q := range rev.Questions {
		ids[i] = q.ID
	}
	return &ActiveReviewer{
		Name:          ext.Name,
		Version:       ext.Manifest.Version,
		SettingsHash:  hash,
		MaxItems:      rev.MaxItems,
		MaxCommandLen: rev.MaxCommandLen,
		Questions:     ids,
		Plugin:        ext.Plugin,
	}, nil
}
