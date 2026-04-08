# Meshless

A lightweight, annotation-driven cross-cluster service endpoint synchronizer for flat networks.

## The Problem

When running multiple Kubernetes clusters in a shared network (e.g., vCluster-in-Docker for local dev), LoadBalancer services are routable across clusters by IP. But Kubernetes DNS only resolves within its own cluster — `foo.bar.svc.cluster.local` in cluster A won't resolve in cluster B.

This means you can't use service FQDNs to connect applications across clusters, even though the network path exists.

## How Meshless Helps

Meshless connects to multiple Kubernetes API servers, watches Services for a specific annotation, and copies endpoint addresses from the cluster that has them (the provider) into EndpointSlices on clusters that don't (the consumers).

The result: a service FQDN resolves identically in every cluster, and traffic flows through the shared network to the actual backend.

```
Cluster A                              Cluster B                              Cluster C
┌─────────────────────┐                ┌─────────────────────┐                ┌─────────────────────┐
│ svc/my-svc (LB IP)  │                │ svc/my-svc (no LB)  │                │ svc/my-svc (no LB)  │
│  → real pods        │                │  → meshless copies  │                │  → meshless copies  │
│     endpoints here  │                │    EndpointSlice    │                │    EndpointSlice    │
└─────────────────────┘                └─────────────────────┘                └─────────────────────┘
         │                                       │                                       │
         └────────────────── meshless ───────────┘───────────────────────────────────────┘
```

## Requirements

Meshless runs as a single binary outside of any cluster. It needs:

- Network reachability to all Kubernetes API servers
- Network reachability from pods in consumer clusters to provider LoadBalancer IPs

For vCluster-in-Docker in the same Docker network, both conditions are met out of the box.

## Usage

```bash
# Build
go build -o meshless ./cmd/meshless

# Sync mode — run as a long-running process
meshless --kubeconfig-dir ./kubeconfigs/ --namespace default

# Or pass individual kubeconfig paths
meshless \
  --kubeconfig ./cluster-a.yaml \
  --kubeconfig ./cluster-b.yaml \
  --kubeconfig ./cluster-c.yaml \
  --namespace default

# Cleanup mode — remove all managed resources
meshless cleanup --kubeconfig-dir ./kubeconfigs/

# Annotate a service to export it across clusters
kubectl annotate svc my-service meshless.global/export=true -n default
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--kubeconfig-dir` | — | Directory of kubeconfig files (one per cluster) |
| `--kubeconfig` | — | Kubeconfig path (repeatable) |
| `--namespace` | `default` | Namespace to watch (repeatable) |
| `--annotation` | `meshless.global/export` | Annotation key to filter services |
| `--sync-interval` | `1s` | Reconcile interval |
| `--sync-endpoints` | `false` | Also sync corev1.Endpoints (default: EndpointSlices only) |

## How It Works

1. Connects to all clusters using kubeconfigs provided at startup.
2. Lists Services in configured namespaces, filters by annotation.
3. For each annotated service, finds the cluster with real endpoints (the provider).
4. In all other clusters (consumers), creates or patches EndpointSlices with the provider's addresses via Server-Side Apply.
5. Repeats on a configurable interval.

EndpointSlices are the default sync target. Pass `--sync-endpoints` to also sync corev1.Endpoints (useful for clients that don't read EndpointSlices).

## RBAC

Minimal `Role` per watched namespace (remove the commented rule unless using `--sync-endpoints`):

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: meshless
rules:
  - apiGroups: [""]
    resources: ["services"]
    verbs: ["get", "list"]
  - apiGroups: ["discovery.k8s.io"]
    resources: ["endpointslices"]
    verbs: ["create", "delete", "get", "list", "patch"]
  # - apiGroups: [""]
  #   resources: ["endpoints"]
  #   verbs: ["create", "delete", "get", "list", "patch"]  # only with --sync-endpoints
```

## Scope

Meshless solves exactly one problem: making service FQDNs resolve across clusters in environments where network connectivity already exists. It is not a service mesh.

| | Cilium Cluster Mesh | Meshless |
|---|---|---|
| Cross-cluster DNS | Yes (built-in) | Yes (via endpoint sync) |
| Encryption | WireGuard/IPsec | No (relies on network) |
| Network policy | Yes | No |
| Setup | CNI-level, complex | Single binary, annotation-driven |
| Designed for | Production | Flat networks, local dev |

## License

Apache 2.0 — see [LICENSE](LICENSE).
