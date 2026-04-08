//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"embed"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"
)

//go:embed testdata/*.yaml
var testdataFS embed.FS

var (
	testdataTemplates = template.Must(template.ParseFS(testdataFS, "testdata/*.yaml"))

	testNamespace = "default"
)

// ── Template data types ──────────────────────────────────────────────────

type nginxData struct {
	ClusterName string
	Namespace   string
}

type serviceData struct {
	ServiceName    string
	Namespace      string
	SelectorLabels string
}

type curlerData struct {
	Name      string
	Namespace string
}

// ── Template render helpers ──────────────────────────────────────────────

func renderTemplate(t *testing.T, name string, data any) string {
	t.Helper()
	var buf bytes.Buffer
	if err := testdataTemplates.ExecuteTemplate(&buf, name, data); err != nil {
		t.Fatalf("failed to render template %s: %v", name, err)
	}
	return buf.String()
}

// ── Test ─────────────────────────────────────────────────────────────────

// TestMeshlessSync verifies the full meshless flow:
//
//  1. Create 3 vind clusters in a shared Docker network
//  2. Deploy nginx pods (with unique response per cluster) + LoadBalancer services
//  3. Build and run meshless
//  4. Verify cross-cluster DNS resolution via curl
//  5. Run meshless cleanup
//  6. Verify endpoints are removed
func TestMeshlessSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	// ── Step 1: Create environment ──────────────────────────────────────
	env := NewEnvironment(t, "meshless")

	clusterA := env.CreateCluster("meshless-a")
	clusterB := env.CreateCluster("meshless-b")
	clusterC := env.CreateCluster("meshless-c")

	for _, c := range []*Cluster{clusterA, clusterB, clusterC} {
		env.WaitForNodes(c, timeoutNodeReady)
	}

	// ── Step 2: Deploy nginx + services ─────────────────────────────────
	// Each cluster gets its own nginx deployment with a unique response.
	// All three services (first, second, third) are created in every cluster,
	// but only the "local" service has a real backend pod.

	clusterServices := map[*Cluster]string{
		clusterA: "first",
		clusterB: "second",
		clusterC: "third",
	}

	for cluster, localSvc := range clusterServices {
		// Deploy nginx with a unique index.html identifying the cluster.
		nginxYAML := renderTemplate(t, "nginx-deployment.yaml", nginxData{
			ClusterName: cluster.Name,
			Namespace:   testNamespace,
		})
		env.KubectlApply(cluster, nginxYAML)

		env.WaitForDeploymentReady(cluster, "nginx-"+cluster.Name, timeoutPodReady)

		// Create all three LoadBalancer services in every cluster.
		// Only the local one gets real endpoints via the nginx pod selector.
		for _, svcName := range []string{"first", "second", "third"} {
			sd := serviceData{
				ServiceName: svcName,
				Namespace:   testNamespace,
			}
			if svcName == localSvc {
				sd.SelectorLabels = "app: nginx-" + cluster.Name
			}
			svcYAML := renderTemplate(t, "service.yaml", sd)
			env.KubectlApply(cluster, svcYAML)
		}

		env.WaitForLoadBalancerIP(cluster, localSvc, timeoutLoadBalancerIP)
	}

	// ── Step 3: Build and run meshless ──────────────────────────────────
	meshlessBin := buildMeshless(t)
	meshlessCancel := runMeshless(t, meshlessBin, env.KubeconfigDir, testNamespace)

	t.Cleanup(func() {
		meshlessCancel()
	})

	// Wait for meshless to sync endpoints (a couple of reconcile ticks).
	time.Sleep(meshlessSyncInterval * 2)

	// ── Step 4: Verify cross-cluster resolution ────────────────────────
	expected := map[string]string{
		"first":  clusterA.Name,
		"second": clusterB.Name,
		"third":  clusterC.Name,
	}

	curlerPod := "curler"
	for _, cluster := range []*Cluster{clusterA, clusterB, clusterC} {
		curlerYAML := renderTemplate(t, "curler.yaml", curlerData{
			Name:      curlerPod,
			Namespace: testNamespace,
		})
		env.KubectlApply(cluster, curlerYAML)
		env.WaitForPodReady(cluster, curlerPod, timeoutPodReady)

		for svcName, wantResponse := range expected {
			url := "http://" + svcName + "." + testNamespace + ".svc.cluster.local"
			got := env.CurlFromCluster(cluster, curlerPod, url, timeoutCurlResponse)
			got = strings.TrimSpace(got)

			if !strings.Contains(got, wantResponse) {
				t.Errorf("cluster %s: curl %s got %q, want response containing %q",
					cluster.Name, url, got, wantResponse)
			} else {
				t.Logf("cluster %s: curl %s → %q ✓", cluster.Name, url, got)
			}
		}
	}

	// ── Step 5: Cleanup ────────────────────────────────────────────────
	meshlessCancel()
	runCleanup(t, meshlessBin, env.KubeconfigDir, testNamespace)

	// ── Step 6: Verify endpoints removed ───────────────────────────────
	for _, cluster := range []*Cluster{clusterA, clusterB, clusterC} {
		for _, svcName := range []string{"first", "second", "third"} {
			if svcName == clusterServices[cluster] {
				continue // local service endpoints untouched by meshless
			}
			env.WaitForNoEndpoints(cluster, svcName, timeoutEndpointsClean)
		}
	}
}

// ── Meshless binary helpers ──────────────────────────────────────────────

// buildMeshless compiles the meshless binary and returns its path.
func buildMeshless(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "meshless")
	projectRoot := filepath.Join("..", "..")
	cmd := exec.Command(goBinary, "build", "-C", projectRoot, "-o", bin, "./cmd/meshless")
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build meshless: %v", err)
	}
	return bin
}

// runMeshless starts meshless in the background and returns a cancel function.
func runMeshless(t *testing.T, bin, kubeconfigDir, namespace string) context.CancelFunc {
	t.Helper()
	t.Log("starting meshless")

	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, bin,
		"--kubeconfig-dir", kubeconfigDir,
		"--namespace", namespace,
		"--sync-interval", meshlessSyncInterval.String(),
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("failed to start meshless: %v", err)
	}

	go func() {
		_ = cmd.Wait()
	}()

	t.Cleanup(func() {
		t.Log("stopping meshless")
		cancel()
	})

	return cancel
}

// runCleanup runs meshless cleanup subcommand.
func runCleanup(t *testing.T, bin, kubeconfigDir, namespace string) {
	t.Helper()
	t.Log("running meshless cleanup")

	cmd := exec.Command(bin, "cleanup",
		"--kubeconfig-dir", kubeconfigDir,
		"--namespace", namespace,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("meshless cleanup failed: %v", err)
	}
}

// ── Environment helpers (cluster-level waits) ────────────────────────────

// WaitForNodes waits until the cluster reports at least one node.
func (e *Environment) WaitForNodes(cluster *Cluster, timeout time.Duration) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			e.t.Fatalf("timed out waiting for nodes in cluster %s", cluster.Name)
		case <-time.After(pollInterval):
		}

		nodes, err := cluster.Client.CoreV1().Nodes().List(ctx, listOpts)
		if err != nil {
			e.t.Logf("cluster %s: error listing nodes: %v", cluster.Name, err)
			continue
		}

		if len(nodes.Items) > 0 {
			e.t.Logf("cluster %s: %d node(s) ready", cluster.Name, len(nodes.Items))
			return
		}
	}
}

// WaitForDeploymentReady waits until a deployment has at least one ready replica.
func (e *Environment) WaitForDeploymentReady(cluster *Cluster, deployName string, timeout time.Duration) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			e.t.Fatalf("timed out waiting for deployment %s in cluster %s", deployName, cluster.Name)
		case <-time.After(pollInterval):
		}

		deploy, err := cluster.Client.AppsV1().Deployments(testNamespace).Get(ctx, deployName, getOpts)
		if err != nil {
			e.t.Logf("deployment %s not found yet: %v", deployName, err)
			continue
		}

		if deploy.Status.ReadyReplicas > 0 {
			e.t.Logf("deployment %s: %d ready replica(s)", deployName, deploy.Status.ReadyReplicas)
			return
		}
	}
}

// WaitForPodReady waits until a pod is in the Running phase.
func (e *Environment) WaitForPodReady(cluster *Cluster, podName string, timeout time.Duration) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			e.t.Fatalf("timed out waiting for pod %s in cluster %s", podName, cluster.Name)
		case <-time.After(pollInterval):
		}

		pod, err := cluster.Client.CoreV1().Pods(testNamespace).Get(ctx, podName, getOpts)
		if err != nil {
			e.t.Logf("pod %s not found yet: %v", podName, err)
			continue
		}

		if pod.Status.Phase == "Running" {
			e.t.Logf("pod %s is running", podName)
			return
		}
	}
}

// WaitForLoadBalancerIP waits until a LoadBalancer service has an external IP.
func (e *Environment) WaitForLoadBalancerIP(cluster *Cluster, svcName string, timeout time.Duration) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			e.t.Fatalf("timed out waiting for LoadBalancer IP for %s in cluster %s", svcName, cluster.Name)
		case <-time.After(pollInterval):
		}

		svc, err := cluster.Client.CoreV1().Services(testNamespace).Get(ctx, svcName, getOpts)
		if err != nil {
			e.t.Logf("service %s not found: %v", svcName, err)
			continue
		}

		if len(svc.Status.LoadBalancer.Ingress) > 0 {
			ip := svc.Status.LoadBalancer.Ingress[0].IP
			e.t.Logf("service %s has LoadBalancer IP: %s", svcName, ip)
			return
		}
	}
}

// WaitForNoEndpoints waits until a service has zero endpoint addresses.
func (e *Environment) WaitForNoEndpoints(cluster *Cluster, svcName string, timeout time.Duration) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			e.t.Fatalf("timed out waiting for endpoints removal for %s in cluster %s", svcName, cluster.Name)
		case <-time.After(pollInterval):
		}

		eps, err := cluster.Client.CoreV1().Endpoints(testNamespace).Get(ctx, svcName, getOpts)
		if err != nil {
			e.t.Logf("endpoints %s not found (removed) in cluster %s", svcName, cluster.Name)
			return
		}

		addrCount := 0
		for _, subset := range eps.Subsets {
			addrCount += len(subset.Addresses)
		}

		if addrCount == 0 {
			e.t.Logf("service %s has no endpoints in cluster %s ✓", svcName, cluster.Name)
			return
		}
		e.t.Logf("service %s still has %d endpoint(s) in cluster %s", svcName, addrCount, cluster.Name)
	}
}
