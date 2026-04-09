//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

var (
	getOpts    = metav1.GetOptions{}
	deleteOpts = metav1.DeleteOptions{}
	listOpts   = metav1.ListOptions{}

	vclusterBinary = envOr("MESHLESS_VCLUSTER_BIN", "/usr/local/bin/vcluster")
	goBinary       = envOr("MESHLESS_GO_BIN", "go")
	kubectlBinary  = envOr("MESHLESS_KUBECTL_BIN", "kubectl")

	// When set, clusters are pre-provisioned (e.g., by setup-vind in CI).
	// CreateCluster will connect to existing clusters instead of creating new ones.
	preprovisionedClusters = envOr("MESHLESS_CLUSTER_NAMES", "")
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

const (
	meshlessSyncInterval = 3 * time.Second

	timeoutNodeReady       = 120 * time.Second
	timeoutPodReady        = 60 * time.Second
	timeoutLoadBalancerIP  = 120 * time.Second
	timeoutEndpointsSynced = 60 * time.Second
	timeoutCurlResponse    = 60 * time.Second
	timeoutEndpointsClean  = 30 * time.Second

	pollInterval = 2 * time.Second
)

// Cluster represents a vind (vCluster in Docker) cluster created for testing.
type Cluster struct {
	Name string
	// KubeconfigPath is the path to the kubeconfig file for this cluster.
	KubeconfigPath string
	// Client is a Kubernetes client for this cluster.
	Client *kubernetes.Clientset
	// RawConfig is the parsed kubeconfig for this cluster.
	RawConfig clientcmdapi.Config
}

// Environment holds all resources created for an e2e test run.
type Environment struct {
	NetworkName   string
	KubeconfigDir string
	Clusters      []*Cluster
	t             *testing.T
}

// NewEnvironment creates a shared Docker network and prepares a temp directory
// for kubeconfigs. Does not create any clusters yet.
// In CI mode (MESHLESS_CLUSTER_NAMES set), skips network creation and uses
// the default kubeconfig.
func NewEnvironment(t *testing.T, networkPrefix string) *Environment {
	t.Helper()

	env := &Environment{
		t: t,
	}

	if preprovisionedClusters == "" {
		env.NetworkName = networkPrefix + "-" + randomSuffix(t)
		env.KubeconfigDir = t.TempDir()

		t.Cleanup(func() {
			env.teardown()
		})
	} else {
		home, _ := os.UserHomeDir()
		env.KubeconfigDir = filepath.Join(home, ".kube")
	}

	return env
}

// CreateCluster creates a single vind cluster in the shared Docker network
// and retrieves its kubeconfig.
// In CI mode (MESHLESS_CLUSTER_NAMES set), connects to an existing cluster
// and extracts its kubeconfig from the default kubeconfig file.
func (e *Environment) CreateCluster(name string) *Cluster {
	e.t.Helper()

	if preprovisionedClusters != "" {
		return e.connectToExistingCluster(name)
	}

	e.t.Logf("creating vind cluster %s (network: %s)", name, e.NetworkName)

	// Create cluster without connecting (don't pollute local kubeconfig).
	// Use --connect=false to skip automatic connection.
	// --privileged is required for the standalone join-node to load kernel
	// modules (bridge, br_netfilter) for pod networking.
	run(e.t,
		vclusterBinary, "create", name,
		"--driver", "docker",
		"--connect=false",
		"--set", "experimental.docker.args[0]=--privileged",
		"--set", fmt.Sprintf("experimental.docker.network=%s", e.NetworkName),
	)

	kubeconfigPath := filepath.Join(e.KubeconfigDir, name+".yaml")

	// Extract kubeconfig. --print outputs to stdout, --silent suppresses
	// progress messages.
	e.t.Logf("extracting kubeconfig for cluster %s", name)
	out := runOutput(e.t,
		vclusterBinary, "connect", name,
		"--driver", "docker",
		"--print",
		"--silent",
	)

	if err := os.WriteFile(kubeconfigPath, out, 0o644); err != nil {
		e.t.Fatalf("failed to write kubeconfig for %s: %v", name, err)
	}

	// Parse and build client.
	config, err := clientcmd.Load(out)
	if err != nil {
		e.t.Fatalf("failed to parse kubeconfig for %s: %v", name, err)
	}

	restConfig, err := clientcmd.NewDefaultClientConfig(*config, nil).ClientConfig()
	if err != nil {
		e.t.Fatalf("failed to create rest config for %s: %v", name, err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		e.t.Fatalf("failed to create kubernetes client for %s: %v", name, err)
	}

	cluster := &Cluster{
		Name:           name,
		KubeconfigPath: kubeconfigPath,
		Client:         clientset,
		RawConfig:      *config,
	}
	e.Clusters = append(e.Clusters, cluster)

	e.t.Cleanup(func() {
		e.t.Logf("deleting vind cluster %s", name)
		// Ignore errors — cluster may already be gone.
		exec.Command(vclusterBinary, "delete", name,
			"--driver", "docker",
			"--ignore-not-found",
		).Run()
	})

	return cluster
}

// Kubectl runs a kubectl command against a specific cluster using its kubeconfig.
func (e *Environment) Kubectl(cluster *Cluster, args ...string) string {
	e.t.Helper()
	fullArgs := append([]string{
		"--kubeconfig", cluster.KubeconfigPath,
		"--namespace", "default",
	}, args...)
	return string(runOutput(e.t, kubectlBinary, fullArgs...))
}

// KubectlApply applies a manifest to a cluster.
func (e *Environment) KubectlApply(cluster *Cluster, yaml string) {
	e.t.Helper()
	e.t.Logf("applying manifest to cluster %s", cluster.Name)

	cmd := exec.Command(kubectlBinary,
		append([]string{
			"--kubeconfig", cluster.KubeconfigPath,
			"--namespace", "default",
			"apply", "-f", "-",
		})...)
	cmd.Stdin = strings.NewReader(yaml)
	runCmd(e.t, cmd)
}

// KubectlDelete deletes resources matching a manifest from a cluster.
func (e *Environment) KubectlDelete(cluster *Cluster, yaml string) {
	e.t.Helper()
	e.t.Logf("deleting manifest from cluster %s", cluster.Name)

	cmd := exec.Command(kubectlBinary,
		append([]string{
			"--kubeconfig", cluster.KubeconfigPath,
			"--namespace", "default",
			"delete", "-f", "-",
			"--ignore-not-found",
		})...)
	cmd.Stdin = strings.NewReader(yaml)
	runCmd(e.t, cmd)
}

// WaitForService waits for a service to exist and have at least one endpoint.
func (e *Environment) WaitForService(cluster *Cluster, svcName string, timeout time.Duration) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			e.t.Fatalf("timed out waiting for service %s endpoints in cluster %s", svcName, cluster.Name)
		case <-time.After(pollInterval):
		}

		svc, err := cluster.Client.CoreV1().Services("default").Get(ctx, svcName, getOpts)
		if err != nil {
			e.t.Logf("service %s not ready yet: %v", svcName, err)
			continue
		}

		// For LoadBalancer type, check that external IP is assigned.
		if svc.Spec.Type == "LoadBalancer" {
			if len(svc.Status.LoadBalancer.Ingress) > 0 {
				ingress := svc.Status.LoadBalancer.Ingress[0]
				e.t.Logf("service %s has LoadBalancer IP: %s", svcName, ingress.IP)
				return
			}
			e.t.Logf("service %s LoadBalancer IP not assigned yet", svcName)
			continue
		}

		// For other types, check that endpoints exist.
		eps, err := cluster.Client.CoreV1().Endpoints("default").Get(ctx, svcName, getOpts)
		if err != nil {
			e.t.Logf("endpoints %s not ready yet: %v", svcName, err)
			continue
		}

		hasAddresses := false
		for _, subset := range eps.Subsets {
			if len(subset.Addresses) > 0 {
				hasAddresses = true
				break
			}
		}
		if hasAddresses {
			e.t.Logf("service %s has endpoints", svcName)
			return
		}
		e.t.Logf("service %s has no endpoint addresses yet", svcName)
	}
}

// WaitForEndpoints waits for a service to have endpoints pointing to a
// specific number of addresses. This is used to verify meshless synced
// endpoints from a remote cluster.
func (e *Environment) WaitForEndpoints(cluster *Cluster, svcName string, wantAddresses int, timeout time.Duration) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			e.t.Fatalf("timed out waiting for %d endpoints for service %s in cluster %s", wantAddresses, svcName, cluster.Name)
		case <-time.After(pollInterval):
		}

		eps, err := cluster.Client.CoreV1().Endpoints("default").Get(ctx, svcName, getOpts)
		if err != nil {
			e.t.Logf("endpoints %s not found: %v", svcName, err)
			continue
		}

		addrCount := 0
		for _, subset := range eps.Subsets {
			addrCount += len(subset.Addresses)
		}

		e.t.Logf("service %s has %d endpoint addresses (want %d)", svcName, addrCount, wantAddresses)
		if addrCount >= wantAddresses {
			return
		}
	}
}

// CurlFromCluster runs curl inside a pod in the given cluster and returns
// the response body. The url should be a service FQDN with port.
func (e *Environment) CurlFromCluster(cluster *Cluster, curlerPod, url string, timeout time.Duration) string {
	e.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			cluster.Client.CoreV1().Pods("default").Delete(ctx, curlerPod, deleteOpts)
			e.t.Fatalf("timed out waiting for curl from %s to %s in cluster %s", curlerPod, url, cluster.Name)
		case <-time.After(pollInterval):
		}

		// Run curl via kubectl exec. Use --connect-timeout to avoid hanging.
		cmd := exec.Command(kubectlBinary,
			"--kubeconfig", cluster.KubeconfigPath,
			"--namespace", "default",
			"exec", curlerPod, "--",
			"curl", "-sf", "--connect-timeout", "2", url,
		)

		out, err := cmd.CombinedOutput()
		if err != nil {
			e.t.Logf("curl from %s to %s failed: %v (output: %s)", curlerPod, url, err, strings.TrimSpace(string(out)))
			continue
		}

		return string(out)
	}
}

func (e *Environment) connectToExistingCluster(name string) *Cluster {
	e.t.Helper()
	e.t.Logf("connecting to pre-provisioned cluster %s", name)

	defaultKubeconfig := filepath.Join(e.KubeconfigDir, "config")

	out := runOutput(e.t,
		vclusterBinary, "connect", name,
		"--driver", "docker",
		"--print",
		"--silent",
		"--kubeconfig", defaultKubeconfig,
	)

	kubeconfigPath := filepath.Join(e.KubeconfigDir, name+".yaml")
	if err := os.WriteFile(kubeconfigPath, out, 0o644); err != nil {
		e.t.Fatalf("failed to write kubeconfig for %s: %v", name, err)
	}

	config, err := clientcmd.Load(out)
	if err != nil {
		e.t.Fatalf("failed to parse kubeconfig for %s: %v", name, err)
	}

	restConfig, err := clientcmd.NewDefaultClientConfig(*config, nil).ClientConfig()
	if err != nil {
		e.t.Fatalf("failed to create rest config for %s: %v", name, err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		e.t.Fatalf("failed to create kubernetes client for %s: %v", name, err)
	}

	cluster := &Cluster{
		Name:           name,
		KubeconfigPath: kubeconfigPath,
		Client:         clientset,
		RawConfig:      *config,
	}
	e.Clusters = append(e.Clusters, cluster)

	return cluster
}

func (e *Environment) teardown() {
	e.t.Logf("tearing down e2e environment")

	for _, c := range e.Clusters {
		e.t.Logf("ensuring cluster %s is deleted", c.Name)
		exec.Command(vclusterBinary, "delete", c.Name,
			"--driver", "docker",
			"--ignore-not-found",
		).Run()
	}
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	// Ignore read error — crypto/rand is always available on supported platforms.
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// exec helpers.

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	runCmd(t, exec.Command(name, args...))
}

func runOutput(t *testing.T, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Stderr = new(bytes.Buffer)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %s: %v\nstderr: %s", name, strings.Join(args, " "), err, cmd.Stderr)
	}
	return out
}

func runCmd(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	cmd.Stderr = new(bytes.Buffer)
	cmd.Stdout = new(bytes.Buffer)
	if err := cmd.Run(); err != nil {
		stderr := cmd.Stderr.(*bytes.Buffer)
		t.Fatalf("%s: %v\nstderr: %s", strings.Join(cmd.Args, " "), err, stderr.String())
	}
}
