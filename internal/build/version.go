package build

// 这些变量由发布构建通过 -ldflags 注入；本地构建保留可识别的默认值。
var (
	Version           = "dev"
	Commit            = "unknown"
	Date              = "unknown"
	UpdateManifestURL = ""
)

// Info 表示可稳定序列化的构建信息。
type Info struct {
	Version string `json:"version" yaml:"version"`
	Commit  string `json:"commit" yaml:"commit"`
	Date    string `json:"date" yaml:"date"`
}

// Current 返回当前二进制的版本、提交和构建时间。
// 入参：无。
// 返回值：Info，当前构建信息。
func Current() Info {
	return Info{Version: Version, Commit: Commit, Date: Date}
}
