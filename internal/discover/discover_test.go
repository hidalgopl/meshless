package discover

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestClassifyService(t *testing.T) {
	tests := []struct {
		name string
		svc  *corev1.Service
		want bool
	}{
		{
			name: "service with selector is provider",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": "myapp"},
				},
			},
			want: true,
		},
		{
			name: "service without selector is consumer",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{},
				},
			},
			want: false,
		},
		{
			name: "service with nil selector is consumer",
			svc:  &corev1.Service{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyService(tt.svc)
			if got != tt.want {
				t.Errorf("ClassifyService() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractAddresses(t *testing.T) {
	tests := []struct {
		name string
		svc  *corev1.Service
		want []string
	}{
		{
			name: "single LoadBalancer IP",
			svc: &corev1.Service{
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{IP: "10.0.0.1"},
						},
					},
				},
			},
			want: []string{"10.0.0.1"},
		},
		{
			name: "multiple LoadBalancer IPs",
			svc: &corev1.Service{
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{IP: "10.0.0.1"},
							{IP: "10.0.0.2"},
							{IP: "10.0.0.3"},
						},
					},
				},
			},
			want: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
		},
		{
			name: "no LoadBalancer IP assigned",
			svc: &corev1.Service{
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: nil,
					},
				},
			},
			want: nil,
		},
		{
			name: "LoadBalancer ingress with hostname only (no IP)",
			svc: &corev1.Service{
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{Hostname: "example.com"},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "mixed IP and hostname entries",
			svc: &corev1.Service{
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{IP: "10.0.0.1"},
							{Hostname: "example.com"},
							{IP: "10.0.0.2"},
						},
					},
				},
			},
			want: []string{"10.0.0.1", "10.0.0.2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractAddresses(tt.svc)
			if len(got) != len(tt.want) {
				t.Fatalf("ExtractAddresses() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ExtractAddresses()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestExtractPorts(t *testing.T) {
	tests := []struct {
		name string
		svc  *corev1.Service
		want []corev1.ServicePort
	}{
		{
			name: "multiple ports with different protocols",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstrFrom(8080)},
						{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP, TargetPort: intstrFrom(8443)},
						{Name: "grpc", Port: 9090, Protocol: corev1.ProtocolUDP, TargetPort: intstrFrom(9090)},
					},
				},
			},
			want: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstrFrom(8080)},
				{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP, TargetPort: intstrFrom(8443)},
				{Name: "grpc", Port: 9090, Protocol: corev1.ProtocolUDP, TargetPort: intstrFrom(9090)},
			},
		},
		{
			name: "single port",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
					},
				},
			},
			want: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			},
		},
		{
			name: "no ports",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Ports: nil,
				},
			},
			want: nil,
		},
		{
			name: "empty ports slice",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{},
				},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractPorts(tt.svc)
			if len(got) != len(tt.want) {
				t.Fatalf("ExtractPorts() length = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].Name != tt.want[i].Name || got[i].Port != tt.want[i].Port || got[i].Protocol != tt.want[i].Protocol {
					t.Errorf("ExtractPorts()[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func intstrFrom(v int32) intstr.IntOrString {
	return intstr.IntOrString{IntVal: v, Type: intstr.Int}
}
