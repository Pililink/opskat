package bootstrap

import (
	"context"
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

	svc := command_review_svc.New(
		CommandReviewConfig,
		command_review_svc.NewRepoCache(command_review_repo.CommandReview()),
		func(apiKey, baseURL string) command_review_svc.Evaluator {
			return typesafe.New(apiKey, typesafe.WithBaseURL(baseURL), typesafe.WithHTTPClient(httpClient))
		},
	)
	if withExt, ok := svc.(interface {
		SetExtensionReviewer(func() command_review_svc.ExtensionReviewer)
	}); ok {
		withExt.SetExtensionReviewer(activeExtensionReviewer)
	}
	command_review_svc.Register(svc)
}

// extensionService 由扩展系统初始化后注入。没有注入时审核继续用内置 TypeSafe。
var extensionService *extension_svc.Service

// SetExtensionService 注入扩展服务，供审核查找当前生效的审核器。
func SetExtensionService(svc *extension_svc.Service) { extensionService = svc }

// activeExtensionReviewer 读取设置里选中的审核扩展。没选、未加载或资格不符时返回 nil，
// 审核退回内置 TypeSafe，而不是因为选不中就放行。
func activeExtensionReviewer() command_review_svc.ExtensionReviewer {
	name := GetConfig().CommandReviewExtension
	if name == "" || extensionService == nil {
		return nil
	}
	active, err := extensionService.ActiveCommandReviewer(context.Background(), name)
	if err != nil {
		logger.Default().Warn("active command reviewer unavailable", zap.String("extension", name), zap.Error(err))
		return nil
	}
	return &loadedExtensionReviewer{active: active}
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
