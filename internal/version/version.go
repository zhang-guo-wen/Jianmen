// Package version 提供 Jianmen 的软件版本号。
package version

import "strings"

// Version 是软件版本号。
// 发布构建通过 -ldflags "-X jianmen/internal/version.Version=v1.0.1" 注入 git tag；
// 本地开发构建未注入时回退到下面的默认值。
var Version = "1.0.0"

// Display 返回界面展示用的版本号：去掉首尾空白和前缀 v，界面统一补 v 前缀。
func Display() string {
	trimmed := strings.TrimSpace(Version)
	trimmed = strings.TrimPrefix(trimmed, "v")
	trimmed = strings.TrimPrefix(trimmed, "V")
	if trimmed == "" {
		return "dev"
	}
	return trimmed
}
