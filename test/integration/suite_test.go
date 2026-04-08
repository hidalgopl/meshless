package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hidalgopl/meshless/internal/cluster"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/homedir"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

type testEnv struct {
	env    *envtest.Environment
	client *kubernetes.Clientset
}

func (te *testEnv) ctxFor(seconds int) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
	go func() {
		<-ctx.Done()
		cancel()
	}()
	return ctx
}

func newCluster(t *testing.T, te *testEnv, id string) cluster.Cluster {
	t.Helper()
	return cluster.Cluster{ID: id, Client: te.client}
}

func startTestEnv(t *testing.T) *testEnv {
	t.Helper()

	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		base := filepath.Join(homedir.HomeDir(), ".local", "share", "kubebuilder-envtest", "k8s")
		entries, err := os.ReadDir(base)
		if err != nil {
			t.Skipf("envtest not available: %v", err)
		}
		var latest string
		for _, e := range entries {
			if e.IsDir() && e.Name() > latest {
				latest = e.Name()
			}
		}
		if latest == "" {
			t.Skip("no envtest versions found")
		}
		os.Setenv("KUBEBUILDER_ASSETS", filepath.Join(base, latest))
	}

	e := &envtest.Environment{}

	cfg, err := e.Start()
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("failed to create kubernetes client: %v", err)
	}

	t.Cleanup(func() { e.Stop() })

	return &testEnv{env: e, client: client}
}

func createNS(t *testing.T, te *testEnv, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := te.client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create namespace %s: %v", name, err)
	}
}

// --- Endpoint helpers ---

func getEndpoints(t *testing.T, te *testEnv, ns, name string) *corev1.Endpoints {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	eps, err := te.client.CoreV1().Endpoints(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get endpoints %s/%s: %v", ns, name, err)
	}
	return eps
}

func endpointsExists(t *testing.T, te *testEnv, ns, name string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := te.client.CoreV1().Endpoints(ns).Get(ctx, name, metav1.GetOptions{})
	return err == nil
}

func createEndpoints(t *testing.T, te *testEnv, eps *corev1.Endpoints) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := te.client.CoreV1().Endpoints(eps.Namespace).Create(ctx, eps, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create endpoints %s/%s: %v", eps.Namespace, eps.Name, err)
	}
}

// --- EndpointSlice helpers ---

func getEndpointSlice(t *testing.T, te *testEnv, ns, name string) *discoveryv1.EndpointSlice {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	eps, err := te.client.DiscoveryV1().EndpointSlices(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get endpointslice %s/%s: %v", ns, name, err)
	}
	return eps
}

func endpointSliceExists(t *testing.T, te *testEnv, ns, name string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := te.client.DiscoveryV1().EndpointSlices(ns).Get(ctx, name, metav1.GetOptions{})
	return err == nil
}

func createEndpointSlice(t *testing.T, te *testEnv, eps *discoveryv1.EndpointSlice) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := te.client.DiscoveryV1().EndpointSlices(eps.Namespace).Create(ctx, eps, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create endpointslice %s/%s: %v", eps.Namespace, eps.Name, err)
	}
}

// --- Namespace helper ---

func nsForTest(t *testing.T) string {
	name := strings.ToLower(strings.ReplaceAll(t.Name(), "_", "-"))
	if len(name) > 58 {
		name = name[:58]
	}
	return "t-" + name
}
