package bootstrap

import (
	"net/http"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/opskat/opskat/internal/pkg/netdial"
	"github.com/opskat/opskat/internal/pkg/typesafe"
	"github.com/opskat/opskat/internal/repository/command_review_repo"
	"github.com/opskat/opskat/internal/service/command_review_svc"
	"github.com/opskat/opskat/internal/service/credential_svc"
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
	))
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
