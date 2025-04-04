/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"errors"
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/checkly/checkly-go-sdk"

	checklyv1alpha1 "github.com/checkly/checkly-operator/api/checkly/v1alpha1"
	checklycontrollers "github.com/checkly/checkly-operator/internal/controller/checkly"
	networkingcontrollers "github.com/checkly/checkly-operator/internal/controller/networking"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(checklyv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var controllerDomain string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&controllerDomain, "controller-domain", "k8s.checklyhq.com", "Domain to use for annotations and finalizers.")
	opts := zap.Options{
		// Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog.Info("Controller domain setup", "value", controllerDomain)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "4e7eab13.checklyhq.com",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	baseUrl := "https://api.checklyhq.com"
	apiKey := os.Getenv("CHECKLY_API_KEY")
	if apiKey == "" {
		setupLog.Error(errors.New("checklyhq.com API key environment variable is undefined"), "checklyhq.com credentials missing")
		os.Exit(1)
	}

	accountId := os.Getenv("CHECKLY_ACCOUNT_ID")
	if accountId == "" {
		setupLog.Error(errors.New("checklyhq.com Account ID environment variable is undefined"), "checklyhq.com credentials missing")
		os.Exit(1)
	}

	client := checkly.NewClient(
		baseUrl,
		apiKey,
		nil, //custom http client, defaults to http.DefaultClient
		nil, //io.Writer to output debug messages
	)

	client.SetAccountId(accountId)

	if err = (&networkingcontrollers.IngressReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		ControllerDomain: controllerDomain,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Ingress")
		os.Exit(1)
	}
	if err = (&checklycontrollers.ApiCheckReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		ApiClient:        client,
		ControllerDomain: controllerDomain,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ApiCheck")
		os.Exit(1)
	}
	if err = (&checklycontrollers.GroupReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		ApiClient:        client,
		ControllerDomain: controllerDomain,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Group")
		os.Exit(1)
	}
	if err = (&checklycontrollers.AlertChannelReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		ApiClient:        client,
		ControllerDomain: controllerDomain,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AlertChannel")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	setupLog.V(1).Info("starting health endpoint")
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	setupLog.V(1).Info("starting ready endpoint")
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.V(1).Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
