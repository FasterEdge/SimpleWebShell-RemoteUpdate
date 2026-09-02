// ─────────────────────────────────────────────────────────────
// FasterEdge 开源项目
// Github: https://github.com/FasterEdge
// Gitee:  https://gitee.com/FasterEdge
// ─────────────────────────────────────────────────────────────
// Package workflow implements the fixed, non-invasive remote release workflow.
package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/FasterEdge/SimpleWebShell-RemoteUpdate/internal/config"
)

// Remote is the minimal SimpleWebShell API used by the workflow.
type Remote interface {
	Probe(context.Context) error
	CreateSession(context.Context) (string, error)
	DeleteSession(context.Context) error
	Exec(context.Context, string) (string, error)
	Upload(context.Context, string, string) error
}

// Engine runs workflows.
type Engine struct {
	Config *config.Config
	Remote Remote
	DryRun bool
	Log    func(string, ...any)
}

// Status describes the remote release state.
type Status struct {
	App             string   `json:"app"`
	Root            string   `json:"root"`
	CurrentTarget   string   `json:"current_target,omitempty"`
	PreviousTarget  string   `json:"previous_target,omitempty"`
	CurrentVersion  string   `json:"current_version,omitempty"`
	PreviousVersion string   `json:"previous_version,omitempty"`
	Releases        []string `json:"releases,omitempty"`
}

func (e *Engine) log(format string, args ...any) {
	if e.Log != nil {
		e.Log(format, args...)
	}
}

// Begin probes the endpoint and optionally creates an isolated SimpleWebShell session.
func (e *Engine) Begin(ctx context.Context) error {
	if err := e.Remote.Probe(ctx); err != nil {
		return err
	}
	if e.Config.Remote.SessionEnabled {
		id, err := e.Remote.CreateSession(ctx)
		if err != nil {
			return fmt.Errorf("创建远程 session 失败: %w", err)
		}
		e.log("远程 session: %s", id)
	}
	return nil
}

// End deletes the temporary session.
func (e *Engine) End(ctx context.Context) {
	if e.Config.Remote.SessionEnabled {
		if err := e.Remote.DeleteSession(ctx); err != nil {
			e.log("清理远程 session 失败: %v", err)
		}
	}
}

// Init creates the immutable release layout without changing SimpleWebShell.
func (e *Engine) Init(ctx context.Context) error {
	root := e.Config.App.Root
	cmd := fmt.Sprintf("umask 022 && mkdir -p %s %s %s && printf '%%s\\n' %s > %s",
		shellQuote(root), shellQuote(joinPath(root, "releases")), shellQuote(joinPath(root, "incoming")),
		shellQuote(e.Config.App.Name), shellQuote(joinPath(root, ".remoteupdate-app")))
	_, err := e.exec(ctx, "初始化目录", cmd)
	return err
}

// Install installs a release when no current link exists; it refuses to overwrite current.
func (e *Engine) Install(ctx context.Context, version, artifact string) error {
	if err := validateVersion(version); err != nil {
		return err
	}
	check := fmt.Sprintf("if [ -L %s ] || [ -e %s ]; then echo CURRENT_EXISTS; exit 17; fi", shellQuote(e.current()), shellQuote(e.current()))
	if _, err := e.exec(ctx, "确认首次安装", check); err != nil {
		return fmt.Errorf("初始化安装拒绝覆盖已有 current，请使用 update: %w", err)
	}
	return e.deploy(ctx, version, artifact, false)
}

// Update installs and activates a new release, with optional automatic rollback.
func (e *Engine) Update(ctx context.Context, version, artifact string) error {
	if err := validateVersion(version); err != nil {
		return err
	}
	return e.deploy(ctx, version, artifact, true)
}

func (e *Engine) deploy(ctx context.Context, version, artifact string, update bool) error {
	if artifact == "" {
		return fmt.Errorf("artifact 不能为空")
	}
	if _, err := os.Stat(artifact); err != nil {
		return fmt.Errorf("读取 artifact 失败: %w", err)
	}
	if err := validateArtifact(artifact, e.Config.App.ArtifactType); err != nil {
		return fmt.Errorf("发布包安全校验失败: %w", err)
	}
	if err := e.Init(ctx); err != nil {
		return err
	}

	checksum, err := fileSHA256(artifact)
	if err != nil {
		return err
	}
	remoteArtifact := joinPath(e.Config.App.Root, "incoming/"+version+"-"+filepath.Base(artifact))
	e.log("上传 %s -> %s", artifact, remoteArtifact)
	if !e.DryRun {
		if err := e.Remote.Upload(ctx, artifact, remoteArtifact); err != nil {
			return err
		}
	}

	releaseDir := joinPath(e.Config.App.Root, "releases/"+version)
	activeTarget := releaseDir
	if e.Config.App.InstallSubdir != "" {
		activeTarget = joinPath(releaseDir, e.Config.App.InstallSubdir)
	}
	verify := fmt.Sprintf(`set -eu; if command -v sha256sum >/dev/null 2>&1; then actual=$(sha256sum %s | awk '{print $1}'); elif command -v shasum >/dev/null 2>&1; then actual=$(shasum -a 256 %s | awk '{print $1}'); else echo 'remote sha256 checker not found'; exit 12; fi; test "$actual" = %s`, shellQuote(remoteArtifact), shellQuote(remoteArtifact), shellQuote(checksum))
	if _, err := e.exec(ctx, "校验上传文件 SHA-256", verify); err != nil {
		return err
	}

	prepare := e.prepareCommand(version, remoteArtifact, releaseDir, checksum)
	if _, err := e.exec(ctx, "准备版本 "+version, prepare); err != nil {
		return err
	}
	if activeTarget != releaseDir {
		if _, err := e.exec(ctx, "确认安装子目录", fmt.Sprintf("test -d %s", shellQuote(activeTarget))); err != nil {
			return err
		}
	}

	oldTarget := ""
	if update {
		oldTarget, _ = e.readlink(ctx, e.current())
	}
	if e.Config.App.PreSwitch != "" {
		if _, err := e.exec(ctx, "切换前命令", e.Config.App.PreSwitch); err != nil {
			return err
		}
	}
	if err := e.activate(ctx, activeTarget, oldTarget); err != nil {
		return err
	}

	if e.Config.App.PostSwitch != "" {
		if _, err := e.exec(ctx, "切换后命令", e.Config.App.PostSwitch); err != nil {
			return e.activationFailure(ctx, oldTarget, err)
		}
	}
	if e.Config.App.HealthCheck != "" {
		if _, err := e.exec(ctx, "健康检查", e.Config.App.HealthCheck); err != nil {
			return e.activationFailure(ctx, oldTarget, err)
		}
	} else if e.Config.Policy.RequireHealthCheck {
		return e.activationFailure(ctx, oldTarget, fmt.Errorf("策略要求健康检查，但 app.health_check 未配置"))
	}

	if _, err := e.exec(ctx, "写入版本元数据", fmt.Sprintf("printf '%%s\\n' %s > %s", shellQuote(version), shellQuote(joinPath(releaseDir, ".remoteupdate-version")))); err != nil {
		return err
	}
	if err := e.Cleanup(ctx); err != nil {
		e.log("清理旧版本失败（不影响已激活版本）: %v", err)
	}
	return nil
}

func (e *Engine) activationFailure(ctx context.Context, oldTarget string, cause error) error {
	if !e.Config.Policy.AutoRollback {
		return cause
	}
	if oldTarget == "" {
		e.log("首次激活失败，移除 current 软链接")
		if _, err := e.exec(ctx, "撤销首次激活", fmt.Sprintf("rm -f %s", shellQuote(e.current()))); err != nil {
			return fmt.Errorf("原错误: %v；撤销首次激活失败: %w", cause, err)
		}
	} else {
		e.log("激活失败，自动回滚到 %s", oldTarget)
		if err := e.switchLink(ctx, oldTarget, ""); err != nil {
			return fmt.Errorf("原错误: %v；自动回滚失败: %w", cause, err)
		}
	}
	if e.Config.App.Rollback != "" {
		_, _ = e.exec(ctx, "回滚后命令", e.Config.App.Rollback)
	}
	return fmt.Errorf("激活失败并已自动回滚: %w", cause)
}

func (e *Engine) prepareCommand(version, artifact, releaseDir, checksum string) string {
	marker := joinPath(releaseDir, ".remoteupdate-sha256")
	base := fmt.Sprintf("set -eu; rm -rf %s; mkdir -p %s; ", shellQuote(releaseDir), shellQuote(releaseDir))
	switch e.Config.App.ArtifactType {
	case "tar.gz", "tgz":
		base += fmt.Sprintf("tar -xzf %s -C %s; ", shellQuote(artifact), shellQuote(releaseDir))
	case "tar":
		base += fmt.Sprintf("tar -xf %s -C %s; ", shellQuote(artifact), shellQuote(releaseDir))
	case "zip":
		base += fmt.Sprintf("unzip -q %s -d %s; ", shellQuote(artifact), shellQuote(releaseDir))
	case "file":
		name := filepath.Base(artifact)
		base += fmt.Sprintf("cp %s %s; chmod +x %s; ", shellQuote(artifact), shellQuote(joinPath(releaseDir, name)), shellQuote(joinPath(releaseDir, name)))
	}
	base += fmt.Sprintf("printf '%%s\\n' %s > %s; rm -f %s", shellQuote(checksum), shellQuote(marker), shellQuote(artifact))
	_ = version
	return base
}

func (e *Engine) activate(ctx context.Context, releaseDir, oldTarget string) error {
	return e.switchLink(ctx, releaseDir, oldTarget)
}

func (e *Engine) switchLink(ctx context.Context, target, oldTarget string) error {
	current := e.current()
	previous := e.previous()
	tmp := current + ".next"
	parts := []string{"set -eu"}
	if oldTarget != "" {
		parts = append(parts, fmt.Sprintf("ln -sfn %s %s", shellQuote(oldTarget), shellQuote(previous)))
	}
	parts = append(parts,
		fmt.Sprintf("ln -sfn %s %s", shellQuote(target), shellQuote(tmp)),
		fmt.Sprintf("mv -Tf %s %s 2>/dev/null || ln -sfn %s %s", shellQuote(tmp), shellQuote(current), shellQuote(target), shellQuote(current)),
		fmt.Sprintf("rm -f %s", shellQuote(tmp)),
	)
	_, err := e.exec(ctx, "切换 current 软链接", strings.Join(parts, "; "))
	return err
}

// Rollback switches current to previous or to a specified release version.
func (e *Engine) Rollback(ctx context.Context, version string) error {
	var target string
	if version == "" {
		var err error
		target, err = e.readlink(ctx, e.previous())
		if err != nil || target == "" {
			return fmt.Errorf("没有可用的 previous 版本")
		}
	} else {
		if err := validateVersion(version); err != nil {
			return err
		}
		target = joinPath(e.Config.App.Root, "releases/"+version)
		if e.Config.App.InstallSubdir != "" {
			target = joinPath(target, e.Config.App.InstallSubdir)
		}
		if _, err := e.exec(ctx, "确认回滚版本存在", fmt.Sprintf("test -d %s", shellQuote(target))); err != nil {
			return err
		}
	}
	current, _ := e.readlink(ctx, e.current())
	if err := e.switchLink(ctx, target, current); err != nil {
		return err
	}
	if e.Config.App.Rollback != "" {
		_, err := e.exec(ctx, "回滚后命令", e.Config.App.Rollback)
		return err
	}
	return nil
}

// Cleanup removes old inactive releases according to keep_releases.
func (e *Engine) Cleanup(ctx context.Context) error {
	keep := e.Config.Policy.KeepReleases
	if keep <= 0 {
		return nil
	}
	root := joinPath(e.Config.App.Root, "releases")
	current, _ := e.readlink(ctx, e.current())
	previous, _ := e.readlink(ctx, e.previous())
	current = e.releaseRootForTarget(current)
	previous = e.releaseRootForTarget(previous)
	cmd := fmt.Sprintf(`set -eu; n=0; (ls -1dt -- %s/* 2>/dev/null || true) | while IFS= read -r d; do [ -d "$d" ] || continue; [ "$d" = %s ] && continue; [ "$d" = %s ] && continue; n=$((n+1)); [ "$n" -le %d ] || rm -rf -- "$d"; done`, shellQuote(root), shellQuote(current), shellQuote(previous), keep)
	_, err := e.exec(ctx, "清理旧版本", cmd)
	return err
}

// GetStatus reads current/previous links and release directories.
func (e *Engine) GetStatus(ctx context.Context) (*Status, error) {
	current, _ := e.readlink(ctx, e.current())
	previous, _ := e.readlink(ctx, e.previous())
	out, err := e.exec(ctx, "读取版本列表", fmt.Sprintf("find %s -mindepth 1 -maxdepth 1 -type d -print 2>/dev/null || true", shellQuote(joinPath(e.Config.App.Root, "releases"))))
	if err != nil {
		return nil, err
	}
	var releases []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			releases = append(releases, filepath.Base(line))
		}
	}
	sort.Strings(releases)
	return &Status{
		App: e.Config.App.Name, Root: e.Config.App.Root,
		CurrentTarget: current, PreviousTarget: previous,
		CurrentVersion: e.versionForTarget(current), PreviousVersion: e.versionForTarget(previous),
		Releases: releases,
	}, nil
}

// MarshalStatus returns indented status JSON.
func MarshalStatus(s *Status) string {
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (e *Engine) readlink(ctx context.Context, path string) (string, error) {
	out, err := e.exec(ctx, "读取链接 "+path, fmt.Sprintf("readlink %s 2>/dev/null || true", shellQuote(path)))
	return strings.TrimSpace(out), err
}

func (e *Engine) current() string  { return joinPath(e.Config.App.Root, "current") }
func (e *Engine) previous() string { return joinPath(e.Config.App.Root, "previous") }

func (e *Engine) releaseRootForTarget(target string) string {
	if target == "" || e.Config.App.InstallSubdir == "" {
		return target
	}
	suffix := "/" + strings.Trim(e.Config.App.InstallSubdir, "/")
	return strings.TrimSuffix(target, suffix)
}

func (e *Engine) versionForTarget(target string) string {
	root := e.releaseRootForTarget(target)
	if root == "" {
		return ""
	}
	return filepath.Base(root)
}

func (e *Engine) exec(ctx context.Context, step, command string) (string, error) {
	e.log("[%s] %s", step, command)
	if e.DryRun {
		return "", nil
	}
	out, err := e.Remote.Exec(ctx, command)
	if err != nil {
		return out, fmt.Errorf("%s: %w", step, err)
	}
	if out != "" {
		e.log("[%s] %s", step, out)
	}
	return out, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// NowVersion creates a sortable version when one is not supplied.
func NowVersion() string { return time.Now().UTC().Format("20060102T150405Z") }
