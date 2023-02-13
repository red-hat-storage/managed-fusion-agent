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
	"flag"
	"os"

	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	configv1 "github.com/openshift/api/config/v1"
	openshiftv1 "github.com/openshift/api/network/v1"
	operators "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ovnv1 "github.com/ovn-org/ovn-kubernetes/go-controller/pkg/crd/egressfirewall/v1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promv1a1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	odfv1a1 "github.com/red-hat-data-services/odf-operator/api/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	v1 "github.com/red-hat-storage/ocs-osd-deployer/api/v1alpha1"
	"github.com/red-hat-storage/ocs-osd-deployer/controllers"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {

	addAllSchemes(scheme)

}

func addAllSchemes(scheme *runtime.Scheme) {

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(ocsv1.AddToScheme(scheme))

	utilruntime.Must(promv1.AddToScheme(scheme))

	utilruntime.Must(promv1a1.AddToScheme(scheme))

	utilruntime.Must(v1.AddToScheme(scheme))

	utilruntime.Must(operators.AddToScheme(scheme))

	utilruntime.Must(openshiftv1.AddToScheme(scheme))

	utilruntime.Must(odfv1a1.AddToScheme(scheme))

	utilruntime.Must(ocsv1alpha1.AddToScheme(scheme))

	utilruntime.Must(configv1.AddToScheme(scheme))

	utilruntime.Must(ovnv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
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
		NewCache: cache.BuilderWithOptions(cache.Options{
			SelectorsByObject: cache.SelectorsByObject{
				&corev1.Namespace{}: {
					Label: labels.SelectorFromSet(
						labels.Set{
							"managed-by-fusion": "true",
						},
					),
				},
			},
		}),
	})
	if err != nil {
		setupLog.Error(err, "Unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.ManagedOCSReconciler{
		Client:                       mgr.GetClient(),
		UnrestrictedClient:           getUnrestrictedClient(),
		Log:                          ctrl.Log.WithName("controllers").WithName("ManagedOCS"),
		Scheme:                       mgr.GetScheme(),
		CustomerNotificationHTMLPath: "templates/customernotification.html",
	}).SetupWithManager(mgr, nil); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "ManagedOCS")
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
