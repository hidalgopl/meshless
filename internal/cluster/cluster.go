package cluster

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type Cluster struct {
	ID     string
	Client *kubernetes.Clientset
}

func LoadFromDir(dir string) ([]Cluster, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var paths []string
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		paths = append(paths, filepath.Join(dir, e.Name()))
	}

	return LoadFromPaths(paths...)
}

func LoadFromPaths(paths ...string) ([]Cluster, error) {
	var clusters []Cluster
	seen := make(map[string]int)

	for _, p := range paths {
		id := filenameStem(p)

		client, err := newClient(p)
		if err != nil {
			slog.Warn("skipping kubeconfig",
				"path", p,
				"error", err,
			)
			continue
		}

		if idx, exists := seen[id]; exists {
			slog.Warn("duplicate cluster ID, overwriting",
				"id", id,
				"previous_path", paths[idx],
			)
			clusters[idx] = Cluster{ID: id, Client: client}
		} else {
			seen[id] = len(clusters)
			clusters = append(clusters, Cluster{ID: id, Client: client})
		}
	}

	if len(clusters) == 0 && len(paths) > 0 {
		return nil, errNoClustersLoaded
	}

	return clusters, nil
}

var errNoClustersLoaded = errorf("no clusters could be loaded")

func errorf(msg string) error { return &loadError{msg: msg} }

type loadError struct{ msg string }

func (e *loadError) Error() string { return e.msg }

func newClient(kubeconfigPath string) (*kubernetes.Clientset, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
}

func filenameStem(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}
