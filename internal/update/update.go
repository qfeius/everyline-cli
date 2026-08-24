package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	maxManifestBytes = 1 << 20
	maxArtifactBytes = 256 << 20
)

// ErrNPMWrapper 表示当前进程由 npm/npx 薄包装启动，不能直接替换包内二进制。
var ErrNPMWrapper = errors.New("当前命令由 npm/npx 薄包装启动，请使用 npm install -g everyline-cli@latest 或 npx 更新包")

// Manifest 描述一个版本及各平台的独立二进制制品。
type Manifest struct {
	Version   string              `json:"version"`
	Platforms map[string]Artifact `json:"platforms"`
}

// Artifact 描述一个平台制品的下载地址和内容摘要。
type Artifact struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// Options 保存更新器的 I/O 和运行边界。
type Options struct {
	HTTPClient     *http.Client
	ExecutablePath string
	Platform       string
	DryRun         bool
	Wrapper        bool
	// replaceBinary 仅用于测试注入 deferred replacement；生产环境使用平台实现。
	replaceBinary func(string, string) (bool, error)
}

// Result 是 update 命令的稳定输出结构。
type Result struct {
	CurrentVersion string `json:"currentVersion" yaml:"currentVersion"`
	LatestVersion  string `json:"latestVersion" yaml:"latestVersion"`
	Platform       string `json:"platform" yaml:"platform"`
	Updated        bool   `json:"updated" yaml:"updated"`
	Scheduled      bool   `json:"scheduled" yaml:"scheduled"`
	DryRun         bool   `json:"dryRun" yaml:"dryRun"`
}

var semverPattern = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

type version struct {
	major      uint64
	minor      uint64
	patch      uint64
	prerelease []string
}

// Run 获取 manifest、校验当前平台制品并在需要时完成独立二进制更新。
// 入参：ctx context.Context 控制 manifest 与制品下载；currentVersion/manifestURL string 分别为当前构建版本和显式 HTTPS manifest 地址；options Options 为更新边界。
// 返回值：Result 为更新结果；error 为校验、下载、替换调度或同步替换失败。
func Run(ctx context.Context, currentVersion string, manifestURL string, options Options) (Result, error) {
	if options.Wrapper {
		return Result{}, ErrNPMWrapper
	}
	manifestURL = strings.TrimSpace(manifestURL)
	if err := validateHTTPSURL(manifestURL, "manifest URL"); err != nil {
		return Result{}, err
	}
	current, err := parseVersion(currentVersion)
	if err != nil {
		return Result{}, fmt.Errorf("当前版本不可用于自更新: %w", err)
	}
	platform := strings.TrimSpace(options.Platform)
	if platform == "" {
		platform = runtime.GOOS + "-" + runtime.GOARCH
	}
	manifest, err := fetchManifest(ctx, options.HTTPClient, manifestURL)
	if err != nil {
		return Result{}, err
	}
	latest, err := parseVersion(manifest.Version)
	if err != nil {
		return Result{}, fmt.Errorf("manifest version 无效: %w", err)
	}
	artifact, ok := manifest.Platforms[platform]
	if !ok {
		return Result{}, fmt.Errorf("manifest 未提供当前平台制品: %s", platform)
	}
	if err := validateArtifact(artifact); err != nil {
		return Result{}, fmt.Errorf("平台 %s 制品无效: %w", platform, err)
	}
	result := Result{
		CurrentVersion: strings.TrimSpace(currentVersion),
		LatestVersion:  strings.TrimSpace(manifest.Version),
		Platform:       platform,
		DryRun:         options.DryRun,
	}
	if compareVersions(current, latest) >= 0 {
		return result, nil
	}
	if options.DryRun {
		return result, nil
	}

	target, info, err := executableTarget(options.ExecutablePath)
	if err != nil {
		return Result{}, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".everyline-cli-update-*")
	if err != nil {
		return Result{}, fmt.Errorf("创建更新临时文件失败: %w", err)
	}
	temporaryPath := temporary.Name()
	deferredCleanup := true
	defer func() {
		if deferredCleanup {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := downloadArtifact(ctx, options.HTTPClient, artifact, temporary); err != nil {
		_ = temporary.Close()
		return Result{}, err
	}
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		_ = temporary.Close()
		return Result{}, fmt.Errorf("设置更新文件权限失败: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return Result{}, fmt.Errorf("写入更新文件失败: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return Result{}, fmt.Errorf("关闭更新文件失败: %w", err)
	}
	replacer := options.replaceBinary
	if replacer == nil {
		replacer = replaceBinary
	}
	deferred, err := replacer(temporaryPath, target)
	if err != nil {
		return Result{}, fmt.Errorf("替换当前二进制失败: %w", err)
	}
	if deferred {
		// Windows helper 尚未完成最终替换；保留下载文件并明确返回 scheduled，不能提前宣称 updated。
		deferredCleanup = false
		result.Scheduled = true
		return result, nil
	}
	result.Updated = true
	return result, nil
}

// RunDeferredReplacement 执行 Windows 替换助手参数；其他平台拒绝该内部入口。
// 入参：args []string 为临时文件、目标文件、父进程 PID 和 helper 路径。
// 返回值：error，为参数、父进程等待或替换失败原因。
func RunDeferredReplacement(args []string) error {
	return runDeferredReplacement(args)
}

// replaceWithRetry 在 deferred helper 中重试替换目标文件，并保留最后一次失败原因。
// 入参：temporaryPath/targetPath string 为制品临时路径和目标路径；attempts int 为最大尝试次数；delay time.Duration 为重试间隔；rename func 执行单次替换；sleep func 执行等待。
// 返回值：error，替换成功时为 nil，参数无效或重试耗尽时返回最后一次替换错误。
func replaceWithRetry(temporaryPath string, targetPath string, attempts int, delay time.Duration, rename func(string, string) error, sleep func(time.Duration)) error {
	if attempts <= 0 {
		return errors.New("替换重试次数必须大于 0")
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := rename(temporaryPath, targetPath); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt+1 < attempts {
			sleep(delay)
		}
	}
	return fmt.Errorf("替换目标文件失败: %w", lastErr)
}

func fetchManifest(ctx context.Context, client *http.Client, manifestURL string) (Manifest, error) {
	content, err := getLimited(ctx, client, manifestURL, maxManifestBytes)
	if err != nil {
		return Manifest{}, fmt.Errorf("下载 manifest 失败: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("manifest JSON 无效: %w", err)
	}
	if strings.TrimSpace(manifest.Version) == "" || len(manifest.Platforms) == 0 {
		return Manifest{}, fmt.Errorf("manifest 必须包含 version 和 platforms")
	}
	return manifest, nil
}

func getLimited(ctx context.Context, client *http.Client, targetURL string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建下载请求失败: %w", err)
	}
	request.Header.Set("Accept", "application/json, application/octet-stream")
	request.Header.Set("User-Agent", "everyline-cli")
	response, err := safeHTTPClient(client).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.Body == nil {
		return nil, errors.New("响应缺少 body")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HTTP 状态 %d", response.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("响应超过 %d 字节限制", limit)
	}
	return content, nil
}

func downloadArtifact(ctx context.Context, client *http.Client, artifact Artifact, destination *os.File) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(artifact.URL), nil)
	if err != nil {
		return fmt.Errorf("创建制品下载请求失败: %w", err)
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "everyline-cli")
	response, err := safeHTTPClient(client).Do(request)
	if err != nil {
		return fmt.Errorf("下载制品失败: %w", err)
	}
	defer response.Body.Close()
	if response.Body == nil {
		return errors.New("下载制品响应缺少 body")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("下载制品失败: HTTP 状态 %d", response.StatusCode)
	}
	hasher := sha256.New()
	limited := io.LimitReader(response.Body, maxArtifactBytes+1)
	count, err := io.Copy(io.MultiWriter(destination, hasher), limited)
	if err != nil {
		return fmt.Errorf("写入制品失败: %w", err)
	}
	if count == 0 {
		return errors.New("下载制品为空")
	}
	if count > maxArtifactBytes {
		return fmt.Errorf("制品超过 %d 字节限制", maxArtifactBytes)
	}
	want, _ := hex.DecodeString(strings.ToLower(strings.TrimSpace(artifact.SHA256)))
	if !equalBytes(hasher.Sum(nil), want) {
		return errors.New("制品 SHA-256 校验失败")
	}
	return nil
}

func equalBytes(left []byte, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}

func validateArtifact(artifact Artifact) error {
	if err := validateHTTPSURL(strings.TrimSpace(artifact.URL), "制品 URL"); err != nil {
		return err
	}
	checksum := strings.TrimSpace(artifact.SHA256)
	if len(checksum) != sha256.Size*2 {
		return errors.New("SHA-256 必须是 64 位十六进制字符串")
	}
	if _, err := hex.DecodeString(checksum); err != nil {
		return errors.New("SHA-256 必须是十六进制字符串")
	}
	return nil
}

func validateHTTPSURL(value string, label string) error {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || strings.ToLower(parsed.Scheme) != "https" {
		return fmt.Errorf("%s 必须是 HTTPS URL", label)
	}
	return nil
}

func executableTarget(path string) (string, os.FileInfo, error) {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = os.Executable()
		if err != nil {
			return "", nil, fmt.Errorf("定位当前二进制失败: %w", err)
		}
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, fmt.Errorf("解析当前二进制路径失败: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", nil, fmt.Errorf("读取当前二进制信息失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", nil, errors.New("当前可执行文件不是普通文件")
	}
	return resolved, info, nil
}

func parseVersion(value string) (version, error) {
	value = strings.TrimSpace(value)
	matches := semverPattern.FindStringSubmatch(value)
	if matches == nil {
		return version{}, fmt.Errorf("%q 不是有效 SemVer", value)
	}
	major, err := parseVersionNumber(matches[1])
	if err != nil {
		return version{}, err
	}
	minor, err := parseVersionNumber(matches[2])
	if err != nil {
		return version{}, err
	}
	patch, err := parseVersionNumber(matches[3])
	if err != nil {
		return version{}, err
	}
	result := version{major: major, minor: minor, patch: patch}
	if matches[4] != "" {
		result.prerelease = strings.Split(matches[4], ".")
	}
	for _, identifier := range result.prerelease {
		if identifier == "" {
			return version{}, fmt.Errorf("%q 的预发布标识无效", value)
		}
		if len(identifier) > 1 && identifier[0] == '0' && isNumeric(identifier) {
			return version{}, fmt.Errorf("%q 的数字预发布标识不能有前导零", value)
		}
	}
	return result, nil
}

func parseVersionNumber(value string) (uint64, error) {
	if len(value) > 1 && value[0] == '0' {
		return 0, fmt.Errorf("版本数字 %q 不能有前导零", value)
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("版本数字 %q 无效", value)
	}
	return parsed, nil
}

func isNumeric(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func compareVersions(left version, right version) int {
	for _, pair := range [][2]uint64{{left.major, right.major}, {left.minor, right.minor}, {left.patch, right.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if len(left.prerelease) == 0 && len(right.prerelease) == 0 {
		return 0
	}
	if len(left.prerelease) == 0 {
		return 1
	}
	if len(right.prerelease) == 0 {
		return -1
	}
	for index := 0; index < len(left.prerelease) && index < len(right.prerelease); index++ {
		leftPart, rightPart := left.prerelease[index], right.prerelease[index]
		leftNumeric, rightNumeric := isNumeric(leftPart), isNumeric(rightPart)
		if leftNumeric && rightNumeric {
			leftNumber, _ := strconv.ParseUint(leftPart, 10, 64)
			rightNumber, _ := strconv.ParseUint(rightPart, 10, 64)
			if leftNumber < rightNumber {
				return -1
			}
			if leftNumber > rightNumber {
				return 1
			}
			continue
		}
		if leftNumeric != rightNumeric {
			if leftNumeric {
				return -1
			}
			return 1
		}
		if leftPart < rightPart {
			return -1
		}
		if leftPart > rightPart {
			return 1
		}
	}
	if len(left.prerelease) < len(right.prerelease) {
		return -1
	}
	if len(left.prerelease) > len(right.prerelease) {
		return 1
	}
	return 0
}

func safeHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &copy
}
