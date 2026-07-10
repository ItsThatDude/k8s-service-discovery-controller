package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DiscoveredService struct {
	Name      string   `json:"name"`
	Endpoints []string `json:"endpoints"`
}

type ApiServerSettings struct {
	ValidPorts  []string `json:"validPorts"`
	CheckHealth bool     `json:"checkHealth"`
}

type ApiServer struct {
	ctx        context.Context
	client     client.Client
	isReadyPtr *bool
	log        logr.Logger
	settings   ApiServerSettings
}

func NewApiServer(ctx context.Context, c client.Client, settings ApiServerSettings, isReady *bool) *ApiServer {
	serverLog := ctrl.Log.WithName("api")

	return &ApiServer{
		ctx:        ctx,
		client:     c,
		isReadyPtr: isReady,
		log:        serverLog,
		settings:   settings,
	}
}

func (s *ApiServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/services", s.handleListServices)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	return mux
}

func (s *ApiServer) handleListServices(w http.ResponseWriter, r *http.Request) {
	var serviceList corev1.ServiceList

	err := s.client.List(s.ctx, &serviceList)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := make([]DiscoveredService, 0, len(serviceList.Items))
	for _, svc := range serviceList.Items {
		if len(s.settings.ValidPorts) > 0 && !s.matchPorts(svc.Spec.Ports) {
			continue
		}

		if s.settings.CheckHealth {
			isHealthy, err := s.isHealthy(&svc)

			if err != nil {
				s.log.Error(err, "Failed to check service health", "service", svc.Name)
			}

			if !isHealthy {
				continue
			}
		}

		var ips []string
		for _, ingress := range svc.Status.LoadBalancer.Ingress {
			if ingress.IP != "" {
				ips = append(ips, ingress.IP)
			}
		}

		if len(ips) > 0 {
			response = append(response, DiscoveredService{
				Name:      svc.Name,
				Endpoints: ips,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *ApiServer) matchPorts(svcPorts []corev1.ServicePort) bool {
	for _, p := range s.settings.ValidPorts {
		for _, sp := range svcPorts {
			if p == sp.Name {
				return true
			}

			if num, err := strconv.Atoi(p); err == nil && int32(num) == sp.Port {
				return true
			}
		}
	}
	return false
}

func (s *ApiServer) isHealthy(svc *corev1.Service) (bool, error) {
	sliceList := &discoveryv1.EndpointSliceList{}

	listOpts := []client.ListOption{
		client.InNamespace(svc.Namespace),
		client.MatchingLabels{"kubernetes.io/service-name": svc.Name},
	}

	if err := s.client.List(s.ctx, sliceList, listOpts...); err != nil {
		return false, fmt.Errorf("failed to list endpoint slices: %w", err)
	}

	for _, slice := range sliceList.Items {
		for _, endpoint := range slice.Endpoints {
			if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
				if endpoint.Conditions.Terminating != nil && *endpoint.Conditions.Terminating {
					continue
				}
				return true, nil
			}
		}
	}

	return false, nil
}

func (s *ApiServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *ApiServer) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if s.isReadyPtr == nil || !*s.isReadyPtr {
		s.log.Info("Cache not synced yet")
		http.Error(w, "cache not synced", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}
