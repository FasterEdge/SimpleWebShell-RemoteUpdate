// ─────────────────────────────────────────────────────────────
// FasterEdge 开源项目
// Github: https://github.com/FasterEdge
// Gitee:  https://gitee.com/FasterEdge
// ─────────────────────────────────────────────────────────────
package workflow

import (
	"fmt"
	"regexp"
	"strings"
)

var safeVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func validateVersion(v string) error {
	if !safeVersion.MatchString(v) || v == "." || v == ".." {
		return fmt.Errorf("非法版本号 %q，只允许字母、数字、点、下划线和横线，最长128字符", v)
	}
	return nil
}

// shellQuote safely quotes arbitrary data for a POSIX shell single argument.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func joinPath(root, child string) string {
	return strings.TrimRight(root, "/") + "/" + strings.TrimLeft(child, "/")
}
