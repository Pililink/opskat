package extension_svc

import (
	"context"
	"errors"
	"fmt"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/opskat/opskat/internal/model/entity/extension_state_entity"
	"github.com/opskat/opskat/internal/pkg/appversion"
	"github.com/opskat/opskat/pkg/extension"
)

// ErrReviewerUnsigned 表示非正式版上本地或开发安装的审核器可用，但未经签名。
// 调用方据此在设置里标注，而不是当成不可用。
var ErrReviewerUnsigned = errors.New("command reviewer is installed locally and is not signed")

// reviewerEligible 按第 3 条判断这条安装记录能不能当审核器。
// 摘要必须与磁盘上的 WASM 一致。正式版只接受商店安装；nightly 与开发版接受本地和开发安装，
// 但返回 ErrReviewerUnsigned。来源为空（迁移前的旧记录）一律不合格。
func reviewerEligible(state *extension_state_entity.ExtensionState, wasmSHA256 string, app appversion.Info) error {
	if state == nil || state.PackageSHA256 == "" || state.PackageSHA256 != wasmSHA256 {
		return fmt.Errorf("command reviewer package digest does not match the installed wasm")
	}
	switch state.Source {
	case InstallOriginStore:
		return nil
	case InstallOriginLocal, InstallOriginDev:
		if app.Kind == appversion.KindRelease {
			return fmt.Errorf("command reviewer must be installed from the store on a release build (source %s)", state.Source)
		}
		return ErrReviewerUnsigned
	default:
		return fmt.Errorf("command reviewer has no install source")
	}
}

// recordProvenance 写入或覆盖安装来源与包摘要，同时设置启用状态。
func (s *Service) recordProvenance(ctx context.Context, name, source, sha256 string, enabled bool) {
	state, err := s.stateRepo.Find(ctx, name)
	if err != nil {
		if err := s.stateRepo.Create(ctx, &extension_state_entity.ExtensionState{
			Name: name, Enabled: enabled, Source: source, PackageSHA256: sha256,
		}); err != nil {
			s.logger.Warn("create extension state", zap.String("name", name), zap.Error(err))
		}
		return
	}
	state.Enabled = enabled
	state.Source = source
	state.PackageSHA256 = sha256
	if err := s.stateRepo.Update(ctx, state); err != nil {
		s.logger.Warn("update extension state", zap.String("name", name), zap.Error(err))
	}
}

// CheckReviewerProvenance 重算已安装扩展的 WASM 摘要，并按当前构建判断能否当审核器。
// 摘要不符或来源不合格时返回错误，调用方按审核失败处理。
func (s *Service) CheckReviewerProvenance(ctx context.Context, name, dir, binary string) error {
	sum, err := extension.PackageSHA256(dir, binary)
	if err != nil {
		return err
	}
	state, err := s.stateRepo.Find(ctx, name)
	if err != nil {
		return fmt.Errorf("command reviewer %q has no install record: %w", name, err)
	}
	if err := reviewerEligible(state, sum, currentApp()); err != nil {
		logger.Ctx(ctx).Warn("command reviewer provenance rejected",
			zap.String("extension", name), zap.String("source", state.Source), zap.Error(err))
		return err
	}
	return nil
}
