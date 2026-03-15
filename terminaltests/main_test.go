package terminaltests

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type builtBinaries struct {
	Dir          string
	RelayBin     string
	WrapperBin   string
	InspectBin   string
	PiBin        string
	RepoRoot     string
	DefaultEnv   []string
	ArtifactsDir string
}

var (
	buildOnce sync.Once
	buildRes  builtBinaries
	buildErr  error
)

func requireTerminalLab(t *testing.T) builtBinaries {
	t.Helper()
	if os.Getenv("WEAVE_TERMINAL_TESTS") == "" {
		t.Skip("set WEAVE_TERMINAL_TESTS=1 to run terminal lab scenarios")
	}
	buildOnce.Do(func() {
		buildRes, buildErr = buildBinaries()
	})
	if buildErr != nil {
		t.Fatalf("build terminal lab binaries: %v", buildErr)
	}
	if _, err := exec.LookPath(buildRes.PiBin); err != nil {
		t.Skipf("pi not available: %v", err)
	}
	return buildRes
}

func buildBinaries() (builtBinaries, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return builtBinaries{}, err
	}
	repoRoot := filepath.Dir(cwd)
	dir, err := os.MkdirTemp("", "weave-terminal-build-")
	if err != nil {
		return builtBinaries{}, err
	}
	artifactsDir := os.Getenv("WEAVE_TERMINAL_ARTIFACTS_DIR")
	if artifactsDir == "" {
		artifactsDir = filepath.Join(repoRoot, ".artifacts", "terminaltests")
	}
	bins := builtBinaries{
		Dir:          dir,
		RelayBin:     filepath.Join(dir, "weave-relay"),
		WrapperBin:   filepath.Join(dir, "weave-wrapper"),
		InspectBin:   filepath.Join(dir, "weave-inspect"),
		PiBin:        "pi",
		RepoRoot:     repoRoot,
		ArtifactsDir: artifactsDir,
		DefaultEnv: []string{
			"GO111MODULE=on",
		},
	}
	for _, target := range []struct{ out, pkg string }{
		{bins.RelayBin, "./cmd/weave-relay"},
		{bins.WrapperBin, "./cmd/weave-wrapper"},
		{bins.InspectBin, "./cmd/weave-inspect"},
	} {
		cmd := exec.Command("go", "build", "-o", target.out, target.pkg)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(), bins.DefaultEnv...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return builtBinaries{}, fmt.Errorf("build %s: %w\n%s", target.pkg, err, string(out))
		}
	}
	return bins, nil
}

type relayCluster struct {
	RunDir     string
	SessionDir string
	Artifacts  string
	RelayURL   string
	Token      string
	RelayCmd   *exec.Cmd
	Bins       builtBinaries
}

func startRelayCluster(t *testing.T, bins builtBinaries) *relayCluster {
	t.Helper()
	port := freePort(t)
	runDir := filepath.Join(bins.ArtifactsDir, sanitizeName(t.Name())+"-"+time.Now().UTC().Format("20060102-150405.000"))
	sessionDir := filepath.Join(runDir, "sessions")
	artifactsDir := filepath.Join(runDir, "artifacts")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	relayURL := fmt.Sprintf("ws://127.0.0.1:%d/ws", port)
	token := fmt.Sprintf("dev-token-%d", time.Now().UnixNano())
	cmd := exec.Command(bins.RelayBin,
		"--addr", fmt.Sprintf("127.0.0.1:%d", port),
		"--public-url", relayURL,
		"--token", token,
		"--wrapper-bin", bins.WrapperBin,
		"--pi-bin", bins.PiBin,
		"--pty-bin", bins.PiBin,
		"--session-dir", sessionDir,
	)
	cmd.Stdout = mustCreateFile(filepath.Join(runDir, "relay.out"))
	cmd.Stderr = mustCreateFile(filepath.Join(runDir, "relay.err"))
	if err := cmd.Start(); err != nil {
		t.Fatalf("start relay: %v", err)
	}
	cluster := &relayCluster{RunDir: runDir, SessionDir: sessionDir, RelayURL: relayURL, Token: token, RelayCmd: cmd, Bins: bins, Artifacts: artifactsDir}
	waitRelayReady(t, cluster)
	t.Cleanup(func() {
		if cluster.RelayCmd != nil && cluster.RelayCmd.Process != nil {
			_ = cluster.RelayCmd.Process.Kill()
			_, _ = cluster.RelayCmd.Process.Wait()
		}
	})
	writeJSON(filepath.Join(runDir, "cluster.json"), map[string]any{
		"relay_url":   relayURL,
		"token":       token,
		"session_dir": sessionDir,
		"goos":        runtime.GOOS,
		"goarch":      runtime.GOARCH,
	})
	return cluster
}

func waitRelayReady(t *testing.T, cluster *relayCluster) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.Command(cluster.Bins.InspectBin, "relay", "--relay", cluster.RelayURL, "--token", cluster.Token, "--identity", "probe", "sessions")
		cmd.Env = append(os.Environ(), "HOME="+cluster.RunDir)
		if err := cmd.Run(); err == nil {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("relay did not become ready: %s", cluster.RelayURL)
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func mustCreateFile(path string) *os.File {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		panic(err)
	}
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	return f
}

func sanitizeName(name string) string {
	repl := func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}
	return strings.Map(repl, name)
}

func writeJSON(path string, v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	_ = os.WriteFile(path, data, 0o644)
}
