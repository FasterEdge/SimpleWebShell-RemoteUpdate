package workflow

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/FasterEdge/SimpleWebShell-RemoteUpdate/internal/config"
)

type fakeRemote struct {
	commands []string
	uploads  [][2]string
	failOn   string
	links    map[string]string
}

func (f *fakeRemote) Probe(context.Context) error                   { return nil }
func (f *fakeRemote) CreateSession(context.Context) (string, error) { return "s1", nil }
func (f *fakeRemote) DeleteSession(context.Context) error           { return nil }
func (f *fakeRemote) Upload(_ context.Context, local, remote string) error {
	f.uploads = append(f.uploads, [2]string{local, remote})
	return nil
}
func (f *fakeRemote) Exec(_ context.Context, cmd string) (string, error) {
	f.commands = append(f.commands, cmd)
	if f.failOn != "" && strings.Contains(cmd, f.failOn) {
		return "failed", errors.New("forced")
	}
	if strings.HasPrefix(cmd, "readlink ") {
		if strings.Contains(cmd, "current") {
			return f.links["current"], nil
		}
		if strings.Contains(cmd, "previous") {
			return f.links["previous"], nil
		}
	}
	return "", nil
}

func testConfig() *config.Config {
	return &config.Config{
		Remote: config.RemoteConfig{URL: "http://host:8878", Key: "k", Timeout: time.Second},
		App:    config.AppConfig{Name: "demo", Root: "/opt/demo", ArtifactType: "tar.gz", HealthCheck: "curl -f http://127.0.0.1/health"},
		Policy: config.PolicyConfig{KeepReleases: 2, AutoRollback: true},
	}
}

func TestValidateVersionRejectsInjection(t *testing.T) {
	bad := []string{"", "../x", "v1;rm -rf /", "$(id)", "v 1", strings.Repeat("a", 129)}
	for _, v := range bad {
		if validateVersion(v) == nil {
			t.Fatalf("expected rejection: %q", v)
		}
	}
	if err := validateVersion("v1.2.3-rc_1"); err != nil {
		t.Fatal(err)
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote("a'b")
	if got != `'a'"'"'b'` {
		t.Fatalf("quote=%q", got)
	}
}

func TestInitFixedLayout(t *testing.T) {
	f := &fakeRemote{}
	e := &Engine{Config: testConfig(), Remote: f}
	if err := e.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	cmd := f.commands[0]
	for _, want := range []string{"'/opt/demo/releases'", "'/opt/demo/incoming'", "'/opt/demo/.remoteupdate-app'"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("missing %s in %s", want, cmd)
		}
	}
}

func TestUpdateUploadsVerifiesAndSwitches(t *testing.T) {
	artifact := createArtifact(t)
	f := &fakeRemote{links: map[string]string{"current": "/opt/demo/releases/v1"}}
	e := &Engine{Config: testConfig(), Remote: f}
	if err := e.Update(context.Background(), "v2", artifact); err != nil {
		t.Fatal(err)
	}
	if len(f.uploads) != 1 || !strings.Contains(f.uploads[0][1], "/incoming/v2-") {
		t.Fatalf("uploads=%v", f.uploads)
	}
	all := strings.Join(f.commands, "\n")
	for _, want := range []string{"sha256sum", "tar -xzf", "ln -sfn '/opt/demo/releases/v2'", "health", ".remoteupdate-version"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in commands:\n%s", want, all)
		}
	}
}

func TestHealthFailureTriggersRollback(t *testing.T) {
	artifact := createArtifact(t)
	f := &fakeRemote{links: map[string]string{"current": "/opt/demo/releases/v1"}, failOn: "curl -f"}
	e := &Engine{Config: testConfig(), Remote: f}
	err := e.Update(context.Background(), "v2", artifact)
	if err == nil || !strings.Contains(err.Error(), "已自动回滚") {
		t.Fatalf("err=%v", err)
	}
	all := strings.Join(f.commands, "\n")
	if strings.Count(all, "ln -sfn '/opt/demo/releases/v1'") < 1 {
		t.Fatalf("rollback command missing:\n%s", all)
	}
}

func TestArtifactRejectsTraversal(t *testing.T) {
	artifact := createTarArtifact(t, "../escape")
	if err := validateArtifact(artifact, "tar.gz"); err == nil || !strings.Contains(err.Error(), "路径穿越") {
		t.Fatalf("err=%v", err)
	}
}

// TestArtifactAcceptsDotRootEntries 模拟 `tar -czf pkg.tar.gz .` 的产物:
// 包内含 "./" 根目录条目与 "./bin/app" 前缀条目, 均落在目标目录内,
// 不应被误判为路径穿越。
func TestArtifactAcceptsDotRootEntries(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "dotroot-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	dirHeader := &tar.Header{Name: "./", Mode: 0755, Typeflag: tar.TypeDir}
	if err := tw.WriteHeader(dirHeader); err != nil {
		t.Fatal(err)
	}
	data := []byte("artifact")
	if err := tw.WriteHeader(&tar.Header{Name: "./bin/app", Mode: 0755, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateArtifact(f.Name(), "tar.gz"); err != nil {
		t.Fatalf("dot-root tar should be accepted, got err=%v", err)
	}
}

func createArtifact(t *testing.T) string {
	t.Helper()
	return createTarArtifact(t, "bin/app")
}

func createTarArtifact(t *testing.T, name string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "artifact-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	data := []byte("artifact")
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}
