package config

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// IdentityKind 标识请求使用的业务身份。
type IdentityKind string

const (
	// IdentityApp 使用 tenant_access_token 访问开放平台。
	IdentityApp IdentityKind = "app"
	// IdentityUser 使用用户认证页面产生的用户访问 token。
	IdentityUser IdentityKind = "user"
)

// ParseIdentityKind 将 CLI 输入转换为受限的身份枚举。
// 入参：value string 为用户传入的身份名称。
// 返回值：IdentityKind 为规范化身份；error 为未知身份。
func ParseIdentityKind(value string) (IdentityKind, error) {
	switch IdentityKind(strings.ToLower(strings.TrimSpace(value))) {
	case "", IdentityApp:
		return IdentityApp, nil
	case IdentityUser:
		return IdentityUser, nil
	default:
		return "", fmt.Errorf("身份必须是 app 或 user")
	}
}

// Profile 保存一个智审开放平台环境的非敏感连接信息。
type Profile struct {
	Name            string       `json:"name" yaml:"name"`
	BaseURL         string       `json:"base_url" yaml:"base_url"`
	UserBaseURL     string       `json:"user_base_url,omitempty" yaml:"user_base_url,omitempty"`
	AuthURL         string       `json:"auth_url,omitempty" yaml:"auth_url,omitempty"`
	TokenURL        string       `json:"token_url" yaml:"token_url"`
	AppID           string       `json:"app_id" yaml:"app_id"`
	DefaultIdentity IdentityKind `json:"default_identity,omitempty" yaml:"default_identity,omitempty"`
	DefaultOutput   string       `json:"default_output" yaml:"default_output"`
}

// Validate 校验 Profile 的名称、URL、应用 ID 和默认输出格式。
// 入参：无，接收者 Profile 为待校验配置。
// 返回值：error，配置有效时为 nil。
func (profile Profile) Validate() error {
	if !profileNamePattern.MatchString(profile.Name) {
		return fmt.Errorf("profile 名称只能包含字母、数字、点、下划线和短横线")
	}
	if err := validateBaseURL(profile.BaseURL); err != nil {
		return err
	}
	if profile.UserBaseURL != "" {
		if err := validateBaseURL(profile.UserBaseURL); err != nil {
			return fmt.Errorf("user-base-url: %w", err)
		}
	}
	if profile.AuthURL != "" {
		if err := validateHTTPURL("auth-url", profile.AuthURL); err != nil {
			return err
		}
	}
	if err := validateHTTPURL("token-url", profile.TokenURL); err != nil {
		return err
	}
	if strings.TrimSpace(profile.AppID) == "" {
		return fmt.Errorf("app-id 不能为空")
	}
	identity, err := ParseIdentityKind(string(profile.DefaultIdentity))
	if err != nil {
		return err
	}
	profile.DefaultIdentity = identity
	if profile.DefaultOutput == "" {
		profile.DefaultOutput = "table"
	}
	switch profile.DefaultOutput {
	case "json", "yaml", "table", "raw":
		return nil
	default:
		return fmt.Errorf("default-output 必须是 json、yaml、table 或 raw")
	}
}

// BaseURLFor 返回指定身份的业务 API 基址；user 未单独配置时复用 app 基址。
// 入参：identity IdentityKind 为请求身份。
// 返回值：string 为可拼接 /open-apis/ 路径的基础地址。
func (profile Profile) BaseURLFor(identity IdentityKind) string {
	if identity == IdentityUser && strings.TrimSpace(profile.UserBaseURL) != "" {
		return profile.UserBaseURL
	}
	return profile.BaseURL
}

// AuthURLFor 返回指定身份的用户认证页面地址；app 身份不需要页面认证。
// 入参：identity IdentityKind 为业务身份。
// 返回值：string 为配置中的自有认证页面地址，app 身份返回空字符串。
func (profile Profile) AuthURLFor(identity IdentityKind) string {
	if identity == IdentityUser {
		return profile.AuthURL
	}
	return ""
}

// validateBaseURL 校验业务基础地址可被 HTTP Adapter 安全拼接固定的 /open-apis/ 路径。
// 入参：value string 为待校验的基础 URL。
// 返回值：error，URL 完整、安全且不含 query/fragment 时为 nil。
func validateBaseURL(value string) error {
	if err := validateHTTPURL("base-url", value); err != nil {
		return err
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil {
		return fmt.Errorf("base-url 必须是完整的 http/https URL")
	}
	// HTTP Adapter 会在 BaseURL 后拼接固定路径，query/fragment 会改变最终请求目标。
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("base-url 不能包含 query 或 fragment")
	}
	return nil
}

// validateHTTPURL 校验字符串是完整的 HTTPS URL；仅为本机开发保留 loopback HTTP。
// 入参：field string 为字段名；value string 为待校验 URL。
// 返回值：error，URL 合法时为 nil。
func validateHTTPURL(field string, value string) error {
	normalized := strings.TrimSpace(value)
	if value != normalized {
		return fmt.Errorf("%s 不能包含首尾空白", field)
	}
	parsed, err := url.ParseRequestURI(normalized)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s 必须是完整的 http/https URL", field)
	}
	// app secret 和 Bearer token 只能经 HTTPS 传输；HTTP 仅允许回环地址用于本地测试。
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return fmt.Errorf("%s 必须使用 https；仅 localhost 或回环 IP 允许 http", field)
	}
	return nil
}

// isLoopbackHost 判断 URL 主机是否严格指向本机，避免明文凭据发往局域网或公网。
// 入参：host string 为不含端口的主机名或 IP。
// 返回值：bool，localhost 或 loopback IP 返回 true。
func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
