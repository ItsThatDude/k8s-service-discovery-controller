package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"k8s-sd-agent/internal/api"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type validPortFlags []string

func (p *validPortFlags) String() string {
	return strings.Join(*p, ", ")
}
func (p *validPortFlags) Set(value string) error {
	*p = append(*p, value)
	return nil
}

type watchNamespaceFlags []string

func (p *watchNamespaceFlags) String() string {
	return strings.Join(*p, ", ")
}
func (p *watchNamespaceFlags) Set(value string) error {
	*p = append(*p, value)
	return nil
}

func main() {
	var watchNamespaces watchNamespaceFlags
	var labelSelectorStr string
	var validPorts validPortFlags
	flag.Var(&watchNamespaces, "namespace", "The namespace to watch (can repeat)")
	flag.StringVar(&labelSelectorStr, "selector", "", "Label selector string (e.g. app=frontend)")
	flag.Var(&validPorts, "port", "Port number or port name to filter by (can repeat)")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	setupLog := ctrl.Log.WithName("setup")
	setupLog.Info("Logger initialized successfully!")

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)

	var labelSelector labels.Selector
	if labelSelectorStr != "" {
		var err error
		labelSelector, err = labels.Parse(labelSelectorStr)
		if err != nil {
			fmt.Printf("Error parsing label-selector: %v\n", err)
			os.Exit(1)
		}
	} else {
		labelSelector = labels.Everything()
	}

	namespacesMap := make(map[string]cache.Config)
	if len(watchNamespaces) > 0 {
		for _, ns := range watchNamespaces {
			namespacesMap[ns] = cache.Config{}
		}
		setupLog.Info(fmt.Sprintf("Watching %d namespaces", len(watchNamespaces)), "namespaces", watchNamespaces)
	} else {
		setupLog.Info("Globally watching all namespaces")
	}

	cluster, err := cluster.New(ctrl.GetConfigOrDie(), func(o *cluster.Options) {
		o.Scheme = scheme
		o.Cache = cache.Options{
			DefaultNamespaces: namespacesMap,
			ByObject: map[client.Object]cache.ByObject{
				&corev1.Service{}: {Label: labelSelector},
			},
		}
	})
	if err != nil {
		os.Exit(1)
	}

	settings := api.ApiServerSettings{
		ValidPorts: validPorts,
	}
	var cacheSynced bool
	apiServer := api.NewApiServer(cluster.GetClient(), settings, &cacheSynced)

	ctx := ctrl.SetupSignalHandler()

	go func() {
		if err := cluster.Start(ctx); err != nil {
			os.Exit(1)
		}
	}()

	go func() {
		if cluster.GetCache().WaitForCacheSync(ctx) {
			cacheSynced = true
		}
	}()

	setupLog.Info("Starting server on :8080")
	_ = http.ListenAndServe(":8080", apiServer.Handler())
}
