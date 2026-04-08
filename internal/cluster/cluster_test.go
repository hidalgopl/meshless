package cluster

import (
	"os"
	"path/filepath"
	"testing"
)

const fakeKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://localhost:6443
  name: test
contexts:
- context:
    cluster: test
    user: test
  name: test
current-context: test
users:
- name: test
  user: {}
`

func writeFakeConfig(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(fakeKubeconfig), 0644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestLoadFromDir(t *testing.T) {
	dir := t.TempDir()

	writeFakeConfig(t, dir, "meshless-a.yaml")
	writeFakeConfig(t, dir, "meshless-b.yaml")
	writeFakeConfig(t, dir, "meshless-c.yaml")

	clusters, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(clusters) != 3 {
		t.Fatalf("expected 3 clusters, got %d", len(clusters))
	}

	ids := make(map[string]bool)
	for _, c := range clusters {
		ids[c.ID] = true
	}
	for _, want := range []string{"meshless-a", "meshless-b", "meshless-c"} {
		if !ids[want] {
			t.Errorf("missing cluster ID %q", want)
		}
	}
	for _, c := range clusters {
		if c.Client == nil {
			t.Errorf("cluster %q has nil client", c.ID)
		}
	}
}

func TestLoadFromDirSkipsDirectories(t *testing.T) {
	dir := t.TempDir()

	writeFakeConfig(t, dir, "meshless-a.yaml")
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	clusters, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}
	if clusters[0].ID != "meshless-a" {
		t.Errorf("expected ID %q, got %q", "meshless-a", clusters[0].ID)
	}
}

func TestLoadFromDirEmpty(t *testing.T) {
	dir := t.TempDir()

	clusters, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(clusters) != 0 {
		t.Fatalf("expected 0 clusters, got %d", len(clusters))
	}
}

func TestLoadFromDirWithInvalidFile(t *testing.T) {
	dir := t.TempDir()

	writeFakeConfig(t, dir, "meshless-good.yaml")

	invalidPath := filepath.Join(dir, "meshless-bad.yaml")
	if err := os.WriteFile(invalidPath, []byte("not a kubeconfig"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	writeFakeConfig(t, dir, "meshless-ok.yaml")

	clusters, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}

	ids := make(map[string]bool)
	for _, c := range clusters {
		ids[c.ID] = true
	}
	for _, want := range []string{"meshless-good", "meshless-ok"} {
		if !ids[want] {
			t.Errorf("missing cluster ID %q", want)
		}
	}
}

func TestLoadFromPaths(t *testing.T) {
	dir := t.TempDir()

	p1 := writeFakeConfig(t, dir, "cluster-east.yaml")
	p2 := writeFakeConfig(t, dir, "cluster-west.yaml")

	clusters, err := LoadFromPaths(p1, p2)
	if err != nil {
		t.Fatalf("LoadFromPaths: %v", err)
	}
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}

	if clusters[0].ID != "cluster-east" {
		t.Errorf("expected ID %q, got %q", "cluster-east", clusters[0].ID)
	}
	if clusters[1].ID != "cluster-west" {
		t.Errorf("expected ID %q, got %q", "cluster-west", clusters[1].ID)
	}
	for _, c := range clusters {
		if c.Client == nil {
			t.Errorf("cluster %q has nil client", c.ID)
		}
	}
}

func TestLoadFromPathsDuplicateID(t *testing.T) {
	dir := t.TempDir()

	p1 := writeFakeConfig(t, dir, "cluster-east.yaml")
	p2 := writeFakeConfig(t, dir, "cluster-east.yaml")

	clusters, err := LoadFromPaths(p1, p2)
	if err != nil {
		t.Fatalf("LoadFromPaths: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected 1 deduplicated cluster, got %d", len(clusters))
	}
	if clusters[0].ID != "cluster-east" {
		t.Errorf("expected ID %q, got %q", "cluster-east", clusters[0].ID)
	}
}
