/*


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
	"context"
	"flag"
	"os"

	opv1 "github.com/operator-framework/api/pkg/operators/v1"
	"go.uber.org/zap/zapcore"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	configv1 "github.com/openshift/api/config/v1"
	openshiftv1 "github.com/openshift/api/network/v1"
	operators "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ovnv1 "github.com/ovn-org/ovn-kubernetes/go-controller/pkg/crd/egressfirewall/v1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promv1a1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	misfv1a1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	"github.com/red-hat-storage/managed-fusion-agent/controllers"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;watch;list

func init() {

	addAllSchemes(scheme)

}

func addAllSchemes(scheme *runtime.Scheme) {

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(promv1.AddToScheme(scheme))

	utilruntime.Must(promv1a1.AddToScheme(scheme))

	utilruntime.Must(v1.AddToScheme(scheme))

	utilruntime.Must(corev1.AddToScheme(scheme))

	utilruntime.Must(operators.AddToScheme(scheme))

	utilruntime.Must(openshiftv1.AddToScheme(scheme))

	utilruntime.Must(configv1.AddToScheme(scheme))

	utilruntime.Must(misfv1a1.AddToScheme(scheme))

	utilruntime.Must(opv1.AddToScheme(scheme))

	utilruntime.Must(ovnv1.AddToScheme(scheme))

	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme

	pluginAddToScheme(scheme)
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true), zap.StacktraceLevel(zapcore.ErrorLevel)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "e0c63ac0.openshift.io",
	})
	if err != nil {
		setupLog.Error(err, "Unable to start manager")
		os.Exit(1)
	}
	namespace, found := os.LookupEnv("NAMESPACE")
	if !found {
		setupLog.Error(err, "Failed to get 'NAMESPACE' environment variable")
		os.Exit(1)
	}

	availableCRDs := mapCRDAvailability()
	unrestictedClient := getUnrestrictedClient()

	if err = (&controllers.ManagedFusionReconciler{
		Client:                       mgr.GetClient(),
		UnrestrictedClient:           unrestictedClient,
		Log:                          ctrl.Log.WithName("controllers").WithName("ManagedFusion"),
		Scheme:                       mgr.GetScheme(),
		Namespace:                    namespace,
		AvailableCRDs:                availableCRDs,
		CustomerNotificationHTMLPath: "templates/customernotification.html",
	}).SetupWithManager(mgr, nil); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "ManagedFusion")
		os.Exit(1)
	}

	if err = (&controllers.ManagedFusionOfferingReconciler{
		Client:             mgr.GetClient(),
		Log:                ctrl.Log.WithName("controllers").WithName("ManagedFusionOffering"),
		Scheme:             mgr.GetScheme(),
		AvailableCRDs:      availableCRDs,
		UnrestrictedClient: unrestictedClient,
	}).SetupWithManager(mgr, nil); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ManagedFusionOffering")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Problem running manager")
		os.Exit(1)
	}
}

// getUnrestrictedClient creates a client required for listing PVCs from all namespaces.
func getUnrestrictedClient() client.Client {
	var options client.Options

	options.Scheme = runtime.NewScheme()
	addAllSchemes(options.Scheme)
	k8sClient, err := client.New(config.GetConfigOrDie(), options)
	if err != nil {
		setupLog.Error(err, "error creating client")
		os.Exit(1)
	}
	return k8sClient
}

// This function is a placeholder for offering plugin integration
func pluginAddToScheme(scheme *runtime.Scheme) {
	controllers.DFAddToScheme(scheme)
	controllers.DFCAddToScheme(scheme)
}

func mapCRDAvailability() map[string]bool {

	var options client.Options
	options.Scheme = runtime.NewScheme()
	utilruntime.Must(apiextensionsv1.AddToScheme(options.Scheme))

	k8sClient, err := client.New(config.GetConfigOrDie(), options)
	if err != nil {
		setupLog.Error(err, "error creating client")
		os.Exit(1)
	}

	CRDExist := map[string]bool{}

	crds := apiextensionsv1.CustomResourceDefinitionList{}
	err = k8sClient.List(context.Background(), &crds)

	if err != nil && !errors.IsNotFound(err) {
		setupLog.Error(err, "Error Getting CRDs installed on the cluster")
		os.Exit(1)
	}

	for i := range crds.Items {
		CRDExist[crds.Items[i].Name] = true
	}

	return CRDExist
}
