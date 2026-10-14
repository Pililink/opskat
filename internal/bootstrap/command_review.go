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
func (r *loadedExtensionReviewer) Questions() []string { return r.active.Questions }
func (r *loadedExtensionReviewer) Review(ctx context.Context, items []extension.ReviewItem) ([]extension.ReviewCommandResult, error) {
	return r.active.Plugin.ReviewCommands(ctx, items, 0)
}

// CommandReviewConfig 从 config.json 读取审核设置，并解密 API key。
// 解密失败时按未配置处理（审核失败，不会因此放行），并记录错误。
func CommandReviewConfig() command_review_svc.Config {
	cfg := GetConfig()
	return command_review_svc.NewConfig(CommandReviewAPIKey(), cfg.CommandReviewBaseURL, cfg.CommandReviewModel, cfg.CommandReviewTimeoutMs, cfg.CommandReviewThreshold)
}

// CommandReviewAPIKey 返回解密后的审核 API key；没有配置或解密失败时为空。
func CommandReviewAPIKey() string {
	encrypted := GetConfig().CommandReviewAPIKey
	if encrypted == "" {
		return ""
	}
	key, err := credential_svc.Default().Decrypt(encrypted)
	if err != nil {
		logger.Default().Error("decrypt command review api key", zap.Error(err))
		return ""
	}
	return key
}
