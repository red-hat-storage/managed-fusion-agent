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
	"net/url"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	openshiftv1 "github.com/openshift/api/network/v1"
	ovnv1 "github.com/ovn-org/ovn-kubernetes/go-controller/pkg/crd/egressfirewall/v1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promv1a1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	"github.com/red-hat-storage/managed-fusion-agent/templates"
	"github.com/red-hat-storage/managed-fusion-agent/utils"
	netv1 "k8s.io/api/networking/v1"
)

const (
	EgressFirewallCRD      = "egressfirewalls.k8s.ovn.org"
	EgressNetworkPolicyCRD = "egressnetworkpolicies.network.openshift.io"

	managedFusionFinalizer           = "managedfusion.ibm.com/finalizer"
	managedFusionSecretName          = "managed-fusion-agent-config"
	prometheusName                   = "managed-fusion-prometheus"
	prometheusServiceName            = "prometheus"
	prometheusProxyNetworkPolicyName = "prometheus-proxy-rule"
	k8sMetricsServiceMonitorName     = "k8s-metrics-service-monitor"
	alertmanagerName                 = "managed-fusion-alertmanager"
	alertmanagerConfigName           = "managed-fusion-alertmanager-config"
	alertmanagerSecretName           = "managed-fusion-alertmanager-secret"
	alertRelabelConfigSecretName     = "managed-fusion-alert-relabel-config-secret"
	alertRelabelConfigSecretKey      = "alertrelabelconfig.yaml"
	agentCSVPrefix                   = "managed-fusion-agent"
	monLabelKey                      = "app"
	monLabelValue                    = "managed-fusion-agent"
	amSecretPagerDutyServiceKeyKey   = "pagerDutyServiceKey"
	amSecretSMTPAuthPasswordKey      = "smtpAuthPassword"
	egressNetworkPolicyName          = "egress-rule"
	egressFirewallName               = "default"
)

type ManagedFusionReconciler struct {
	Client                       client.Client
	UnrestrictedClient           client.Client
	Log                          logr.Logger
	Scheme                       *runtime.Scheme
	Namespace                    string
	AvailableCRDs                map[string]bool
	CustomerNotificationHTMLPath string

	ctx                          context.Context
	prometheus                   *promv1.Prometheus
	prometheusKubeRBACConfigMap  *corev1.ConfigMap
	prometheusService            *corev1.Service
	prometheusProxyNetworkPolicy *netv1.NetworkPolicy
	alertmanager                 *promv1.Alertmanager
	alertmanagerConfig           *promv1a1.AlertmanagerConfig
	alertmanagerSecret           *corev1.Secret
	alertRelabelConfigSecret     *corev1.Secret
	k8sMetricsServiceMonitor     *promv1.ServiceMonitor
	managedFusionSecret          *corev1.Secret
	smtpConfigData               *smtpConfig
	pagerDutyConfigData          *pagerDutyConfig
	egressNetworkPolicy          *openshiftv1.EgressNetworkPolicy
	egressFirewall               *ovnv1.EgressFirewall
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
	ServiceKey  string `yaml:"serviceKey"`
}

// Add necessary rbac permissions for managedfusion finalizer in order to set blockOwnerDeletion.
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources={alertmanagers,prometheuses,alertmanagerconfigs},verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=prometheusrules,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=podmonitors,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=servicemonitors,verbs=get;list;watch;update;patch;create;delete
// +kubebuilder:rbac:groups=operators.coreos.com,resources=clusterserviceversions,verbs=get;list;watch;delete;update;patch
// +kubebuilder:rbac:groups="apps",resources=statefulsets,verbs=get;list;watch
// +kubebuilder:rbac:groups="coordination.k8s.io",resources=leases,verbs=create;get;list;watch;update
// +kubebuilder:rbac:groups="",resources={secrets,secrets/finalizers},verbs=create;get;list;watch;update
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="networking.k8s.io",resources=networkpolicies,verbs=create;get;list;watch;update
// +kubebuilder:rbac:groups="network.openshift.io",resources=egressnetworkpolicies,verbs=create;get;list;watch;update
// +kubebuilder:rbac:groups="k8s.ovn.org",resources=egressfirewalls,verbs=create;get;list;watch;update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=create;get;list;watch;update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="config.openshift.io",resources=clusterversions,verbs=get;watch;list

// SetupWithManager creates an setup a ManagedFusion to work with the provided manager
func (r *ManagedFusionReconciler) SetupWithManager(mgr ctrl.Manager, ctrlOptions *controller.Options) error {
	if ctrlOptions == nil {
		ctrlOptions = &controller.Options{
			MaxConcurrentReconciles: 1,
		}
	}
	managedFusionSecretPredicates := builder.WithPredicates(
		predicate.NewPredicateFuncs(
			func(object client.Object) bool {
				name := object.GetName()
				namespace := object.GetNamespace()
				return name == managedFusionSecretName &&
					namespace == r.Namespace
			},
		),
	)
	monResourcesPredicates := builder.WithPredicates(
		predicate.NewPredicateFuncs(
			func(client client.Object) bool {
				labels := client.GetLabels()
				return labels == nil || labels[monLabelKey] != monLabelValue
			},
		),
	)
	enqueueManagedFusionAgentRequest := handler.EnqueueRequestsFromMapFunc(
		func(object client.Object) []reconcile.Request {
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Name:      managedFusionSecretName,
					Namespace: r.Namespace,
				},
			}}
		},
	)
	ctrlBuilder := ctrl.NewControllerManagedBy(mgr).
		WithOptions(*ctrlOptions).
		For(&corev1.Secret{}, managedFusionSecretPredicates).
		// Watch owned resources
		Owns(&promv1.Prometheus{}).
		Owns(&promv1.Alertmanager{}).
		Owns(&corev1.Secret{}).
		Owns(&promv1a1.AlertmanagerConfig{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&promv1.ServiceMonitor{}).
		Owns(&netv1.NetworkPolicy{}).
		Watches(
			&source.Kind{Type: &promv1.PodMonitor{}},
			enqueueManagedFusionAgentRequest,
			monResourcesPredicates,
		).
		Watches(
			&source.Kind{Type: &promv1.ServiceMonitor{}},
			enqueueManagedFusionAgentRequest,
			monResourcesPredicates,
		).
		Watches(
			&source.Kind{Type: &promv1.PrometheusRule{}},
			enqueueManagedFusionAgentRequest,
			monResourcesPredicates,
		)

	if r.AvailableCRDs[EgressFirewallCRD] {
		ctrlBuilder = ctrlBuilder.Owns(&ovnv1.EgressFirewall{})
	}
	if r.AvailableCRDs[EgressNetworkPolicyCRD] {
		ctrlBuilder = ctrlBuilder.Owns(&openshiftv1.EgressNetworkPolicy{})
	}
	return ctrlBuilder.Complete(r)
}

// Reconcile changes to all owned resource based on the infromation provided by the ManangedFusion secret resource
func (r *ManagedFusionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("req.Namespace", req.Namespace, "req.Name", req.Name)
	log.Info("Starting reconcile for ManangedFusion")

	r.initReconciler(ctx, req)
	if err := r.get(r.managedFusionSecret); err != nil {
		if errors.IsNotFound(err) {
			r.Log.V(-1).Info(fmt.Sprintf("%s secret resource not found", managedFusionSecretName))
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, fmt.Sprintf("Failed to get %s secret", managedFusionSecretName))
		return ctrl.Result{}, fmt.Errorf("failed to get %s secret: %v", managedFusionSecretName, err)
	}
	// Check if the structure of managedFusionSecret is valid or not
	if config, found := r.managedFusionSecret.Data["pager_duty_config"]; !found {
		return ctrl.Result{}, fmt.Errorf("%s secret does not contain pager_duty_config entry", managedFusionSecretName)
	} else if err := yaml.Unmarshal(config, r.pagerDutyConfigData); err != nil {
		return ctrl.Result{}, fmt.Errorf("%s.pager_duty_config is not in a valid yaml format: %v", managedFusionSecretName, err)
	}

	if config, found := r.managedFusionSecret.Data["smtp_config"]; !found {
		return ctrl.Result{}, fmt.Errorf("%s secret does not contain smtp_config entry", managedFusionSecretName)
	} else if err := yaml.Unmarshal(config, r.smtpConfigData); err != nil {
		return ctrl.Result{}, fmt.Errorf("%s.smtp_config is not in a valid yaml format: %v", managedFusionSecretName, err)
	}

	// Run the reconcile phases
	if result, err := r.reconcilePhases(); err != nil {
		r.Log.Error(err, "An error was encountered during reconcilePhases")
		return ctrl.Result{}, err
	} else {
		return result, nil
	}
}

func (r *ManagedFusionReconciler) initReconciler(ctx context.Context, req ctrl.Request) {
	r.ctx = ctx

	// This is an input resource
	r.managedFusionSecret = &corev1.Secret{}
	r.managedFusionSecret.Name = managedFusionSecretName
	r.managedFusionSecret.Namespace = r.Namespace

	r.prometheus = &promv1.Prometheus{}
	r.prometheus.Name = prometheusName
	r.prometheus.Namespace = r.Namespace

	r.prometheusKubeRBACConfigMap = &corev1.ConfigMap{}
	r.prometheusKubeRBACConfigMap.Name = templates.PrometheusKubeRBACPoxyConfigMapName
	r.prometheusKubeRBACConfigMap.Namespace = r.Namespace

	r.prometheusService = &corev1.Service{}
	r.prometheusService.Name = prometheusServiceName
	r.prometheusService.Namespace = r.Namespace

	r.k8sMetricsServiceMonitor = &promv1.ServiceMonitor{}
	r.k8sMetricsServiceMonitor.Name = k8sMetricsServiceMonitorName
	r.k8sMetricsServiceMonitor.Namespace = r.Namespace

	r.prometheusProxyNetworkPolicy = &netv1.NetworkPolicy{}
	r.prometheusProxyNetworkPolicy.Name = prometheusProxyNetworkPolicyName
	r.prometheusProxyNetworkPolicy.Namespace = r.Namespace

	r.alertmanager = &promv1.Alertmanager{}
	r.alertmanager.Name = alertmanagerName
	r.alertmanager.Namespace = r.Namespace

	r.alertmanagerSecret = &corev1.Secret{}
	r.alertmanagerSecret.Name = alertmanagerSecretName
	r.alertmanagerSecret.Namespace = r.Namespace

	r.alertmanagerConfig = &promv1a1.AlertmanagerConfig{}
	r.alertmanagerConfig.Name = alertmanagerConfigName
	r.alertmanagerConfig.Namespace = r.Namespace

	r.alertRelabelConfigSecret = &corev1.Secret{}
	r.alertRelabelConfigSecret.Name = alertRelabelConfigSecretName
	r.alertRelabelConfigSecret.Namespace = r.Namespace

	r.egressNetworkPolicy = &openshiftv1.EgressNetworkPolicy{}
	r.egressNetworkPolicy.Name = egressNetworkPolicyName
	r.egressNetworkPolicy.Namespace = r.Namespace

	r.egressFirewall = &ovnv1.EgressFirewall{}
	r.egressFirewall.Name = egressFirewallName
	r.egressFirewall.Namespace = r.Namespace

	r.smtpConfigData = &smtpConfig{}
	r.pagerDutyConfigData = &pagerDutyConfig{}
}

func (r *ManagedFusionReconciler) reconcilePhases() (reconcile.Result, error) {
	if !r.managedFusionSecret.DeletionTimestamp.IsZero() {
		if r.verifyOfferringsDoNotExist() {
			r.Log.Info(fmt.Sprintf("removing finalizer from %s secret", managedFusionSecretName))
			r.managedFusionSecret.SetFinalizers(utils.Remove(r.managedFusionSecret.GetFinalizers(), managedFusionFinalizer))
			if err := r.Client.Update(r.ctx, r.managedFusionSecret); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from %s secret: %v", managedFusionSecretName, err)
			}
			r.Log.Info("finallizer removed successfully")

			if err := r.initiateAgentUninstallation(); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to initiate agent uninstallation %v", err)
			}
		}

	} else if r.managedFusionSecret.UID != "" {
		if !utils.Contains(r.managedFusionSecret.GetFinalizers(), managedFusionFinalizer) {
			r.Log.V(-1).Info(fmt.Sprintf("finalizer missing on the %s secret resource, adding...", managedFusionSecretName))
			r.managedFusionSecret.SetFinalizers(append(r.managedFusionSecret.GetFinalizers(), managedFusionFinalizer))
			if err := r.update(r.managedFusionSecret); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update %s secret with finalizer: %v", managedFusionSecretName, err)
			}
		}

		if err := r.reconcileAlertRelabelConfigSecret(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcilePrometheusKubeRBACConfigMap(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcilePrometheusService(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcilePrometheus(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileAlertmanager(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileAlertmanagerSecret(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileAlertmanagerConfig(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileK8SMetricsServiceMonitor(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileMonitoringResources(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcilePrometheusProxyNetworkPolicy(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileEgressFirewall(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileEgressNetworkPolicy(); err != nil {
			return ctrl.Result{}, err
		}

	}

	return ctrl.Result{}, nil
}

// TODO Add logic to check if the offerings exists
func (r *ManagedFusionReconciler) verifyOfferringsDoNotExist() bool {
	return true
}

func (r *ManagedFusionReconciler) reconcilePrometheus() error {
	r.Log.Info("Reconciling Prometheus")

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.prometheus, func() error {
		if err := r.own(r.prometheus); err != nil {
			return err
		}

		desired := templates.PrometheusTemplate.DeepCopy()
		utils.AddLabel(r.prometheus, monLabelKey, monLabelValue)

		// use the container image of kube-rbac-proxy that comes in agent CSV
		// for prometheus kube-rbac-proxy sidecar
		agentCSV, err := r.getCSVByPrefix(agentCSVPrefix)
		if err != nil {
			return fmt.Errorf("Unable to set image for kube-rbac-proxy container: %v", err)
		}

		agentCSVDeployments := agentCSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs
		var agentCSVDeployment *opv1a1.StrategyDeploymentSpec = nil
		for key := range agentCSVDeployments {
			deployment := &agentCSVDeployments[key]
			if deployment.Name == "managed-fusion-controller-manager" {
				agentCSVDeployment = deployment
			}
		}

		agentCSVContainers := agentCSVDeployment.Spec.Template.Spec.Containers
		var kubeRbacImage string
		for key := range agentCSVContainers {
			container := agentCSVContainers[key]
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

func (r *ManagedFusionReconciler) reconcileAlertmanager() error {
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

func (r *ManagedFusionReconciler) reconcileAlertmanagerSecret() error {
	r.Log.Info("Reconciling AlertmanagerSecret")
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.alertmanagerSecret, func() error {
		if err := r.own(r.alertmanagerSecret); err != nil {
			return err
		}
		if r.pagerDutyConfigData.ServiceKey == "" {
			return fmt.Errorf("Agent PagerDuty configuration does not contain serviceKey entry")
		}
		if r.smtpConfigData.Password == "" {
			return fmt.Errorf("Agent SMTP configuration does not contain password entry")
		}
		r.alertmanagerSecret.Data = map[string][]byte{
			amSecretPagerDutyServiceKeyKey: []byte(r.pagerDutyConfigData.ServiceKey),
			amSecretSMTPAuthPasswordKey:    []byte(r.smtpConfigData.Password),
		}
		return nil
	})

	return err
}

func (r *ManagedFusionReconciler) reconcileAlertmanagerConfig() error {
	r.Log.Info("Reconciling AlertmanagerConfig")

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.alertmanagerConfig, func() error {
		if err := r.own(r.alertmanagerConfig); err != nil {
			return err
		}

		if r.pagerDutyConfigData.SOPEndpoint == "" {
			return fmt.Errorf("Agent PagerDuty configuration does not contain sopEndpoint entry")
		}
		if r.smtpConfigData.Username == "" {
			return fmt.Errorf("Agent SMTP configuration does not contain username entry")
		}
		if r.smtpConfigData.Endpoint == "" {
			return fmt.Errorf("Agent SMTP configuration does not contain endpoint entry")
		}
		if r.smtpConfigData.FromAddress == "" {
			return fmt.Errorf("Agent SMTP configuration does not contain fromAddress entry")
		}
		alertingAddressList := []string{}
		alertingAddressList = append(alertingAddressList,
			r.smtpConfigData.NotificationEmails...)
		smtpHTML, err := ioutil.ReadFile(r.CustomerNotificationHTMLPath)
		if err != nil {
			return fmt.Errorf("unable to read customernotification.html file: %v", err)
		}

		desired := templates.AlertmanagerConfigTemplate.DeepCopy()
		for i := range desired.Spec.Receivers {
			receiver := &desired.Spec.Receivers[i]
			switch receiver.Name {
			case "pagerduty":
				receiver.PagerDutyConfigs[0].ServiceKey.Key = amSecretPagerDutyServiceKeyKey
				receiver.PagerDutyConfigs[0].ServiceKey.LocalObjectReference.Name = r.alertmanagerSecret.Name
				receiver.PagerDutyConfigs[0].Details[0].Key = "SOP"
				receiver.PagerDutyConfigs[0].Details[0].Value = r.pagerDutyConfigData.SOPEndpoint
			case "SendGrid":
				if len(alertingAddressList) > 0 {
					receiver.EmailConfigs[0].Smarthost = r.smtpConfigData.Endpoint
					receiver.EmailConfigs[0].AuthUsername = r.smtpConfigData.Username
					receiver.EmailConfigs[0].AuthPassword.LocalObjectReference.Name = r.alertmanagerSecret.Name
					receiver.EmailConfigs[0].AuthPassword.Key = amSecretSMTPAuthPasswordKey
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

// AlertRelabelConfigSecret will have configuration for relabeling the alerts that are firing.
// It will add namespace label to firing alerts before they are sent to the alertmanager
func (r *ManagedFusionReconciler) reconcileAlertRelabelConfigSecret() error {
	r.Log.Info("Reconciling alertRelabelConfigSecret")

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.alertRelabelConfigSecret, func() error {
		if err := r.own(r.alertRelabelConfigSecret); err != nil {
			return err
		}

		alertRelabelConfig := []struct {
			SourceLabels []string `yaml:"source_labels"`
			TargetLabel  string   `yaml:"target_label,omitempty"`
			Replacement  string   `yaml:"replacement,omitempty"`
		}{
			{
				SourceLabels: []string{"namespace"},
				TargetLabel:  "alertnamespace",
			},
			{
				TargetLabel: "namespace",
				Replacement: r.Namespace,
			},
		}

		config, err := yaml.Marshal(alertRelabelConfig)
		if err != nil {
			return fmt.Errorf("Unable to encode alert relabel conifg: %v", err)
		}

		r.alertRelabelConfigSecret.Data = map[string][]byte{
			alertRelabelConfigSecretKey: config,
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("Unable to create/update AlertRelabelConfigSecret: %v", err)
	}

	return nil
}

func (r *ManagedFusionReconciler) reconcilePrometheusKubeRBACConfigMap() error {
	r.Log.Info("Reconciling kubeRBACConfigMap")

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.prometheusKubeRBACConfigMap, func() error {
		if err := r.own(r.prometheusKubeRBACConfigMap); err != nil {
			return err
		}

		r.prometheusKubeRBACConfigMap.Data = templates.KubeRBACProxyConfigMap.DeepCopy().Data

		return nil
	})

	if err != nil {
		return fmt.Errorf("unable to create kubeRBACConfig config map: %v", err)
	}

	return nil
}

// reconcilePrometheusService function wait for prometheus Service
// to start and sets appropriate annotation for 'service-ca' controller
func (r *ManagedFusionReconciler) reconcilePrometheusService() error {
	r.Log.Info("Reconciling PrometheusService")

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.prometheusService, func() error {
		if err := r.own(r.prometheusService); err != nil {
			return err
		}

		r.prometheusService.Spec.Ports = []corev1.ServicePort{
			{
				Name:       "https",
				Protocol:   corev1.ProtocolTCP,
				Port:       int32(templates.KubeRBACProxyPortNumber),
				TargetPort: intstr.FromString("https"),
			},
		}
		r.prometheusService.Spec.Selector = map[string]string{
			"app.kubernetes.io/name": r.prometheusService.Name,
		}
		utils.AddAnnotation(
			r.prometheusService,
			"service.beta.openshift.io/serving-cert-secret-name",
			templates.PrometheusServingCertSecretName,
		)
		utils.AddAnnotation(
			r.prometheusService,
			"service.alpha.openshift.io/serving-cert-secret-name",
			templates.PrometheusServingCertSecretName,
		)
		// This label is required to enable us to use metrics federation
		// mechanism provided by Managed-tenants
		utils.AddLabel(r.prometheusService, monLabelKey, monLabelValue)
		return nil
	})
	return err
}

func (r *ManagedFusionReconciler) reconcileK8SMetricsServiceMonitor() error {
	r.Log.Info("Reconciling k8sMetricsServiceMonitor")

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.k8sMetricsServiceMonitor, func() error {
		if err := r.own(r.k8sMetricsServiceMonitor); err != nil {
			return err
		}
		desired := templates.K8sMetricsServiceMonitorTemplate.DeepCopy()
		r.k8sMetricsServiceMonitor.Spec = desired.Spec
		return nil
	})
	if err != nil {
		return fmt.Errorf("Failed to update k8sMetricsServiceMonitor: %v", err)
	}
	return nil
}

// reconcileMonitoringResources labels all monitoring resources (ServiceMonitors, PodMonitors, and PrometheusRules)
// found in the target namespace with a label that matches the label selector the defined on the Prometheus resource
// we are reconciling in reconcilePrometheus. Doing so instructs the Prometheus instance to notice and react to these labeled
// monitoring resources
func (r *ManagedFusionReconciler) reconcileMonitoringResources() error {
	r.Log.Info("reconciling monitoring resources")

	podMonitorList := promv1.PodMonitorList{}
	if err := r.list(&podMonitorList); err != nil {
		return fmt.Errorf("Could not list pod monitors: %v", err)
	}
	for i := range podMonitorList.Items {
		obj := podMonitorList.Items[i]
		utils.AddLabel(obj, monLabelKey, monLabelValue)
		if err := r.update(obj); err != nil {
			return err
		}
	}

	serviceMonitorList := promv1.ServiceMonitorList{}
	if err := r.list(&serviceMonitorList); err != nil {
		return fmt.Errorf("Could not list service monitors: %v", err)
	}
	for i := range serviceMonitorList.Items {
		obj := serviceMonitorList.Items[i]
		utils.AddLabel(obj, monLabelKey, monLabelValue)
		if err := r.update(obj); err != nil {
			return err
		}
	}

	promRuleList := promv1.PrometheusRuleList{}
	if err := r.list(&promRuleList); err != nil {
		return fmt.Errorf("Could not list prometheus rules: %v", err)
	}
	for i := range promRuleList.Items {
		obj := promRuleList.Items[i]
		utils.AddLabel(obj, monLabelKey, monLabelValue)
		if err := r.update(obj); err != nil {
			return err
		}
	}

	return nil
}
func (r *ManagedFusionReconciler) reconcilePrometheusProxyNetworkPolicy() error {
	r.Log.Info("reconciling PrometheusProxyNetworkPolicy resources")

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.prometheusProxyNetworkPolicy, func() error {
		if err := r.own(r.prometheusProxyNetworkPolicy); err != nil {
			return err
		}
		desired := templates.PrometheusProxyNetworkPolicyTemplate.DeepCopy()
		r.prometheusProxyNetworkPolicy.Spec = desired.Spec
		return nil
	})
	if err != nil {
		return fmt.Errorf("Failed to update prometheus proxy NetworkPolicy: %v", err)
	}
	return nil
}

func (r *ManagedFusionReconciler) reconcileEgressFirewall() error {
	if !r.AvailableCRDs[EgressFirewallCRD] {
		r.Log.Info("EgressFirewall CRD not present, skipping reconcile for egress firewall")
		return nil
	}

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.egressFirewall, func() error {
		if err := r.own(r.egressFirewall); err != nil {
			return err
		}
		desiredEgressSpec, err := r.getEgressFirewallDesiredState()
		if err != nil {
			return err
		}
		r.egressFirewall.Spec = *desiredEgressSpec
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update egressFirewall: %v", err)
	}
	return nil

}

func (r *ManagedFusionReconciler) reconcileEgressNetworkPolicy() error {
	if !r.AvailableCRDs[EgressNetworkPolicyCRD] {
		r.Log.Info("EgressNetworkPolicy CRD not present, skipping reconcile for egress network policy")
		return nil
	}
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.egressNetworkPolicy, func() error {
		if err := r.own(r.egressNetworkPolicy); err != nil {
			return err
		}
		desiredEgressFirewallSpec, err := r.getEgressFirewallDesiredState()
		if err != nil {
			return err
		}

		r.egressNetworkPolicy.Spec = *egressFirewallSpecToEgressNetworkPolicySpec(desiredEgressFirewallSpec)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update egressNetworkPolicy: %v", err)
	}
	return nil
}

func (r *ManagedFusionReconciler) getEgressFirewallDesiredState() (*ovnv1.EgressFirewallSpec, error) {
	allowEgressRules := []ovnv1.EgressFirewallRule{}
	denyEgressRules := []ovnv1.EgressFirewallRule{}

	desired := templates.EgressFirewallTemplate.DeepCopy()
	for i := range desired.Spec.Egress {
		egressRule := desired.Spec.Egress[i]
		switch egressRule.Type {
		case ovnv1.EgressFirewallRuleAllow:
			allowEgressRules = append(allowEgressRules, egressRule)
		case ovnv1.EgressFirewallRuleDeny:
			denyEgressRules = append(denyEgressRules, egressRule)
		}
	}

	// Get the aws config map
	imdsConfigMap := &corev1.ConfigMap{}
	imdsConfigMap.Name = utils.IMDSConfigMapName
	imdsConfigMap.Namespace = r.Namespace
	if err := r.get(imdsConfigMap); err != nil {
		return nil, fmt.Errorf("unable to get AWS IMDS ConfigMap: %v", err)
	}

	// Get the machine cidr
	vpcCIDR, ok := imdsConfigMap.Data[utils.CIDRKey]
	if !ok {
		return nil, fmt.Errorf("unable to determine machine CIDR from AWS IMDS ConfigMap")
	}
	// Allow egress traffic to that cidr range
	cidrList := strings.Split(vpcCIDR, ";")
	for _, cidr := range cidrList {
		if cidr == "" {
			continue
		}
		allowEgressRules = append(allowEgressRules, ovnv1.EgressFirewallRule{
			Type: ovnv1.EgressFirewallRuleAllow,
			To: ovnv1.EgressFirewallDestination{
				CIDRSelector: cidr,
			},
		})
	}

	smtpURL, err := url.ParseRequestURI(r.smtpConfigData.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("unable to parse SMTP Endpoint url: %v", err)
	}
	if smtpURL.Hostname() == "" {
		return nil, fmt.Errorf("smtp config does not contain a host entry")
	}

	smtpEgressRule := ovnv1.EgressFirewallRule{}
	smtpEgressRule.To.DNSName = smtpURL.Hostname()
	smtpEgressRule.Type = ovnv1.EgressFirewallRuleAllow

	allowEgressRules = append(
		allowEgressRules,
		smtpEgressRule,
	)

	// The order or rules matter
	// https://docs.openshift.com/container-platform/4.10/networking/openshift_sdn/configuring-egress-firewall.html#policy-rule-order_openshift-sdn-egress-firewall
	// Inserting EgressNetworkPolicyRuleAllow rule in the front ensures they come before the EgressNetworkPolicyRuleDeny rule.
	desired.Spec.Egress = append(allowEgressRules, denyEgressRules...)
	return &desired.Spec, nil
}

func egressFirewallSpecToEgressNetworkPolicySpec(firewallSpec *ovnv1.EgressFirewallSpec) *openshiftv1.EgressNetworkPolicySpec {
	networkPolicySpec := &openshiftv1.EgressNetworkPolicySpec{}

	for i := range firewallSpec.Egress {
		firewalRule := &firewallSpec.Egress[i]
		networkPolicyRule := openshiftv1.EgressNetworkPolicyRule{}

		// Convert rule type
		switch firewalRule.Type {
		case ovnv1.EgressFirewallRuleAllow:
			networkPolicyRule.Type = openshiftv1.EgressNetworkPolicyRuleAllow
		case ovnv1.EgressFirewallRuleDeny:
			networkPolicyRule.Type = openshiftv1.EgressNetworkPolicyRuleDeny
		}

		// Convert Rule Peer
		networkPolicyRule.To = openshiftv1.EgressNetworkPolicyPeer{
			CIDRSelector: firewalRule.To.CIDRSelector,
			DNSName:      firewalRule.To.DNSName,
		}

		networkPolicySpec.Egress = append(
			networkPolicySpec.Egress,
			networkPolicyRule,
		)
	}

	return networkPolicySpec
}

func (r *ManagedFusionReconciler) initiateAgentUninstallation() error {
	r.Log.Info(fmt.Sprintf("deleting %s namespace", r.Namespace))
	if err := r.delete(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.Namespace,
		},
	}); err != nil {
		return fmt.Errorf("failed to delete namespace: %v", err)
	}
	return nil
}

func (r *ManagedFusionReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *ManagedFusionReconciler) list(obj client.ObjectList) error {
	listOptions := client.InNamespace(r.Namespace)
	return r.Client.List(r.ctx, obj, listOptions)
}

func (r *ManagedFusionReconciler) update(obj client.Object) error {
	return r.Client.Update(r.ctx, obj)
}

func (r *ManagedFusionReconciler) delete(obj client.Object) error {
	if err := r.Client.Delete(r.ctx, obj); err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *ManagedFusionReconciler) own(resource metav1.Object) error {
	// Ensure ManangedFusion secret ownership on a resource
	if err := ctrl.SetControllerReference(r.managedFusionSecret, resource, r.Scheme); err != nil {
		return err
	}
	return nil
}

func (r *ManagedFusionReconciler) getCSVByPrefix(name string) (*opv1a1.ClusterServiceVersion, error) {
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
