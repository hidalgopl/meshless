package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hidalgopl/meshless/internal/cluster"
	"github.com/hidalgopl/meshless/internal/discover"
	"github.com/hidalgopl/meshless/internal/endpoint"
	"k8s.io/apimachinery/pkg/types"
)

const (
	defaultNamespace    = "default"
	defaultAnnotation   = "meshless.global/export"
	defaultSyncInterval = 1 * time.Second
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "cleanup":
		runCleanup(os.Args[2:])
	default:
		runSync(os.Args[1:])
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "meshless — cross-cluster service endpoint synchronizer")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  meshless [flags]          run sync loop (default)")
	fmt.Fprintln(os.Stderr, "  meshless cleanup [flags]  remove managed endpoints")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  --kubeconfig-dir string    directory of kubeconfig files")
	fmt.Fprintln(os.Stderr, "  --kubeconfig string        kubeconfig path (repeatable)")
	fmt.Fprintln(os.Stderr, "  --namespace string         namespace to watch (repeatable, default: default)")
	fmt.Fprintln(os.Stderr, "  --annotation string        annotation key (default: meshless.global/export)")
	fmt.Fprintln(os.Stderr, "  --sync-interval duration   reconcile interval (default: 1s)")
	fmt.Fprintln(os.Stderr, "  --sync-endpoints           also sync corev1.Endpoints (default: EndpointSlices only)")
}

type stringSlice []string

func (s *stringSlice) String() string     { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error { *s = append(*s, v); return nil }

type flags struct {
	dir           string
	kubeconfigs   []string
	namespaces    []string
	annotation    string
	interval      time.Duration
	syncEndpoints bool
}

func parseFlags(args []string) flags {
	var fs flags
	f := flag.NewFlagSet("meshless", flag.ExitOnError)
	f.StringVar(&fs.dir, "kubeconfig-dir", "", "directory of kubeconfig files")
	var kubeconfigSlice stringSlice
	f.Var(&kubeconfigSlice, "kubeconfig", "kubeconfig path (repeatable)")
	var namespaceSlice stringSlice
	f.Var(&namespaceSlice, "namespace", "namespace to watch (repeatable)")
	f.StringVar(&fs.annotation, "annotation", defaultAnnotation, "annotation key")
	f.DurationVar(&fs.interval, "sync-interval", defaultSyncInterval, "reconcile interval")
	f.BoolVar(&fs.syncEndpoints, "sync-endpoints", false, "also sync corev1.Endpoints")
	f.Parse(args) //nolint:errcheck // ExitOnError handles parse failures

	if len(namespaceSlice) == 0 {
		namespaceSlice = []string{defaultNamespace}
	}

	fs.kubeconfigs = []string(kubeconfigSlice)
	fs.namespaces = []string(namespaceSlice)
	return fs
}

func loadClusters(dir string, paths []string) ([]cluster.Cluster, error) {
	var clusters []cluster.Cluster
	var err error

	if dir != "" {
		clusters, err = cluster.LoadFromDir(dir)
	} else if len(paths) > 0 {
		clusters, err = cluster.LoadFromPaths(paths...)
	} else {
		home, _ := os.UserHomeDir()
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = home + "/.kube/config"
		}
		clusters, err = cluster.LoadFromPaths(kubeconfig)
	}

	return clusters, err
}

func runSync(args []string) {
	fs := parseFlags(args)

	clusters, err := loadClusters(fs.dir, fs.kubeconfigs)
	if err != nil {
		slog.Error("failed to load clusters", "error", err)
		os.Exit(1)
	}
	if len(clusters) == 0 {
		slog.Error("no clusters loaded")
		os.Exit(1)
	}

	opts := endpoint.SyncOptions{SyncEndpoints: fs.syncEndpoints}

	slog.Info("starting meshless sync",
		"clusters", len(clusters),
		"namespaces", fs.namespaces,
		"annotation", fs.annotation,
		"interval", fs.interval,
		"sync_endpoints", opts.SyncEndpoints,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	ticker := time.NewTicker(fs.interval)
	defer ticker.Stop()

	for {
		observations, err := discover.Observe(ctx, clusters, fs.namespaces, fs.annotation)
		if err != nil {
			slog.Error("discovery failed", "error", err)
		} else {
			endpoint.Sync(ctx, clusters, observations, opts)
			logObservations(observations)
		}

		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			return
		case <-ticker.C:
		}
	}
}

func runCleanup(args []string) {
	fs := parseFlags(args)

	clusters, err := loadClusters(fs.dir, fs.kubeconfigs)
	if err != nil {
		slog.Error("failed to load clusters", "error", err)
		os.Exit(1)
	}
	if len(clusters) == 0 {
		slog.Error("no clusters loaded")
		os.Exit(1)
	}

	opts := endpoint.SyncOptions{SyncEndpoints: fs.syncEndpoints}

	slog.Info("running meshless cleanup",
		"clusters", len(clusters),
		"namespaces", fs.namespaces,
		"sync_endpoints", opts.SyncEndpoints,
	)

	ctx := context.Background()
	endpoint.Cleanup(ctx, clusters, fs.namespaces, opts)

	slog.Info("cleanup complete")
}

func logObservations(observations map[types.NamespacedName][]discover.Observation) {
	for nsn, obs := range observations {
		var provider *discover.Observation
		consumers := 0
		for i := range obs {
			if len(obs[i].Addresses) > 0 {
				provider = &obs[i]
			} else {
				consumers++
			}
		}
		if provider != nil {
			slog.Debug("service synced",
				"service", nsn.Name,
				"namespace", nsn.Namespace,
				"provider", provider.ClusterID,
				"addresses", provider.Addresses,
				"consumers", consumers,
			)
		}
	}
}
