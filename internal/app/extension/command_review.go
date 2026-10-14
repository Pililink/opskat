package extension

import (
	"fmt"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/opskat/opskat/internal/bootstrap"
	"github.com/opskat/opskat/internal/service/credential_svc"
	"github.com/opskat/opskat/internal/service/extension_svc"
)

// ListCommandReviewers 列出已加载的审核扩展及其能否使用，供设置页选择生效的审核器。
func (e *Extension) ListCommandReviewers() ([]extension_svc.CommandReviewerInfo, error) {
	if e.service == nil {
		return nil, fmt.Errorf("extension system not initialized")
	}
	return e.service.ListCommandReviewers(e.ctx, e.lang.Lang()), nil
}

// GetCommandReviewer 返回当前选中的审核扩展名；空表示用内置审核。
func (e *Extension) GetCommandReviewer() string {
	return bootstrap.GetConfig().CommandReviewExtension
}

// SetCommandReviewer 选择生效的审核扩展；name 为空表示改回内置审核。
// 只能选现在能用的审核扩展：选一个用不了的，之后每次审核都会失败。
func (e *Extension) SetCommandReviewer(name string) error {
	if name != "" {
		reviewers, err := e.ListCommandReviewers()
		if err != nil {
			return err
		}
		if err := checkSelectableReviewer(reviewers, name); err != nil {
			return err
		}
	}
	cfg := bootstrap.GetConfig()
	cfg.CommandReviewExtension = name
	if err := bootstrap.SaveConfig(cfg); err != nil {
		logger.Ctx(e.ctx).Error("save command reviewer failed", zap.String("extension", name), zap.Error(err))
		return err
	}
	logger.Ctx(e.ctx).Info("command reviewer selected", zap.String("extension", name))
	return nil
}

func checkSelectableReviewer(reviewers []extension_svc.CommandReviewerInfo, name string) error {
	for _, r := range reviewers {
		if r.Name != name {
			continue
		}
		if !r.Usable {
			return fmt.Errorf("审核扩展 %s 现在不能使用：%s", name, r.Reason)
		}
		return nil
	}
	return fmt.Errorf("%s 不是已加载的审核扩展", name)
}

// GetExtensionSettings 返回扩展设置表单：schema、非密钥字段的值和已设置的密钥字段。
func (e *Extension) GetExtensionSettings(name string) (*extension_svc.ExtensionSettingsView, error) {
	if e.service == nil {
		return nil, fmt.Errorf("extension system not initialized")
	}
	return e.service.SettingsView(e.ctx, name, e.lang.Lang())
}

// SaveExtensionSettings 保存扩展设置，密钥字段加密后入库；密钥字段留空表示不修改。
func (e *Extension) SaveExtensionSettings(name string, values map[string]string) error {
	if e.service == nil {
		return fmt.Errorf("extension system not initialized")
	}
	if err := e.service.SaveExtensionSettings(e.ctx, name, values, credential_svc.Default().Encrypt); err != nil {
		logger.Ctx(e.ctx).Warn("save extension settings failed", zap.String("extension", name), zap.Error(err))
		return err
	}
	logger.Ctx(e.ctx).Info("extension settings saved", zap.String("extension", name))
	return nil
}
