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

package controllers

import (
	"context"
	"fmt"
	"io/ioutil"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promv1a1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	"github.com/red-hat-storage/managed-fusion-agent/templates"
	"github.com/red-hat-storage/managed-fusion-agent/utils"
)

const (
	managedFusionAgentFinalizer          = "managedfusionagent.ibm.com/finalizer"
	managedFusionAgentSecretName         = "managed-fusion-agent-config"
	managedFusionAgentSecretSMTPKey      = "smtp_config"
	managedFusionAgentSecretPagerDutyKey = "pager_duty_config"
	prometheusName                       = "managed-fusion-prometheus"
	alertmanagerName                     = "managed-fusion-alertmanager"
	alertmanagerConfigName               = "managed-fusion-alertmanager-config"
	alertmanagerConfigSecretName         = "managed-fusion-alertmanager-config"
	agentCSVPrefix                       = "managed-fusion-agent"
	monLabelKey                          = "app"
	monLabelValue                        = "managed-fusion-agent"
	pagerDutyKey                         = "pagerDutyServiceKey"
	smtpPasswordKey                      = "smtpAuthPassword"
	alertRelabelConfigSecretName         = "managed-ocs-alert-relabel-config-secret"
	alertRelabelConfigSecretKey          = "alertrelabelconfig.yaml"
)

type ManagedFusionDeploymentReconciler struct {
	Client             client.Client
	UnrestrictedClient client.Client
	Log                logr.Logger
	Scheme             *runtime.Scheme

	ctx                           context.Context
	Namespace                     string
	prometheus                    *promv1.Prometheus
	alertmanager                  *promv1.Alertmanager
	alertmanagerConfig            *promv1a1.AlertmanagerConfig
	alertmanagerConfigSecret      *corev1.Secret
	managedFusionDeploymentSecret *corev1.Secret
	smtpConfigData                *smtpConfig
	pagerDutyConfigData           *pagerDutyConfig
	CustomerNotificationHTMLPath  string
}

// Add necessary rbac permissions for managedfusiondeployment finalizer in order to set blockOwnerDeletion.
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources={alertmanagers,prometheuses,alertmanagerconfigs},verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=prometheusrules,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=podmonitors,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=servicemonitors,verbs=get;list;watch;update;patch;create;delete
// +kubebuilder:rbac:groups="",resources={secrets,secrets/finalizers},verbs=create;get;list;watch;update
// +kubebuilder:rbac:groups=operators.coreos.com,resources=clusterserviceversions,verbs=get;list;watch;delete;update;patch
// +kubebuilder:rbac:groups="apps",resources=statefulsets,verbs=get;list;watch
// +kubebuilder:rbac:groups="coordination.k8s.io",resources=leases,verbs=create;get;list;watch;update

// SetupWithManager creates an setup a ManagedFusionDeploymentReconciler to work with the provided manager
func (r *ManagedFusionDeploymentReconciler) SetupWithManager(mgr ctrl.Manager, ctrlOptions *controller.Options) error {
	if ctrlOptions == nil {
		ctrlOptions = &controller.Options{
			MaxConcurrentReconciles: 1,
		}
	}
	managedFusionDeploymentSecretPredicates := builder.WithPredicates(
		predicate.NewPredicateFuncs(
			func(client client.Object) bool {
				name := client.GetName()
				return name == managedFusionAgentSecretName
			},
		),
	)
	monStatefulSetPredicates := builder.WithPredicates(
		predicate.NewPredicateFuncs(
			func(client client.Object) bool {
				name := client.GetName()
				return name == fmt.Sprintf("prometheus-%s", prometheusName) ||
					name == fmt.Sprintf("alertmanager-%s", alertmanagerName)
			},
		),
	)
	namespacePredicate := builder.WithPredicates(
		predicate.NewPredicateFuncs(
			func(object client.Object) bool {
				namespace := object.GetNamespace()
				return namespace == r.Namespace
			},
		),
	)
	enqueueManangedFusionDeploymentRequest := handler.EnqueueRequestsFromMapFunc(
		func(client client.Object) []reconcile.Request {
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Name:      managedFusionAgentSecretName,
					Namespace: client.GetNamespace(),
				},
			}}
		},
	)

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(*ctrlOptions).
		For(&corev1.Secret{}, managedFusionDeploymentSecretPredicates, namespacePredicate).
		// Watch owned resources
		Owns(&promv1.Prometheus{}, namespacePredicate).
		Owns(&promv1.Alertmanager{}, namespacePredicate).
		Owns(&corev1.Secret{}, namespacePredicate).
		Owns(&promv1a1.AlertmanagerConfig{}, namespacePredicate).
		// Watch non-owned resources
		Watches(
			&source.Kind{Type: &appsv1.StatefulSet{}},
			enqueueManangedFusionDeploymentRequest,
			monStatefulSetPredicates,
			namespacePredicate,
		).
		Complete(r)
}

// Reconcile changes to all owned resource based on the infromation provided by the ManangedFusionDeployment secret resource
func (r *ManagedFusionDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("req.Namespace", req.Namespace, "req.Name", req.Name)
	log.Info("Starting reconcile for ManangedFusionDeployment")

	// Initalize the reconciler properties from the request
	r.initReconciler(ctx, req)

	// Load the ManangedFusionDeployment secret resource (input)
	if err := r.get(r.managedFusionDeploymentSecret); err != nil {
		if errors.IsNotFound(err) {
			r.Log.V(-1).Info("managed-fusion-agent-config secret resource not found")
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "Failed to get managed-fusion-agent-config secret")
		return ctrl.Result{}, fmt.Errorf("failed to get managed-fusion-agent-config secret: %v", err)
	}

	// Run the reconcile phases
	result, err := r.reconcilePhases()
	if err != nil {
		r.Log.Error(err, "An error was encountered during reconcilePhases")
	}
	return result, nil
}

func (r *ManagedFusionDeploymentReconciler) initReconciler(ctx context.Context, req ctrl.Request) {
	r.ctx = ctx

	r.prometheus = &promv1.Prometheus{}
	r.prometheus.Name = prometheusName
	r.prometheus.Namespace = r.Namespace

	r.alertmanager = &promv1.Alertmanager{}
	r.alertmanager.Name = alertmanagerName
	r.alertmanager.Namespace = r.Namespace

	r.alertmanagerConfigSecret = &corev1.Secret{}
	r.alertmanagerConfigSecret.Name = alertmanagerConfigSecretName
	r.alertmanagerConfigSecret.Namespace = r.Namespace

	r.alertmanagerConfig = &promv1a1.AlertmanagerConfig{}
	r.alertmanagerConfig.Name = alertmanagerConfigName
	r.alertmanagerConfig.Namespace = r.Namespace

	r.managedFusionDeploymentSecret = &corev1.Secret{}
	r.managedFusionDeploymentSecret.Name = managedFusionAgentSecretName
	r.managedFusionDeploymentSecret.Namespace = r.Namespace

	r.smtpConfigData = &smtpConfig{}
	r.pagerDutyConfigData = &pagerDutyConfig{}
}

func (r *ManagedFusionDeploymentReconciler) reconcilePhases() (reconcile.Result, error) {
	// Uninstallation depends on the status of the components.
	// We are checking the uninstallation condition before getting the component status
	// to mitigate scenarios where changes to the component status occurs while the uninstallation logic is running.

	if !r.managedFusionDeploymentSecret.DeletionTimestamp.IsZero() {
		r.Log.Info("removing finalizer from the managed-fusion-agent-config secret resource")
		r.managedFusionDeploymentSecret.SetFinalizers(utils.Remove(r.managedFusionDeploymentSecret.GetFinalizers(), managedFusionAgentFinalizer))
		if err := r.Client.Update(r.ctx, r.managedFusionDeploymentSecret); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from managed-fusion-agent-config secret: %v", err)
		}
		r.Log.Info("finallizer removed successfully")

		if err := r.removeOLMComponents(); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to remove agent CSV: %v", err)
		}
	} else if r.managedFusionDeploymentSecret.UID != "" {
		if !utils.Contains(r.managedFusionDeploymentSecret.GetFinalizers(), managedFusionAgentFinalizer) {
			r.Log.V(-1).Info("finalizer missing on the managed-fusion-agent-config secret resource, adding...")
			r.managedFusionDeploymentSecret.SetFinalizers(append(r.managedFusionDeploymentSecret.GetFinalizers(), managedFusionAgentFinalizer))
			if err := r.update(r.managedFusionDeploymentSecret); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update managed-fusion-agent-config secret with finalizer: %v", err)
			}
		}
		if err := r.reconcilePrometheus(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileAlertmanager(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileAlertmanagerConfigSecret(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileAlertmanagerConfig(); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *ManagedFusionDeploymentReconciler) reconcilePrometheus() error {
	r.Log.Info("Reconciling Prometheus")

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.prometheus, func() error {
		if err := r.own(r.prometheus); err != nil {
			return err
		}

		desired := templates.PrometheusTemplate.DeepCopy()
		utils.AddLabel(r.prometheus, monLabelKey, monLabelValue)

		// use the container image of kube-rbac-proxy that comes in deployer CSV
		// for prometheus kube-rbac-proxy sidecar
		deployerCSV, err := r.getCSVByPrefix(agentCSVPrefix)
		if err != nil {
			return fmt.Errorf("Unable to set image for kube-rbac-proxy container: %v", err)
		}

		deployerCSVDeployments := deployerCSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs
		var deployerCSVDeployment *opv1a1.StrategyDeploymentSpec = nil
		for key := range deployerCSVDeployments {
			deployment := &deployerCSVDeployments[key]
			if deployment.Name == "ocs-osd-controller-manager" {
				deployerCSVDeployment = deployment
			}
		}

		deployerCSVContainers := deployerCSVDeployment.Spec.Template.Spec.Containers
		var kubeRbacImage string
		for key := range deployerCSVContainers {
			container := deployerCSVContainers[key]
			if container.Name == "kube-rbac-proxy" {
				kubeRbacImage = container.Image
			}
		}

		prometheusContainers := desired.Spec.Containers
		for key := range prometheusContainers {
			container := &prometheusContainers[key]
			if container.Name == "kube-rbac-proxy" {
				container.Image = kubeRbacImage
			}
		}

		clusterVersion := &configv1.ClusterVersion{}
		clusterVersion.Name = "version"
		if err := r.get(clusterVersion); err != nil {
			return err
		}

		r.prometheus.Spec = desired.Spec
		r.prometheus.Spec.ExternalLabels["clusterId"] = string(clusterVersion.Spec.ClusterID)
		r.prometheus.Spec.Alerting.Alertmanagers[0].Namespace = r.Namespace
		r.prometheus.Spec.AdditionalAlertRelabelConfigs = &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: alertRelabelConfigSecretName,
			},
			Key: alertRelabelConfigSecretKey,
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("Failed to update Prometheus: %v", err)
	}

	return nil
}

func (r *ManagedFusionDeploymentReconciler) reconcileAlertmanager() error {
	r.Log.Info("Reconciling Alertmanager")
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.alertmanager, func() error {
		if err := r.own(r.alertmanager); err != nil {
			return err
		}

		desired := templates.AlertmanagerTemplate.DeepCopy()
		desired.Spec.AlertmanagerConfigSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{
				monLabelKey: monLabelValue,
			},
		}
		r.alertmanager.Spec = desired.Spec
		utils.AddLabel(r.alertmanager, monLabelKey, monLabelValue)

		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

type smtpConfig struct {
	Endpoint           string   `yaml:"endpoint"`
	Username           string   `yaml:"username"`
	Password           string   `yaml:"password"`
	FromAddress        string   `yaml:"fromAddress"`
	NotificationEmails []string `yaml:"notificationEmails"`
}

type pagerDutyConfig struct {
	SOPEndpoint string `yaml:"sopEndpoint"`
	SecretKey   string `yaml:"secretKey"`
}

func (r *ManagedFusionDeploymentReconciler) reconcileAlertmanagerConfigSecret() error {
	r.Log.Info("Reconciling AlertmanagerConfigSecret")

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.alertmanagerConfigSecret, func() error {
		if err := r.own(r.alertmanagerConfigSecret); err != nil {
			return err
		}

		managedFusionDeploymentSecretData := r.managedFusionDeploymentSecret.Data
		managedFusionDeploymentPagerDutyConfig, exist := managedFusionDeploymentSecretData[managedFusionAgentSecretPagerDutyKey]
		if !exist {
			return fmt.Errorf("pager_duty_config key not found in managed-fusion-agent-config secret")
		}
		if err := yaml.Unmarshal(managedFusionDeploymentPagerDutyConfig, r.pagerDutyConfigData); err != nil {
			return err
		}
		if r.pagerDutyConfigData.SecretKey == "" {
			return fmt.Errorf("managed-fusion-agent-config secret contains an empty pagerKey entry")
		}

		managedFusionDeploymentSMTPConfig, exist := managedFusionDeploymentSecretData[managedFusionAgentSecretSMTPKey]
		if !exist {
			return fmt.Errorf("smtp_config key not found in managed-fusion-agent-config secret")
		}
		if err := yaml.Unmarshal(managedFusionDeploymentSMTPConfig, r.smtpConfigData); err != nil {
			return err
		}
		if r.smtpConfigData.Password == "" {
			return fmt.Errorf("managed-fusion-agent-config secret contains an empty smtpPassword entry")
		}

		r.alertmanagerConfigSecret.Data = map[string][]byte{
			pagerDutyKey:    []byte(r.pagerDutyConfigData.SecretKey),
			smtpPasswordKey: []byte(r.smtpConfigData.Password),
		}

		return nil
	})

	return err
}

func (r *ManagedFusionDeploymentReconciler) reconcileAlertmanagerConfig() error {
	r.Log.Info("Reconciling AlertmanagerConfig")

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.alertmanagerConfig, func() error {
		if err := r.own(r.alertmanagerConfig); err != nil {
			return err
		}

		if r.pagerDutyConfigData.SOPEndpoint == "" {
			return fmt.Errorf("managed-fusion-agent-config secret does not contain a SOPEndpoint entry")
		}

		alertingAddressList := []string{}
		alertingAddressList = append(alertingAddressList,
			r.smtpConfigData.NotificationEmails...)

		if r.smtpConfigData.Username == "" {
			return fmt.Errorf("managed-fusion-agent-config secret does not contain a username entry")
		}
		if r.smtpConfigData.Endpoint == "" {
			return fmt.Errorf("managed-fusion-agent-config secret does not contain a endpoint entry")
		}
		if r.smtpConfigData.FromAddress == "" {
			return fmt.Errorf("managed-fusion-agent-config secret does not contain a fromAddress entry")
		}
		smtpHTML, err := ioutil.ReadFile(r.CustomerNotificationHTMLPath)
		if err != nil {
			return fmt.Errorf("unable to read customernotification.html file: %v", err)
		}

		desired := templates.AlertmanagerConfigTemplate.DeepCopy()
		for i := range desired.Spec.Receivers {
			receiver := &desired.Spec.Receivers[i]
			switch receiver.Name {
			case "pagerduty":
				receiver.PagerDutyConfigs[0].ServiceKey.Key = pagerDutyKey
				receiver.PagerDutyConfigs[0].ServiceKey.LocalObjectReference.Name = r.alertmanagerConfigSecret.Name
				receiver.PagerDutyConfigs[0].Details[0].Key = "SOP"
				receiver.PagerDutyConfigs[0].Details[0].Value = r.pagerDutyConfigData.SOPEndpoint
			case "SendGrid":
				if len(alertingAddressList) > 0 {
					receiver.EmailConfigs[0].Smarthost = r.smtpConfigData.Endpoint
					receiver.EmailConfigs[0].AuthUsername = r.smtpConfigData.Username
					receiver.EmailConfigs[0].AuthPassword.LocalObjectReference.Name = r.alertmanagerConfigSecret.Name
					receiver.EmailConfigs[0].AuthPassword.Key = smtpPasswordKey
					receiver.EmailConfigs[0].From = r.smtpConfigData.FromAddress
					receiver.EmailConfigs[0].To = strings.Join(alertingAddressList, ", ")
					receiver.EmailConfigs[0].HTML = string(smtpHTML)
				} else {
					r.Log.V(-1).Info("Customer Email for alert notification is not provided")
					receiver.EmailConfigs = []promv1a1.EmailConfig{}
				}
			}
		}
		r.alertmanagerConfig.Spec = desired.Spec
		utils.AddLabel(r.alertmanagerConfig, monLabelKey, monLabelValue)
		return nil
	})

	return err
}

func (r *ManagedFusionDeploymentReconciler) removeOLMComponents() error {

	r.Log.Info("deleting agent csv")
	if err := r.deleteCSVByPrefix(agentCSVPrefix); err != nil {
		return fmt.Errorf("Unable to delete csv: %v", err)
	} else {
		r.Log.Info("Agent csv removed successfully")
		return nil
	}
}

func (r *ManagedFusionDeploymentReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *ManagedFusionDeploymentReconciler) list(obj client.ObjectList) error {
	listOptions := client.InNamespace(r.Namespace)
	return r.Client.List(r.ctx, obj, listOptions)
}

func (r *ManagedFusionDeploymentReconciler) update(obj client.Object) error {
	return r.Client.Update(r.ctx, obj)
}

func (r *ManagedFusionDeploymentReconciler) delete(obj client.Object) error {
	if err := r.Client.Delete(r.ctx, obj); err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *ManagedFusionDeploymentReconciler) own(resource metav1.Object) error {
	// Ensure ManangedFusionDeployment secret ownership on a resource
	if err := ctrl.SetControllerReference(r.managedFusionDeploymentSecret, resource, r.Scheme); err != nil {
		return err
	}
	return nil
}

func (r *ManagedFusionDeploymentReconciler) deleteCSVByPrefix(name string) error {
	if csv, err := r.getCSVByPrefix(name); err == nil {
		return r.delete(csv)
	} else if errors.IsNotFound(err) {
		return nil
	} else {
		return err
	}
}

func (r *ManagedFusionDeploymentReconciler) getCSVByPrefix(name string) (*opv1a1.ClusterServiceVersion, error) {
	csvList := opv1a1.ClusterServiceVersionList{}
	if err := r.list(&csvList); err != nil {
		return nil, fmt.Errorf("unable to list csv resources: %v", err)
	}

	for index := range csvList.Items {
		candidate := &csvList.Items[index]
		if strings.HasPrefix(candidate.Name, name) {
			return candidate, nil
		}
	}
	return nil, errors.NewNotFound(opv1a1.Resource("csv"), fmt.Sprintf("unable to find a csv prefixed with %s", name))
}
