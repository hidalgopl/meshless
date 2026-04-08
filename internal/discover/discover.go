package discover

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hidalgopl/meshless/internal/cluster"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type Observation struct {
	ClusterID string
	Addresses []string
	Ports     []corev1.ServicePort
}

func ClassifyService(svc *corev1.Service) bool {
	return len(svc.Spec.Selector) > 0
}

func ExtractAddresses(svc *corev1.Service) []string {
	ingress := svc.Status.LoadBalancer.Ingress
	if len(ingress) == 0 {
		return nil
	}
	ips := make([]string, 0, len(ingress))
	for _, ing := range ingress {
		if ing.IP != "" {
			ips = append(ips, ing.IP)
		}
	}
	return ips
}

func ExtractPorts(svc *corev1.Service) []corev1.ServicePort {
	if len(svc.Spec.Ports) == 0 {
		return nil
	}
	ports := make([]corev1.ServicePort, len(svc.Spec.Ports))
	copy(ports, svc.Spec.Ports)
	return ports
}

func Observe(ctx context.Context, clusters []cluster.Cluster, namespaces []string, annotation string) (map[types.NamespacedName][]Observation, error) {
	result := make(map[types.NamespacedName][]Observation)
	failCount := 0

	for _, c := range clusters {
		if c.Client == nil {
			slog.Error("cluster has nil client", "cluster_id", c.ID)
			failCount++
			continue
		}

		var localFailed bool
		for _, ns := range namespaces {
			svcList, err := c.Client.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				slog.Error("failed to list services", "cluster_id", c.ID, "namespace", ns, "error", err)
				localFailed = true
				continue
			}

			for i := range svcList.Items {
				svc := &svcList.Items[i]
				if _, ok := svc.Annotations[annotation]; !ok {
					continue
				}

				nn := types.NamespacedName{Namespace: ns, Name: svc.Name}
				obs := Observation{
					ClusterID: c.ID,
				}

				if ClassifyService(svc) {
					obs.Addresses = ExtractAddresses(svc)
					if len(obs.Addresses) == 0 {
						slog.Debug("provider service has no LoadBalancer IP yet",
							"cluster_id", c.ID,
							"service", svc.Name,
							"namespace", ns,
						)
					}
					obs.Ports = ExtractPorts(svc)
				}

				result[nn] = append(result[nn], obs)
			}
		}

		if localFailed {
			failCount++
		}
	}

	if failCount == len(clusters) && len(clusters) > 0 {
		return result, fmt.Errorf("all %d clusters failed", failCount)
	}
	return result, nil
}
