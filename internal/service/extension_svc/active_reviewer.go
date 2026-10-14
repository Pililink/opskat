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
	Questions     []extension.ReviewQuestion
	Plugin        *extension.Plugin
}

// ActiveCommandReviewer 返回名为 name 的已加载审核器。
// 不是审核器、未加载、或来源/摘要不合格时返回错误。name 为空表示没有选中。
func (s *Service) ActiveCommandReviewer(ctx context.Context, name string) (*ActiveReviewer, error) {
	if name == "" {
		return nil, fmt.Errorf("no command reviewer selected")
	}
	ext := s.manager.GetExtension(name)
	if ext == nil && s.reviewerLoader != nil {
		loaded, err := s.loadReviewer(ctx, name)
		if err != nil {
			return nil, err
		}
		ext = loaded
	}
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
	return &ActiveReviewer{
		Name:          ext.Name,
		Version:       ext.Manifest.Version,
		SettingsHash:  hash,
		MaxItems:      rev.MaxItems,
		MaxCommandLen: rev.MaxCommandLen,
		Questions:     rev.Questions,
		Plugin:        ext.Plugin,
	}, nil
}

// LoadReviewersOnDemand 让 ActiveCommandReviewer 在选中的审核器还没加载时自己加载它。
// 桌面端启动时已加载全部启用的扩展，不用这个；opsctl 平时只按缓存的 describe() 注册扩展，
// 只在真正要审核时才编译、实例化选中的那一个。
func (s *Service) LoadReviewersOnDemand() {
	s.reviewerLoader = func(ctx context.Context, dir string) error {
		_, err := s.manager.LoadExtension(ctx, dir)
		return err
	}
}

// loadReviewer 先确认扩展已启用、包摘要和来源合格，再运行它的代码。
func (s *Service) loadReviewer(ctx context.Context, name string) (*extension.Extension, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ext := s.manager.GetExtension(name); ext != nil {
		return ext, nil
	}
	state, err := s.stateRepo.Find(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("command reviewer %q has no install record: %w", name, err)
	}
	if !state.Enabled {
		return nil, fmt.Errorf("command reviewer %q is disabled", name)
	}
	dir := s.manager.ExtDir(name)
	info, err := extension.LoadManifestInfo(dir)
	if err != nil {
		return nil, fmt.Errorf("read command reviewer %q: %w", name, err)
	}
	if err := s.CheckReviewerProvenance(ctx, name, dir, info.Manifest.Backend.Binary); err != nil {
		return nil, err
	}
	if err := s.reviewerLoader(ctx, dir); err != nil {
		return nil, fmt.Errorf("load command reviewer %q: %w", name, err)
	}
	return s.manager.GetExtension(name), nil
}

// CommandReviewerInfo 是设置页列出的一个审核扩展。
type CommandReviewerInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Version     string `json:"version"`
	// Usable 表示现在能当审核器；不能时 Reason 说明原因（已停用、包摘要不符、正式版只认商店安装等）。
	Usable bool   `json:"usable"`
	Reason string `json:"reason,omitempty"`
	// Signed 表示从官方商店安装、签名已校验；非正式版上本地安装的审核器可用但不是签名的。
	Signed bool `json:"signed"`
}

// ListCommandReviewers 列出已加载的审核扩展及其能否使用，供设置页选择。
func (s *Service) ListCommandReviewers(ctx context.Context, lang string) []CommandReviewerInfo {
	var out []CommandReviewerInfo
	for _, ext := range s.manager.ListExtensions() {
		if !ext.Manifest.IsCommandReviewer() {
			continue
		}
		localized := ext.Manifest.Localized(func(key string) string { return ext.Translate(lang, key) })
		info := CommandReviewerInfo{Name: ext.Name, DisplayName: localized.I18n.DisplayName, Version: ext.Manifest.Version}
		signed, err := s.reviewerProvenance(ctx, ext.Name, ext.Dir, ext.Manifest.Backend.Binary)
		if err != nil {
			info.Reason = err.Error()
		} else {
			info.Usable, info.Signed = true, signed
		}
		out = append(out, info)
	}
	return out
}
