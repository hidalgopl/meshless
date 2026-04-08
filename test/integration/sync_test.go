package integration

import (
	"testing"

	"github.com/hidalgopl/meshless/internal/cluster"
	"github.com/hidalgopl/meshless/internal/discover"
	"github.com/hidalgopl/meshless/internal/endpoint"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestSync_CreatesEndpointSliceOnConsumer(t *testing.T) {
	te := startTestEnv(t)
	ns := nsForTest(t)
	createNS(t, te, ns)

	cl := newCluster(t, te, "consumer")
	opts := endpoint.SyncOptions{} // default: EndpointSlices only

	observations := map[types.NamespacedName][]discover.Observation{
		{Namespace: ns, Name: "my-svc"}: {
			{ClusterID: "remote-provider", Addresses: []string{"10.0.0.1"}, Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			}},
			{ClusterID: "consumer", Addresses: nil},
		},
	}

	endpoint.Sync(te.ctxFor(10), []cluster.Cluster{cl}, observations, opts)

	// EndpointSlice should be created.
	es := getEndpointSlice(t, te, ns, "my-svc")
	if len(es.Endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(es.Endpoints))
	}
	if len(es.Endpoints[0].Addresses) != 1 || es.Endpoints[0].Addresses[0] != "10.0.0.1" {
		t.Errorf("expected address 10.0.0.1, got %v", es.Endpoints[0].Addresses)
	}
	if es.Labels[discoveryv1.LabelManagedBy] != "meshless" {
		t.Errorf("expected managed-by label meshless, got %q", es.Labels[discoveryv1.LabelManagedBy])
	}

	// Endpoints should NOT be created by default.
	if endpointsExists(t, te, ns, "my-svc") {
		t.Error("endpoints should not be created when SyncEndpoints is false")
	}
}

func TestSync_EndpointSliceLabels(t *testing.T) {
	te := startTestEnv(t)
	ns := nsForTest(t)
	createNS(t, te, ns)

	cl := newCluster(t, te, "consumer")

	observations := map[types.NamespacedName][]discover.Observation{
		{Namespace: ns, Name: "labeled-svc"}: {
			{ClusterID: "provider", Addresses: []string{"1.2.3.4"}, Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			}},
			{ClusterID: "consumer", Addresses: nil},
		},
	}

	endpoint.Sync(te.ctxFor(10), []cluster.Cluster{cl}, observations, endpoint.SyncOptions{})

	es := getEndpointSlice(t, te, ns, "labeled-svc")
	if es.Labels == nil {
		t.Fatal("endpointslice has no labels")
	}
	if es.Labels[discoveryv1.LabelServiceName] != "labeled-svc" {
		t.Errorf("expected service-name label, got %v", es.Labels[discoveryv1.LabelServiceName])
	}
	if es.Labels[discoveryv1.LabelManagedBy] != "meshless" {
		t.Errorf("expected managed-by label, got %v", es.Labels[discoveryv1.LabelManagedBy])
	}
}

func TestSync_SyncEndpointsFlag_CreatesBoth(t *testing.T) {
	te := startTestEnv(t)
	ns := nsForTest(t)
	createNS(t, te, ns)

	cl := newCluster(t, te, "consumer")
	opts := endpoint.SyncOptions{SyncEndpoints: true}

	observations := map[types.NamespacedName][]discover.Observation{
		{Namespace: ns, Name: "both-svc"}: {
			{ClusterID: "provider", Addresses: []string{"10.0.0.5"}, Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			}},
			{ClusterID: "consumer", Addresses: nil},
		},
	}

	endpoint.Sync(te.ctxFor(10), []cluster.Cluster{cl}, observations, opts)

	// Both should exist.
	es := getEndpointSlice(t, te, ns, "both-svc")
	if len(es.Endpoints) != 1 || es.Endpoints[0].Addresses[0] != "10.0.0.5" {
		t.Errorf("endpointslice: expected address 10.0.0.5, got %v", es.Endpoints)
	}

	eps := getEndpoints(t, te, ns, "both-svc")
	if len(eps.Subsets[0].Addresses) != 1 || eps.Subsets[0].Addresses[0].IP != "10.0.0.5" {
		t.Errorf("endpoints: expected address 10.0.0.5, got %v", eps.Subsets[0].Addresses)
	}
}

func TestSync_UpdatesEndpointSlice(t *testing.T) {
	te := startTestEnv(t)
	ns := nsForTest(t)
	createNS(t, te, ns)

	cl := newCluster(t, te, "consumer")
	nn := types.NamespacedName{Namespace: ns, Name: "update-svc"}

	observations := map[types.NamespacedName][]discover.Observation{
		nn: {
			{ClusterID: "provider", Addresses: []string{"10.0.0.1"}, Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			}},
			{ClusterID: "consumer", Addresses: nil},
		},
	}

	endpoint.Sync(te.ctxFor(10), []cluster.Cluster{cl}, observations, endpoint.SyncOptions{})
	es := getEndpointSlice(t, te, ns, "update-svc")
	if len(es.Endpoints) != 1 || es.Endpoints[0].Addresses[0] != "10.0.0.1" {
		t.Fatalf("initial: expected 10.0.0.1, got %v", es.Endpoints[0].Addresses)
	}

	observations[nn][0].Addresses = []string{"10.0.0.2", "10.0.0.3"}

	endpoint.Sync(te.ctxFor(10), []cluster.Cluster{cl}, observations, endpoint.SyncOptions{})
	es = getEndpointSlice(t, te, ns, "update-svc")
	if len(es.Endpoints) != 2 {
		t.Fatalf("after update: expected 2 endpoints, got %d", len(es.Endpoints))
	}
	if es.Endpoints[0].Addresses[0] != "10.0.0.2" || es.Endpoints[1].Addresses[0] != "10.0.0.3" {
		t.Errorf("after update: expected [10.0.0.2, 10.0.0.3], got %v", es.Endpoints)
	}
}

func TestSync_SkipsWhenNoProvider(t *testing.T) {
	te := startTestEnv(t)
	ns := nsForTest(t)
	createNS(t, te, ns)

	cl := newCluster(t, te, "consumer")

	observations := map[types.NamespacedName][]discover.Observation{
		{Namespace: ns, Name: "no-provider-svc"}: {
			{ClusterID: "consumer", Addresses: nil},
		},
	}

	endpoint.Sync(te.ctxFor(10), []cluster.Cluster{cl}, observations, endpoint.SyncOptions{})

	if endpointSliceExists(t, te, ns, "no-provider-svc") {
		t.Error("expected no endpointslice when no provider has addresses")
	}
}

func TestSync_SkipsProviderCluster(t *testing.T) {
	te := startTestEnv(t)
	ns := nsForTest(t)
	createNS(t, te, ns)

	cl := newCluster(t, te, "provider")

	observations := map[types.NamespacedName][]discover.Observation{
		{Namespace: ns, Name: "provider-svc"}: {
			{ClusterID: "provider", Addresses: []string{"10.0.0.1"}, Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			}},
		},
	}

	endpoint.Sync(te.ctxFor(10), []cluster.Cluster{cl}, observations, endpoint.SyncOptions{})

	if endpointSliceExists(t, te, ns, "provider-svc") {
		t.Error("expected no endpointslice on provider cluster itself")
	}
}
