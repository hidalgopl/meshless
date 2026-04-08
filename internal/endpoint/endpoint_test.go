package endpoint

import (
	"encoding/json"
	"testing"

	"github.com/hidalgopl/meshless/internal/discover"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
)

// --- BuildEndpointSlice tests ---

func TestBuildEndpointSlice_Metadata(t *testing.T) {
	nn := types.NamespacedName{Namespace: "prod", Name: "my-svc"}
	obs := discover.Observation{
		ClusterID: "cluster-1",
		Addresses: []string{"10.0.0.1"},
		Ports: []corev1.ServicePort{
			{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
		},
	}

	eps := BuildEndpointSlice(nn, obs)

	if eps.Name != "my-svc" {
		t.Errorf("expected name %q, got %q", "my-svc", eps.Name)
	}
	if eps.Namespace != "prod" {
		t.Errorf("expected namespace %q, got %q", "prod", eps.Namespace)
	}
	if eps.APIVersion != "discovery.k8s.io/v1" {
		t.Errorf("expected apiVersion %q, got %q", "discovery.k8s.io/v1", eps.APIVersion)
	}
	if eps.Kind != "EndpointSlice" {
		t.Errorf("expected kind %q, got %q", "EndpointSlice", eps.Kind)
	}
	if eps.AddressType != discoveryv1.AddressTypeIPv4 {
		t.Errorf("expected addressType %q, got %q", discoveryv1.AddressTypeIPv4, eps.AddressType)
	}
	if eps.Labels[discoveryv1.LabelServiceName] != "my-svc" {
		t.Errorf("expected service-name label %q, got %q", "my-svc", eps.Labels[discoveryv1.LabelServiceName])
	}
	if eps.Labels[discoveryv1.LabelManagedBy] != "meshless" {
		t.Errorf("expected managed-by label %q, got %q", "meshless", eps.Labels[discoveryv1.LabelManagedBy])
	}
	v, ok := eps.Labels[managedLabelKey]
	if !ok || v != managedLabelVal {
		t.Errorf("expected managed label %q, got %q", managedLabelVal, v)
	}
}

func TestBuildEndpointSlice_Addresses(t *testing.T) {
	nn := types.NamespacedName{Namespace: "default", Name: "test"}
	obs := discover.Observation{
		Addresses: []string{"10.0.0.1"},
		Ports: []corev1.ServicePort{
			{Name: "grpc", Port: 9090, Protocol: corev1.ProtocolTCP},
		},
	}

	eps := BuildEndpointSlice(nn, obs)

	if len(eps.Endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(eps.Endpoints))
	}
	if len(eps.Endpoints[0].Addresses) != 1 || eps.Endpoints[0].Addresses[0] != "10.0.0.1" {
		t.Errorf("expected address [10.0.0.1], got %v", eps.Endpoints[0].Addresses)
	}
}

func TestBuildEndpointSlice_MultipleAddresses(t *testing.T) {
	nn := types.NamespacedName{Namespace: "default", Name: "multi"}
	obs := discover.Observation{
		Addresses: []string{"10.0.0.1", "10.0.0.2"},
		Ports: []corev1.ServicePort{
			{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
		},
	}

	eps := BuildEndpointSlice(nn, obs)

	if len(eps.Endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(eps.Endpoints))
	}
}

func TestBuildEndpointSlice_Ports(t *testing.T) {
	nn := types.NamespacedName{Namespace: "ns", Name: "svc"}
	obs := discover.Observation{
		Addresses: []string{"10.0.0.1"},
		Ports: []corev1.ServicePort{
			{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP},
		},
	}

	eps := BuildEndpointSlice(nn, obs)

	if len(eps.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(eps.Ports))
	}
	if *eps.Ports[0].Name != "http" || *eps.Ports[0].Port != 80 {
		t.Errorf("port 0 mismatch: %+v", eps.Ports[0])
	}
	if *eps.Ports[1].Name != "https" || *eps.Ports[1].Port != 443 {
		t.Errorf("port 1 mismatch: %+v", eps.Ports[1])
	}
}

func TestBuildEndpointSlice_SSAJSONFormat(t *testing.T) {
	nn := types.NamespacedName{Namespace: "prod", Name: "my-svc"}
	obs := discover.Observation{
		ClusterID: "cluster-1",
		Addresses: []string{"10.0.0.1"},
		Ports: []corev1.ServicePort{
			{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
		},
	}

	eps := BuildEndpointSlice(nn, obs)
	data, err := json.Marshal(eps)
	if err != nil {
		t.Fatalf("failed to marshal endpointslice: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	assertJSONString(t, m, "apiVersion", "discovery.k8s.io/v1")
	assertJSONString(t, m, "kind", "EndpointSlice")

	var meta map[string]json.RawMessage
	if err := json.Unmarshal(m["metadata"], &meta); err != nil {
		t.Fatal("missing or invalid metadata")
	}

	assertJSONString(t, meta, "name", "my-svc")
	assertJSONString(t, meta, "namespace", "prod")

	var labels map[string]json.RawMessage
	if err := json.Unmarshal(meta["labels"], &labels); err != nil {
		t.Fatal("missing or invalid labels")
	}
	assertJSONString(t, labels, "kubernetes.io/service-name", "my-svc")
	assertJSONString(t, labels, "endpointslice.kubernetes.io/managed-by", "meshless")
}

// --- BuildEndpoints tests ---

func TestBuildEndpoints_NameNamespaceLabel(t *testing.T) {
	nn := types.NamespacedName{Namespace: "prod", Name: "my-svc"}
	obs := discover.Observation{
		ClusterID: "cluster-1",
		Addresses: []string{"10.0.0.1"},
		Ports: []corev1.ServicePort{
			{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
		},
	}

	eps := BuildEndpoints(nn, obs)

	if eps.Name != "my-svc" {
		t.Errorf("expected name %q, got %q", "my-svc", eps.Name)
	}
	if eps.Namespace != "prod" {
		t.Errorf("expected namespace %q, got %q", "prod", eps.Namespace)
	}
	if eps.APIVersion != "v1" {
		t.Errorf("expected apiVersion %q, got %q", "v1", eps.APIVersion)
	}
	if eps.Kind != "Endpoints" {
		t.Errorf("expected kind %q, got %q", "Endpoints", eps.Kind)
	}
	v, ok := eps.Labels[managedLabelKey]
	if !ok || v != managedLabelVal {
		t.Errorf("expected managed label %q, got %q", managedLabelVal, v)
	}
}

func TestBuildEndpoints_AddressesInSubset(t *testing.T) {
	nn := types.NamespacedName{Namespace: "default", Name: "test"}
	obs := discover.Observation{
		Addresses: []string{"10.0.0.1"},
		Ports: []corev1.ServicePort{
			{Name: "grpc", Port: 9090, Protocol: corev1.ProtocolTCP},
		},
	}

	eps := BuildEndpoints(nn, obs)

	if len(eps.Subsets) != 1 {
		t.Fatalf("expected 1 subset, got %d", len(eps.Subsets))
	}
	if len(eps.Subsets[0].Addresses) != 1 {
		t.Fatalf("expected 1 address, got %d", len(eps.Subsets[0].Addresses))
	}
	if eps.Subsets[0].Addresses[0].IP != "10.0.0.1" {
		t.Errorf("expected IP %q, got %q", "10.0.0.1", eps.Subsets[0].Addresses[0].IP)
	}
}

func TestBuildEndpoints_PortsConverted(t *testing.T) {
	nn := types.NamespacedName{Namespace: "ns", Name: "svc"}
	obs := discover.Observation{
		Addresses: []string{"10.0.0.1"},
		Ports: []corev1.ServicePort{
			{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP},
		},
	}

	eps := BuildEndpoints(nn, obs)

	ports := eps.Subsets[0].Ports
	if len(ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(ports))
	}
	if ports[0].Name != "http" || ports[0].Port != 80 {
		t.Errorf("port 0 mismatch: %+v", ports[0])
	}
	if ports[1].Name != "https" || ports[1].Port != 443 {
		t.Errorf("port 1 mismatch: %+v", ports[1])
	}
}

func TestBuildEndpoints_MultipleAddresses(t *testing.T) {
	nn := types.NamespacedName{Namespace: "default", Name: "multi"}
	obs := discover.Observation{
		Addresses: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
		Ports: []corev1.ServicePort{
			{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
		},
	}

	eps := BuildEndpoints(nn, obs)

	addrs := eps.Subsets[0].Addresses
	if len(addrs) != 3 {
		t.Fatalf("expected 3 addresses, got %d", len(addrs))
	}
	expected := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	for i, want := range expected {
		if addrs[i].IP != want {
			t.Errorf("address %d: expected %q, got %q", i, want, addrs[i].IP)
		}
	}
}

func TestBuildEndpoints_EmptyAddresses(t *testing.T) {
	nn := types.NamespacedName{Namespace: "default", Name: "empty"}
	obs := discover.Observation{
		Addresses: []string{},
		Ports: []corev1.ServicePort{
			{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
		},
	}

	eps := BuildEndpoints(nn, obs)

	if len(eps.Subsets) != 1 {
		t.Fatalf("expected 1 subset, got %d", len(eps.Subsets))
	}
	if len(eps.Subsets[0].Addresses) != 0 {
		t.Errorf("expected 0 addresses, got %d", len(eps.Subsets[0].Addresses))
	}
}

func TestBuildEndpoints_SSAJSONFormat(t *testing.T) {
	nn := types.NamespacedName{Namespace: "prod", Name: "my-svc"}
	obs := discover.Observation{
		ClusterID: "cluster-1",
		Addresses: []string{"10.0.0.1"},
		Ports: []corev1.ServicePort{
			{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
		},
	}

	eps := BuildEndpoints(nn, obs)
	data, err := json.Marshal(eps)
	if err != nil {
		t.Fatalf("failed to marshal endpoints: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	assertJSONString(t, m, "apiVersion", "v1")
	assertJSONString(t, m, "kind", "Endpoints")

	var meta map[string]json.RawMessage
	if err := json.Unmarshal(m["metadata"], &meta); err != nil {
		t.Fatal("missing or invalid metadata")
	}

	assertJSONString(t, meta, "name", "my-svc")
	assertJSONString(t, meta, "namespace", "prod")

	var labels map[string]json.RawMessage
	if err := json.Unmarshal(meta["labels"], &labels); err != nil {
		t.Fatal("missing or invalid labels")
	}
	assertJSONString(t, labels, managedLabelKey, managedLabelVal)

	var subsets []map[string]json.RawMessage
	if err := json.Unmarshal(m["subsets"], &subsets); err != nil {
		t.Fatal("missing or invalid subsets")
	}
	if len(subsets) != 1 {
		t.Fatalf("expected 1 subset, got %d", len(subsets))
	}

	var addrs []map[string]json.RawMessage
	if err := json.Unmarshal(subsets[0]["addresses"], &addrs); err != nil {
		t.Fatal("missing or invalid addresses")
	}
	if len(addrs) != 1 {
		t.Fatalf("expected 1 address, got %d", len(addrs))
	}
	assertJSONString(t, addrs[0], "ip", "10.0.0.1")

	var ports []map[string]json.RawMessage
	if err := json.Unmarshal(subsets[0]["ports"], &ports); err != nil {
		t.Fatal("missing or invalid ports")
	}
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(ports))
	}
	assertJSONString(t, ports[0], "name", "http")
	assertJSONNumber(t, ports[0], "port", 80)
}

// --- JSON helpers ---

func assertJSONString(t *testing.T, m map[string]json.RawMessage, key, want string) {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		t.Fatalf("missing key %q", key)
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("key %q is not a string: %v", key, err)
	}
	if got != want {
		t.Errorf("key %q: expected %q, got %q", key, want, got)
	}
}

func assertJSONNumber(t *testing.T, m map[string]json.RawMessage, key string, want int) {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		t.Fatalf("missing key %q", key)
	}
	var got json.Number
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("key %q is not a number: %v", key, err)
	}
	v, err := got.Int64()
	if err != nil {
		t.Fatalf("key %q: failed to parse number: %v", key, err)
	}
	if int(v) != want {
		t.Errorf("key %q: expected %d, got %d", key, want, v)
	}
}
