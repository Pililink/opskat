package system

import (
	"fmt"
	"strings"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/opskat/opskat/internal/bootstrap"
	"github.com/opskat/opskat/internal/service/command_review_svc"
	"github.com/opskat/opskat/internal/service/credential_svc"
)

// CommandReviewSettings 是设置页读到的模型审核设置（已套上默认值）。API key 只回是否已设置。
type CommandReviewSettings struct {
	APIKeySet bool    `json:"apiKeySet"`
	Model     string  `json:"model"`
	TimeoutMs int     `json:"timeoutMs"`
	Threshold float64 `json:"threshold"`
}

// CommandReviewSaveInput 是保存模型审核设置的入参。数值填 0、模型填空表示用默认值。
type CommandReviewSaveInput struct {
	APIKey      string  `json:"apiKey"` // 非空时替换已保存的 key
	ClearAPIKey bool    `json:"clearApiKey"`
	Model       string  `json:"model"`
	TimeoutMs   int     `json:"timeoutMs"`
	Threshold   float64 `json:"threshold"`
}

func (in CommandReviewSaveInput) validate() error {
	model := strings.TrimSpace(in.Model)
	if model == "jev-latest" || model == "jev-preview" {
		return fmt.Errorf("请填写具体的模型版本（如 %s），别名会随新版本发布自动变化", command_review_svc.DefaultModel)
	}
	if in.TimeoutMs != 0 && (in.TimeoutMs < 1000 || in.TimeoutMs > 60000) {
		return fmt.Errorf("超时需在 1000~60000 毫秒之间")
	}
	if in.Threshold < 0 || in.Threshold >= 1 {
		return fmt.Errorf("阈值需在 0~1 之间")
	}
	return nil
}

// GetCommandReviewSettings 读取模型审核设置。
func (s *System) GetCommandReviewSettings() (*CommandReviewSettings, error) {
	cfg := bootstrap.CommandReviewConfig()
	return &CommandReviewSettings{
		APIKeySet: cfg.APIKey != "",
		Model:     cfg.Model,
		TimeoutMs: int(cfg.Timeout.Milliseconds()),
		Threshold: cfg.Threshold,
	}, nil
}

// SaveCommandReviewSettings 保存模型审核设置，API key 加密后落盘。
func (s *System) SaveCommandReviewSettings(in CommandReviewSaveInput) error {
	ctx := s.desktopCtx()
	if err := in.validate(); err != nil {
		return err
	}
	cfg := bootstrap.GetConfig()
	switch {
	case in.ClearAPIKey:
		cfg.CommandReviewAPIKey = ""
	case in.APIKey != "":
		encrypted, err := credential_svc.Default().Encrypt(strings.TrimSpace(in.APIKey))
		if err != nil {
			return fmt.Errorf("加密 TypeSafe API key 失败: %w", err)
		}
		cfg.CommandReviewAPIKey = encrypted
	}
	cfg.CommandReviewModel = strings.TrimSpace(in.Model)
	cfg.CommandReviewTimeoutMs = in.TimeoutMs
	cfg.CommandReviewThreshold = in.Threshold
	if err := bootstrap.SaveConfig(cfg); err != nil {
		logger.Ctx(ctx).Error("save command review settings failed", zap.Error(err))
		return err
	}
	logger.Ctx(ctx).Info("command review settings saved",
		zap.Bool("apiKeySet", cfg.CommandReviewAPIKey != ""), zap.String("model", cfg.CommandReviewModel))
	return nil
}

// TestCommandReview 用当前设置发一次最小请求，检查 API key 和网络。
func (s *System) TestCommandReview() error {
	ctx := s.desktopCtx()
	logger.Ctx(ctx).Info("test command review started")
	if err := command_review_svc.Default().TestConnection(ctx); err != nil {
		logger.Ctx(ctx).Warn("test command review failed", zap.Error(err))
		return err
	}
	logger.Ctx(ctx).Info("test command review succeeded")
	return nil
}
