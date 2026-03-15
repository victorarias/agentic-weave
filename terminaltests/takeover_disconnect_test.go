package terminaltests

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/victorarias/agentic-weave/internal/ptytest"
)

type encodingFixtureFile struct {
	Name      string            `json:"name"`
	Encodings []encodingFixture `json:"encodings"`
}

type encodingFixture struct {
	Name     string `json:"name"`
	Bytes    []byte `json:"bytes"`
	SendMode string `json:"send_mode"`
}

func TestTakeoverDisconnectEncodings(t *testing.T) {
	bins := requireTerminalLab(t)
	fixtures := loadEncodingFixtures(t, filepath.Join(bins.RepoRoot, "testdata", "terminal", "encodings", "ctrl-right-bracket.json"))
	cluster := startRelayCluster(t, bins)

	modes := []string{"plain"}
	if _, err := exec.LookPath("tmux"); err == nil {
		modes = append(modes, "tmux")
	}

	for _, mode := range modes {
		for _, fixture := range fixtures.Encodings {
			t.Run(mode+"/"+fixture.Name, func(t *testing.T) {
				sessionID := fmt.Sprintf("takeover-%s-%s", mode, fixture.Name)
				spawnSession(t, cluster, sessionID)
				artDir := filepath.Join(cluster.Artifacts, sanitizeName(t.Name()))
				sess := startInteractiveSession(t, cluster, mode, artDir, "relay", "--relay", cluster.RelayURL, "--token", cluster.Token, "--identity", "human-1", "--session", sessionID, "takeover")
				defer sess.Close()
				if err := sess.WaitFor("interactive mode active", 20*time.Second); err != nil {
					t.Fatal(err)
				}
				if err := sendFixture(sess, fixture); err != nil {
					t.Fatal(err)
				}
				if err := sess.WaitFor("disconnect requested", 10*time.Second); err != nil {
					t.Fatal(err)
				}
				waitForDetach(t, cluster, sessionID)
			})
		}
	}
}

func TestTUITakeoverDisconnectEncodings(t *testing.T) {
	bins := requireTerminalLab(t)
	fixtures := loadEncodingFixtures(t, filepath.Join(bins.RepoRoot, "testdata", "terminal", "encodings", "ctrl-right-bracket.json"))

	for _, fixture := range fixtures.Encodings {
		t.Run(fixture.Name, func(t *testing.T) {
			cluster := startRelayCluster(t, bins)
			artDir := filepath.Join(cluster.Artifacts, sanitizeName(t.Name()))
			sess := startInteractiveSession(t, cluster, "plain", artDir, "relay", "--relay", cluster.RelayURL, "--token", cluster.Token, "--identity", "human-1", "tui")
			defer sess.Close()
			if err := sess.WaitFor("weave-inspect relay tui", 20*time.Second); err != nil {
				t.Fatal(err)
			}
			if err := sess.SendString("n"); err != nil {
				t.Fatal(err)
			}
			if err := sess.WaitFor("status: action complete", 30*time.Second); err != nil {
				t.Fatal(err)
			}
			if err := sess.WaitFor("selected: tui-", 30*time.Second); err != nil {
				t.Fatal(err)
			}
			if err := sess.SendString("t"); err != nil {
				t.Fatal(err)
			}
			if err := sess.WaitFor("interactive mode active", 30*time.Second); err != nil {
				t.Fatal(err)
			}
			if err := sendFixture(sess, fixture); err != nil {
				t.Fatal(err)
			}
			if err := sess.WaitFor("returned from child command", 15*time.Second); err != nil {
				t.Fatal(err)
			}
			if err := sess.SendString("q"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func loadEncodingFixtures(t *testing.T, path string) encodingFixtureFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixtures encodingFixtureFile
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func spawnSession(t *testing.T, cluster *relayCluster, sessionID string) {
	t.Helper()
	cmd := exec.Command(cluster.Bins.InspectBin, "relay", "--relay", cluster.RelayURL, "--token", cluster.Token, "--identity", "orch-1", "--session", sessionID, "spawn")
	cmd.Env = append(os.Environ(), "HOME="+cluster.RunDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("spawn session: %v\n%s", err, string(out))
	}
}

func waitForDetach(t *testing.T, cluster *relayCluster, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.Command(cluster.Bins.InspectBin, "relay", "--relay", cluster.RelayURL, "--token", cluster.Token, "--identity", "orch-1", "--session", sessionID, "status")
		cmd.Env = append(os.Environ(), "HOME="+cluster.RunDir)
		out, err := cmd.CombinedOutput()
		if err == nil {
			text := string(out)
			if !strings.Contains(text, "attached_mode=") && strings.Contains(text, "runtime_transport=rpc") {
				return
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("session %s did not detach/return to rpc in time", sessionID)
}

func startInteractiveSession(t *testing.T, cluster *relayCluster, mode, artifactDir string, inspectArgs ...string) *ptytest.Session {
	t.Helper()
	homeDir := filepath.Join(artifactDir, "home")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + homeDir, "TERM=xterm-256color"}
	meta := map[string]any{"mode": mode, "relay_url": cluster.RelayURL, "scenario": t.Name(), "env": env}
	if mode == "tmux" {
		commandLine := shellJoin(append([]string{cluster.Bins.InspectBin}, inspectArgs...))
		return mustStartPTY(t, "tmux", []string{"-L", sanitizeName(t.Name()), "-f", "/dev/null", "new-session", "-s", "lab", commandLine}, env, artifactDir, meta)
	}
	return mustStartPTY(t, cluster.Bins.InspectBin, inspectArgs, env, artifactDir, meta)
}

func mustStartPTY(t *testing.T, command string, args, env []string, artifactDir string, meta map[string]any) *ptytest.Session {
	t.Helper()
	sess, err := ptytest.Start(command, args, ptytest.Options{Env: env, Rows: 40, Cols: 120, ArtifactDir: artifactDir, Metadata: meta})
	if err != nil {
		t.Fatalf("start PTY session %s: %v", command, err)
	}
	return sess
}

func sendFixture(sess *ptytest.Session, fixture encodingFixture) error {
	switch fixture.SendMode {
	case "split", "":
		return sess.SendChunked(fixture.Bytes, 15*time.Millisecond)
	default:
		return sess.Send(fixture.Bytes)
	}
}

func shellJoin(parts []string) string {
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			quoted = append(quoted, "''")
			continue
		}
		quoted = append(quoted, "'"+strings.ReplaceAll(p, "'", `'"'"'`)+"'")
	}
	return strings.Join(quoted, " ")
}
