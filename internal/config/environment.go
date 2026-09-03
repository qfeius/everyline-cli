package config

import (
	"fmt"
	"strings"
)

// EnvironmentPreset 保存一个预设环境的公开连接地址，不包含 app ID 或 app secret。
type EnvironmentPreset struct {
	BaseURL             string
	AuthURL             string
	TokenURL            string
	OAuthMetadataURL    string
	OAuthBusinessType   string
	OAuthClientID       string
	OAuthDeviceClientID string
	OAuthRedirectURL    string
	OAuthScopes         []string
}

// environmentPresets 是环境名到公开连接地址的唯一映射，凭证字段由调用方提供。
var environmentPresets = map[string]EnvironmentPreset{
	"dev": {
		BaseURL:             "https://dev-open.qtech.cn",
		AuthURL:             "https://dev-contract-agent.qtech.cn",
		TokenURL:            "https://dev-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal",
		OAuthMetadataURL:    "https://dev-myaccount.qtech.cn/.well-known/oauth-authorization-server/contract-review",
		OAuthBusinessType:   "contract-review",
		OAuthClientID:       "zscli_a9f2a3ce87fa5bb6",
		OAuthDeviceClientID: "zscli_c77221e810ce3977",
		OAuthRedirectURL:    "http://127.0.0.1:8000/login",
		OAuthScopes:         []string{"contract-review:full"},
	},
	"test": {
		BaseURL:             "https://test-open.qtech.cn",
		AuthURL:             "https://test-contract-agent.qtech.cn",
		TokenURL:            "https://test-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal",
		OAuthMetadataURL:    "https://test-myaccount.qtech.cn/.well-known/oauth-authorization-server/contract-review",
		OAuthBusinessType:   "contract-review",
		OAuthClientID:       "zscli_a94c9aa398389bd7",
		OAuthDeviceClientID: "zscli_c77221e810ce3977",
		OAuthRedirectURL:    "http://127.0.0.1:8000/login",
		OAuthScopes:         []string{"contract-review:full"},
	},
	"blue": {
		BaseURL:  "https://blue-open.qtech.cn",
		AuthURL:  "https://blue-contract-agent.qtech.cn",
		TokenURL: "https://blue-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal",
	},
	"prod": {
		BaseURL:  "https://open.qfei.cn",
		AuthURL:  "https://contract-agent.qfei.cn",
		TokenURL: "https://open.qfei.cn/open-apis/auth/v3/tenant_access_token/internal",
	},
}

// ResolveEnvironmentDeviceClientID 按标准 metadata URL 查找内置环境的 Device client，供旧 Profile 无感迁移。
// 入参：metadataURL string 为 Profile 保存的 OAuth metadata URL。
// 返回值：string 为匹配环境的 Device client ID；非内置环境返回空字符串。
func ResolveEnvironmentDeviceClientID(metadataURL string) string {
	for _, preset := range environmentPresets {
		if strings.TrimSpace(metadataURL) == preset.OAuthMetadataURL {
			return preset.OAuthDeviceClientID
		}
	}
	return ""
}

// ResolveEnvironment 返回指定环境的预设地址。
// 入参：name string 为环境名称，支持 dev、test、blue、prod，不区分大小写。
// 返回值：EnvironmentPreset 为公开连接地址；error 为未知环境时的可识别错误。
func ResolveEnvironment(name string) (EnvironmentPreset, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	if preset, exists := environmentPresets[key]; exists {
		return preset, nil
	}
	return EnvironmentPreset{}, fmt.Errorf("不支持环境 %q，可选环境为 dev、test、blue、prod", name)
}
