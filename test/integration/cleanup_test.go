//nolint:staticcheck // corev1.Endpoints intentionally used for --sync-endpoints tests
package integration

import (
	"testing"

	"github.com/hidalgopl/meshless/internal/cluster"
	"github.com/hidalgopl/meshless/internal/endpoint"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// --- EndpointSlice cleanup tests ---

func TestCleanup_DeletesManagedEndpointSlice(t *testing.T) {
	te := startTestEnv(t)
	ns := nsForTest(t)
	createNS(t, te, ns)

	cl := newCluster(t, te, "c1")

	createEndpointSlice(t, te, &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "managed-es",
			Namespace: ns,
			Labels: map[string]string{
				"meshless.global/managed":    "true",
				discoveryv1.LabelServiceName: "some-svc",
				discoveryv1.LabelManagedBy:   "meshless",
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	})

	endpoint.Cleanup(te.ctxFor(10), []cluster.Cluster{cl}, []string{ns}, endpoint.SyncOptions{})

	if endpointSliceExists(t, te, ns, "managed-es") {
		t.Error("managed endpointslice should have been deleted")
	}
}

func TestCleanup_PreservesUnmanagedEndpointSlice(t *testing.T) {
	te := startTestEnv(t)
	ns := nsForTest(t)
	createNS(t, te, ns)

	cl := newCluster(t, te, "c1")

	createEndpointSlice(t, te, &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "k8s-native-es",
			Namespace: ns,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: "my-app",
				discoveryv1.LabelManagedBy:   "endpointslice-controller.k8s.io",
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	})

	endpoint.Cleanup(te.ctxFor(10), []cluster.Cluster{cl}, []string{ns}, endpoint.SyncOptions{})

	if !endpointSliceExists(t, te, ns, "k8s-native-es") {
		t.Error("unmanaged endpointslice should NOT have been deleted")
	}
}

// --- Endpoints cleanup tests (only when SyncEndpoints=true) ---

func TestCleanup_DeletesManagedEndpoints_WhenEnabled(t *testing.T) {
	te := startTestEnv(t)
	ns := nsForTest(t)
	createNS(t, te, ns)

	cl := newCluster(t, te, "c1")
	opts := endpoint.SyncOptions{SyncEndpoints: true}

	createEndpoints(t, te, &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "managed-eps",
			Namespace: ns,
			Labels:    map[string]string{"meshless.global/managed": "true"},
		},
		Subsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{IP: "10.0.0.1"}},
			Ports:     []corev1.EndpointPort{{Name: "http", Port: 80}},
		}},
	})

	endpoint.Cleanup(te.ctxFor(10), []cluster.Cluster{cl}, []string{ns}, opts)

	if endpointsExists(t, te, ns, "managed-eps") {
		t.Error("managed endpoints should have been deleted")
	}
}

func TestCleanup_DoesNotTouchEndpoints_WhenDisabled(t *testing.T) {
	te := startTestEnv(t)
	ns := nsForTest(t)
	createNS(t, te, ns)

	cl := newCluster(t, te, "c1")

	createEndpoints(t, te, &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "managed-eps",
			Namespace: ns,
			Labels:    map[string]string{"meshless.global/managed": "true"},
		},
		Subsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{IP: "10.0.0.1"}},
			Ports:     []corev1.EndpointPort{{Name: "http", Port: 80}},
		}},
	})

	// Default opts: SyncEndpoints=false, so Cleanup should NOT delete Endpoints.
	endpoint.Cleanup(te.ctxFor(10), []cluster.Cluster{cl}, []string{ns}, endpoint.SyncOptions{})

	if !endpointsExists(t, te, ns, "managed-eps") {
		t.Error("managed endpoints should NOT be deleted when SyncEndpoints is false")
	}
}

func TestCleanup_PreservesUnmanagedEndpoints(t *testing.T) {
	te := startTestEnv(t)
	ns := nsForTest(t)
	createNS(t, te, ns)

	cl := newCluster(t, te, "c1")
	opts := endpoint.SyncOptions{SyncEndpoints: true}

	createEndpoints(t, te, &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubernetes-native-eps",
			Namespace: ns,
			Labels:    map[string]string{"app": "my-app"},
		},
		Subsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{IP: "10.0.0.5"}},
			Ports:     []corev1.EndpointPort{{Name: "http", Port: 80}},
		}},
	})

	endpoint.Cleanup(te.ctxFor(10), []cluster.Cluster{cl}, []string{ns}, opts)

	if !endpointsExists(t, te, ns, "kubernetes-native-eps") {
		t.Error("unmanaged endpoints should NOT have been deleted")
	}
}

// --- Both resources cleaned up together ---

func TestCleanup_DeletesBoth_WhenEnabled(t *testing.T) {
	te := startTestEnv(t)
	ns := nsForTest(t)
	createNS(t, te, ns)

	cl := newCluster(t, te, "c1")
	opts := endpoint.SyncOptions{SyncEndpoints: true}

	createEndpointSlice(t, te, &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "managed-es",
			Namespace: ns,
			Labels: map[string]string{
				"meshless.global/managed":    "true",
				discoveryv1.LabelServiceName: "svc",
				discoveryv1.LabelManagedBy:   "meshless",
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	})
	createEndpoints(t, te, &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "managed-eps",
			Namespace: ns,
			Labels:    map[string]string{"meshless.global/managed": "true"},
		},
		Subsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{IP: "10.0.0.1"}},
			Ports:     []corev1.EndpointPort{{Name: "http", Port: 80}},
		}},
	})

	endpoint.Cleanup(te.ctxFor(10), []cluster.Cluster{cl}, []string{ns}, opts)

	if endpointSliceExists(t, te, ns, "managed-es") {
		t.Error("managed endpointslice should have been deleted")
	}
	if endpointsExists(t, te, ns, "managed-eps") {
		t.Error("managed endpoints should have been deleted")
	}
}
