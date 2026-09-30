package system

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommandReviewSaveInputValidate(t *testing.T) {
	valid := []CommandReviewSaveInput{
		{},
		{Model: "jev-1.13.0", TimeoutMs: 1000, Threshold: 0.3},
		{TimeoutMs: 60000, Threshold: 0.99},
		// 模型名由用户自己填，不做限制
		{Model: "jev-latest"},
		{Model: "my-self-hosted-jev"},
		{BaseURL: "https://api.typesafe.ai"},
		{BaseURL: " http://10.0.0.5:8080/ "},
	}
	for _, in := range valid {
		assert.NoError(t, in.validate(), "%+v", in)
	}

	invalid := []CommandReviewSaveInput{
		{BaseURL: "api.typesafe.ai"},
		{BaseURL: "ftp://10.0.0.5"},
		{BaseURL: "https://"},
		{TimeoutMs: 999},
		{TimeoutMs: 60001},
		{Threshold: -0.1},
		{Threshold: 1},
	}
	for _, in := range invalid {
		assert.Error(t, in.validate(), "%+v", in)
	}
}

// 地址只在这里规范化一次：去掉首尾空白和末尾的 /，客户端再拼上 /v1/systemone。
func TestCommandReviewBaseURLIsNormalized(t *testing.T) {
	assert.Equal(t, "http://10.0.0.5:8080", CommandReviewSaveInput{BaseURL: " http://10.0.0.5:8080/ "}.baseURL())
	assert.Equal(t, "", CommandReviewSaveInput{}.baseURL())
}
