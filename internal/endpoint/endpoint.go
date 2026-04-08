package endpoint

import (
	"context"
	"log/slog"

	"github.com/hidalgopl/meshless/internal/cluster"
	"github.com/hidalgopl/meshless/internal/discover"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/kubernetes"
)

const (
	managedLabelKey = "meshless.global/managed"
	managedLabelVal = "true"
	fieldManager    = "meshless"
)

type SyncOptions struct {
	SyncEndpoints bool
}

func Sync(ctx context.Context, clusters []cluster.Cluster, observations map[types.NamespacedName][]discover.Observation, opts SyncOptions) {
	clusterMap := make(map[string]*cluster.Cluster, len(clusters))
	for i := range clusters {
		c := clusters[i]
		clusterMap[c.ID] = &c
	}

	for nsName, obs := range observations {
		provider := findProvider(obs)
		if provider == nil {
			slog.Warn("no provider found for service",
				"namespace", nsName.Namespace,
				"name", nsName.Name,
			)
			continue
		}

		for _, consumer := range obs {
			if len(consumer.Addresses) > 0 {
				continue
			}

			c, ok := clusterMap[consumer.ClusterID]
			if !ok {
				slog.Warn("cluster not found for consumer",
					"cluster_id", consumer.ClusterID,
					"namespace", nsName.Namespace,
					"name", nsName.Name,
				)
				continue
			}

			syncEndpointSlice(ctx, c.Client, nsName, *provider)

			if opts.SyncEndpoints {
				syncEndpoints(ctx, c.Client, nsName, *provider)
			}
		}
	}
}

func findProvider(obs []discover.Observation) *discover.Observation {
	for i := range obs {
		if len(obs[i].Addresses) > 0 {
			return &obs[i]
		}
	}
	return nil
}

func syncEndpointSlice(ctx context.Context, client *kubernetes.Clientset, nsName types.NamespacedName, provider discover.Observation) {
	eps := BuildEndpointSlice(nsName, provider)

	data, err := json.Marshal(eps)
	if err != nil {
		slog.Error("failed to marshal endpointslice",
			"namespace", nsName.Namespace,
			"name", eps.Name,
			"error", err,
		)
		return
	}

	_, err = client.DiscoveryV1().EndpointSlices(nsName.Namespace).Patch(ctx, eps.Name, types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: fieldManager,
	})
	if err != nil {
		slog.Error("failed to patch endpointslice",
			"namespace", nsName.Namespace,
			"name", eps.Name,
			"error", err,
		)
		return
	}

	slog.Info("synced endpointslice",
		"namespace", nsName.Namespace,
		"name", eps.Name,
		"addresses", provider.Addresses,
	)
}

func syncEndpoints(ctx context.Context, client *kubernetes.Clientset, nsName types.NamespacedName, provider discover.Observation) {
	eps := BuildEndpoints(nsName, provider)

	data, err := json.Marshal(eps)
	if err != nil {
		slog.Error("failed to marshal endpoints",
			"namespace", nsName.Namespace,
			"name", nsName.Name,
			"error", err,
		)
		return
	}

	_, err = client.CoreV1().Endpoints(nsName.Namespace).Patch(ctx, nsName.Name, types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: fieldManager,
	})
	if err != nil {
		slog.Error("failed to patch endpoints",
			"namespace", nsName.Namespace,
			"name", nsName.Name,
			"error", err,
		)
		return
	}

	slog.Info("synced endpoints",
		"namespace", nsName.Namespace,
		"name", nsName.Name,
		"addresses", provider.Addresses,
	)
}

func Cleanup(ctx context.Context, clusters []cluster.Cluster, namespaces []string, opts SyncOptions) {
	for _, c := range clusters {
		for _, ns := range namespaces {
			cleanupEndpointSlices(ctx, c.Client, ns, c.ID)
			if opts.SyncEndpoints {
				cleanupEndpoints(ctx, c.Client, ns, c.ID)
			}
		}
	}
}

func cleanupEndpointSlices(ctx context.Context, client *kubernetes.Clientset, ns, clusterID string) {
	list, err := client.DiscoveryV1().EndpointSlices(ns).List(ctx, metav1.ListOptions{
		LabelSelector: managedLabelKey + "=" + managedLabelVal,
	})
	if err != nil {
		slog.Error("failed to list endpointslices",
			"namespace", ns,
			"cluster_id", clusterID,
			"error", err,
		)
		return
	}

	for _, eps := range list.Items {
		if err := client.DiscoveryV1().EndpointSlices(ns).Delete(ctx, eps.Name, metav1.DeleteOptions{}); err != nil {
			slog.Error("failed to delete endpointslice",
				"name", eps.Name,
				"namespace", ns,
				"cluster_id", clusterID,
				"error", err,
			)
		} else {
			slog.Info("deleted endpointslice",
				"name", eps.Name,
				"namespace", ns,
				"cluster_id", clusterID,
			)
		}
	}
}

func cleanupEndpoints(ctx context.Context, client *kubernetes.Clientset, ns, clusterID string) {
	list, err := client.CoreV1().Endpoints(ns).List(ctx, metav1.ListOptions{
		LabelSelector: managedLabelKey + "=" + managedLabelVal,
	})
	if err != nil {
		slog.Error("failed to list endpoints",
			"namespace", ns,
			"cluster_id", clusterID,
			"error", err,
		)
		return
	}

	for _, ep := range list.Items {
		if err := client.CoreV1().Endpoints(ns).Delete(ctx, ep.Name, metav1.DeleteOptions{}); err != nil {
			slog.Error("failed to delete endpoints",
				"name", ep.Name,
				"namespace", ns,
				"cluster_id", clusterID,
				"error", err,
			)
		} else {
			slog.Info("deleted endpoints",
				"name", ep.Name,
				"namespace", ns,
				"cluster_id", clusterID,
			)
		}
	}
}

func BuildEndpointSlice(nsName types.NamespacedName, provider discover.Observation) *discoveryv1.EndpointSlice {
	endpoints := make([]discoveryv1.Endpoint, 0, len(provider.Addresses))
	for _, addr := range provider.Addresses {
		endpoints = append(endpoints, discoveryv1.Endpoint{
			Addresses: []string{addr},
		})
	}

	ports := make([]discoveryv1.EndpointPort, 0, len(provider.Ports))
	for _, p := range provider.Ports {
		ports = append(ports, discoveryv1.EndpointPort{
			Name:     &p.Name,
			Port:     &p.Port,
			Protocol: &p.Protocol,
		})
	}

	return &discoveryv1.EndpointSlice{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "discovery.k8s.io/v1",
			Kind:       "EndpointSlice",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      nsName.Name,
			Namespace: nsName.Namespace,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: nsName.Name,
				discoveryv1.LabelManagedBy:   "meshless",
				managedLabelKey:              managedLabelVal,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints:   endpoints,
		Ports:       ports,
	}
}

//nolint:staticcheck // corev1.Endpoints is intentionally supported via --sync-endpoints flag
func BuildEndpoints(nsName types.NamespacedName, provider discover.Observation) *corev1.Endpoints {
	addresses := make([]corev1.EndpointAddress, 0, len(provider.Addresses))
	for _, addr := range provider.Addresses {
		addresses = append(addresses, corev1.EndpointAddress{IP: addr})
	}

	ports := make([]corev1.EndpointPort, 0, len(provider.Ports))
	for _, p := range provider.Ports {
		ports = append(ports, corev1.EndpointPort{
			Name:     p.Name,
			Port:     p.Port,
			Protocol: p.Protocol,
		})
	}

	return &corev1.Endpoints{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Endpoints",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      nsName.Name,
			Namespace: nsName.Namespace,
			Labels:    map[string]string{managedLabelKey: managedLabelVal},
		},
		Subsets: []corev1.EndpointSubset{{
			Addresses: addresses,
			Ports:     ports,
		}},
	}
}
