package bootstrap

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/opskat/opskat/internal/pkg/netdial"
	"github.com/opskat/opskat/internal/pkg/typesafe"
	"github.com/opskat/opskat/internal/repository/command_review_repo"
	"github.com/opskat/opskat/internal/service/command_review_svc"
	"github.com/opskat/opskat/internal/service/credential_svc"
	"github.com/opskat/opskat/internal/service/extension_svc"
	"github.com/opskat/opskat/pkg/extension"
)

// registerCommandReview 注册命令的模型审核服务。桌面端和 opsctl 都经 Init 走到这里，
// 两边用同一份设置和同一个数据库缓存。
func registerCommandReview() {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = netdial.Default().DialContext
	httpClient := &http.Client{Transport: transport}

	command_review_svc.Register(command_review_svc.New(
		CommandReviewConfig,
		command_review_svc.NewRepoCache(command_review_repo.CommandReview()),
		func(apiKey, baseURL string) command_review_svc.Evaluator {
			return typesafe.New(apiKey, typesafe.WithBaseURL(baseURL), typesafe.WithHTTPClient(httpClient))
		},
		activeExtensionReviewer,
	))
}

// extensionService 由扩展系统初始化后注入。没有注入时审核继续用内置 TypeSafe。
var extensionService *extension_svc.Service

// SetExtensionService 注入扩展服务，供审核查找当前生效的审核器。
func SetExtensionService(svc *extension_svc.Service) { extensionService = svc }

// activeExtensionReviewer 读取设置里选中的审核扩展。
// 没选时返回 (nil, nil)，审核用内置 TypeSafe。
// 选中了但未加载、不是审核器或来源/摘要不符时返回 ErrReviewerUnavailable，审核失败，不退回内置。
func activeExtensionReviewer(ctx context.Context) (command_review_svc.ExtensionReviewer, error) {
	name := GetConfig().CommandReviewExtension
	if name == "" {
		return nil, nil
	}
	if extensionService == nil {
		return nil, fmt.Errorf("%w: extension system is not initialized", command_review_svc.ErrReviewerUnavailable)
	}
	active, err := extensionService.ActiveCommandReviewer(ctx, name)
	if err != nil {
		logger.Ctx(ctx).Warn("active command reviewer unavailable", zap.String("extension", name), zap.Error(err))
		return nil, fmt.Errorf("%w: %s", command_review_svc.ErrReviewerUnavailable, err.Error())
	}
	return &loadedExtensionReviewer{active: active}, nil
}

type loadedExtensionReviewer struct {
	active *extension_svc.ActiveReviewer
}

func (r *loadedExtensionReviewer) Identity() (string, string, string) {
	return r.active.Name, r.active.Version, r.active.SettingsHash
}
func (r *loadedExtensionReviewer) Limits() (int, int) {
	return r.active.MaxItems, r.active.MaxCommandLen
}
func (r *loadedExtensionReviewer) Questions() []extension.ReviewQuestion {
	return r.active.Questions
}
func (r *loadedExtensionReviewer) Review(ctx context.Context, items []extension.ReviewItem) ([]extension.ReviewCommandResult, error) {
	return r.active.Plugin.ReviewCommands(ctx, items, 0)
}

// CommandReviewSettings 是 config.json 里的审核设置，不含 API key。
func CommandReviewSettings() command_review_svc.Settings {
	cfg := GetConfig()
	return command_review_svc.Settings{
		BaseURL:   cfg.CommandReviewBaseURL,
		Model:     cfg.CommandReviewModel,
		TimeoutMs: cfg.CommandReviewTimeoutMs,
		Threshold: cfg.CommandReviewThreshold,
	}
}

// CommandReviewConfig 是审核服务用的设置：config.json 里的设置加上解密后的 API key。
// 保存过的 API key 解不开时返回错误，内置审核按失败处理，不会因此放行；这时仍返回不含
// API key 的其余设置，审核扩展用不到这个 key，照常按其中的超时审核。
func CommandReviewConfig() (command_review_svc.Config, error) {
	s := CommandReviewSettings()
	key, err := CommandReviewAPIKey()
	if err != nil {
		return command_review_svc.NewConfig(s), err
	}
	s.APIKey = key
	return command_review_svc.NewConfig(s), nil
}

// CommandReviewAPIKey 返回解密后的审核 API key；没有配置时为空。
func CommandReviewAPIKey() (string, error) {
	encrypted := GetConfig().CommandReviewAPIKey
	if encrypted == "" {
		return "", nil
	}
	key, err := credential_svc.Default().Decrypt(encrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt command review api key: %w", err)
	}
	return key, nil
}
