package controllers

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promv1a1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	v1 "github.com/red-hat-storage/ocs-osd-deployer/api/v1alpha1"
	utils "github.com/red-hat-storage/ocs-osd-deployer/testutils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ManagedOCS controller", func() {
	// Define utility constants for object names and testing timeouts/durations and intervals.
	const (
		timeout  = time.Second * 3
		interval = time.Millisecond * 250
	)

	ctx := context.Background()
	managedFusionDeploymentTemplate := &v1.ManagedFusionDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      managedFusionDeploymentName,
			Namespace: testPrimaryNamespace,
		},
	}
	managedFusionDeploymentSecretTemplate := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      managedFusionDeploymentSecretName,
			Namespace: testPrimaryNamespace,
		},
		Data: map[string][]byte{},
	}
	promTemplate := promv1.Prometheus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      prometheusName,
			Namespace: testPrimaryNamespace,
		},
	}
	promStsTemplate := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("prometheus-%s", prometheusName),
			Namespace: testPrimaryNamespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"label": "value"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"label": "value"},
				},
			},
		},
	}
	amTemplate := promv1.Alertmanager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      alertmanagerName,
			Namespace: testPrimaryNamespace,
		},
	}
	amStsTemplate := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("alertmanager-%s", alertmanagerName),
			Namespace: testPrimaryNamespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"label": "value"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"label": "value"},
				},
			},
		},
	}
	amConfigTemplate := promv1a1.AlertmanagerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      alertmanagerConfigName,
			Namespace: testPrimaryNamespace,
		},
	}
	csvTemplate := opv1a1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDeployerCSVName,
			Namespace: testPrimaryNamespace,
		},
	}

	setupUninstallConditions := func(
		shouldPrometheusBeReady bool,
		shouldAlertmanagerBeReady bool,
	) {
		// Setup alertmanager state
		am := amTemplate.DeepCopy()
		Expect(k8sClient.Get(ctx, utils.GetResourceKey(am), am)).Should(Succeed())
		desiredReplicas := int32(1)
		if am.Spec.Replicas != nil {
			desiredReplicas = *am.Spec.Replicas
		}
		amSts := amStsTemplate.DeepCopy()
		Expect(k8sClient.Get(ctx, utils.GetResourceKey(amSts), amSts)).Should(Succeed())
		if shouldAlertmanagerBeReady {
			amSts.Status.Replicas = desiredReplicas
			amSts.Status.ReadyReplicas = desiredReplicas
		} else {
			amSts.Status.Replicas = 0
			amSts.Status.ReadyReplicas = 0
		}
		Expect(k8sClient.Status().Update(ctx, amSts)).Should(Succeed())

		// Setup prometheus state
		prom := promTemplate.DeepCopy()
		Expect(k8sClient.Get(ctx, utils.GetResourceKey(prom), prom)).Should(Succeed())
		desiredReplicas = int32(1)
		if prom.Spec.Replicas != nil {
			desiredReplicas = *prom.Spec.Replicas
		}
		promSts := promStsTemplate.DeepCopy()
		Expect(k8sClient.Get(ctx, utils.GetResourceKey(promSts), promSts)).Should(Succeed())
		if shouldPrometheusBeReady {
			promSts.Status.Replicas = desiredReplicas
			promSts.Status.ReadyReplicas = desiredReplicas
		} else {
			promSts.Status.Replicas = 0
			promSts.Status.ReadyReplicas = 0
		}
		Expect(k8sClient.Status().Update(ctx, promSts)).Should(Succeed())
	}

	setupAlertmanagerConfigConditions := func(
		hasPagerConfig bool,
		hasSMTPConfig bool,
		hasPagerKey bool,
		hasSMTPPassword bool,
	) {
		managedFusionDeploymentSecret := managedFusionDeploymentSecretTemplate.DeepCopy()
		var managedFusionDeploymentSecretExists bool
		if err := k8sClient.Get(ctx, utils.GetResourceKey(managedFusionDeploymentSecret), managedFusionDeploymentSecret); err == nil {
			managedFusionDeploymentSecretExists = true
		} else if errors.IsNotFound(err) {
			managedFusionDeploymentSecretExists = false
		} else {
			Expect(err).ToNot(HaveOccurred())
		}
		if managedFusionDeploymentSecret.Data == nil {
			managedFusionDeploymentSecret.Data = make(map[string][]byte)
		}
		if hasPagerKey {
			managedFusionDeploymentSecret.Data["pagerKey"] = []byte("test-key")
		} else {
			managedFusionDeploymentSecret.Data["pagerKey"] = nil
		}
		if hasSMTPPassword {
			managedFusionDeploymentSecret.Data["smtpPassword"] = []byte("test-key")
		} else {
			managedFusionDeploymentSecret.Data["smtpPassword"] = []byte("")
		}
		if managedFusionDeploymentSecretExists {
			Expect(k8sClient.Update(ctx, managedFusionDeploymentSecret)).Should(Succeed())
		} else {
			Expect(k8sClient.Create(ctx, managedFusionDeploymentSecret)).Should(Succeed())
		}

		managedFusionDeploymentCR := managedFusionDeploymentTemplate.DeepCopy()
		var managedFusionDeploymentCRExists bool
		if err := k8sClient.Get(ctx, utils.GetResourceKey(managedFusionDeploymentCR), managedFusionDeploymentCR); err == nil {
			managedFusionDeploymentCRExists = true
		} else if errors.IsNotFound(err) {
			managedFusionDeploymentCRExists = false
		} else {
			Expect(err).ToNot(HaveOccurred())
		}
		if hasPagerConfig {
			managedFusionDeploymentCR.Spec.Pager.SOPEndpoint = "https://red-hat-storage.github.io/ocs-sop/sop/OSD"
		} else {
			managedFusionDeploymentCR.Spec.Pager.SOPEndpoint = ""
		}
		if hasSMTPConfig {
			managedFusionDeploymentCR.Spec.SMTP = v1.SMTPSpec{
				Endpoint:           "smtp.sendgrid.net:587",
				Username:           "test",
				FromAddress:        "noreply-test@test.com",
				NotificationEmails: []string{"test@test.com"},
			}
		} else {
			managedFusionDeploymentCR.Spec.SMTP = v1.SMTPSpec{}
		}
		if managedFusionDeploymentCRExists {
			Expect(k8sClient.Update(ctx, managedFusionDeploymentCR)).Should(Succeed())
		} else {
			Expect(k8sClient.Create(ctx, managedFusionDeploymentCR)).Should(Succeed())
		}
	}

	Context("reconcile()", Ordered, func() {
		When("there is no managedFusionDeployment CR in the cluster", func() {
			It("should not create a reconciled resources", func() {
				// Verify that a secret is not present
				deploymentCR := managedFusionDeploymentTemplate.DeepCopy()
				Expect(k8sClient.Get(ctx, utils.GetResourceKey(deploymentCR), deploymentCR)).Should(
					WithTransform(errors.IsNotFound, BeTrue()),
				)

				//Ensure, over a period of time, that the resources are not created
				resList := []client.Object{
					promTemplate.DeepCopy(),
					amTemplate.DeepCopy(),
				}
				utils.EnsureNoResources(k8sClient, ctx, resList, timeout, interval)
			})
		})
		When("there is a valid managedFusionDeployment CR in the cluster", func() {
			It("should create reconciled resources", func() {

				// Create a valid add-on parameters secret
				deploymentCR := managedFusionDeploymentTemplate.DeepCopy()
				// deploymentCR.Spec.Pager.SOPEndpoint = "https://red-hat-storage.github.io/ocs-sop/sop/OSD"
				// deploymentCR.Spec.SMTP.Endpoint = "smtp.sendgrid.net:587"
				// deploymentCR.Spec.SMTP.Username = "test"
				// deploymentCR.Spec.SMTP.FromAddress = "noreply-test@test.com"
				// deploymentCR.Spec.SMTP.NotificationEmails = []string{"test@test.com"}
				Expect(k8sClient.Create(ctx, deploymentCR)).Should(Succeed())

				By("Creating a prometheus resource")
				utils.WaitForResource(k8sClient, ctx, promTemplate.DeepCopy(), timeout, interval)

				By("Creating an alertmanager resource")
				utils.WaitForResource(k8sClient, ctx, amTemplate.DeepCopy(), timeout, interval)
			})
		})
		When("prometheus has non-ready replicas", func() {
			It("should reflect that in the ManagedOCS resource status", func() {
				By("by setting Status.Components.Prometheus.State to Pending")
				// Updating the status of the prometheus statefulset should trigger a reconcile for managedocs
				promSts := promStsTemplate.DeepCopy()
				Expect(k8sClient.Create(ctx, promSts)).Should(Succeed())

				promSts.Status.Replicas = 0
				promSts.Status.ReadyReplicas = 0
				Expect(k8sClient.Status().Update(ctx, promSts)).Should(Succeed())

				managedOCS := managedFusionDeploymentTemplate.DeepCopy()
				key := utils.GetResourceKey(managedOCS)
				Eventually(func() v1.ComponentState {
					Expect(k8sClient.Get(ctx, key, managedOCS)).Should(Succeed())
					return managedOCS.Status.Components.Prometheus.State
				}, timeout, interval).Should(Equal(v1.ComponentPending))
			})
		})
		When("all prometheus replicas are ready", func() {
			It("should reflect that in the ManagedOCS resource status", func() {
				By("by setting Status.Components.Prometheus.State to Ready")
				prom := promTemplate.DeepCopy()
				Expect(k8sClient.Get(ctx, utils.GetResourceKey(prom), prom)).Should(Succeed())
				desiredReplicas := int32(1)
				if prom.Spec.Replicas != nil {
					desiredReplicas = *prom.Spec.Replicas
				}

				// Updating the status of the prometheus statefulset should trigger a reconcile for managedocs
				promSts := promStsTemplate.DeepCopy()
				promSts.Status.Replicas = desiredReplicas
				promSts.Status.ReadyReplicas = desiredReplicas
				Expect(k8sClient.Status().Update(ctx, promSts)).Should(Succeed())

				managedOCS := managedFusionDeploymentTemplate.DeepCopy()
				key := utils.GetResourceKey(managedOCS)
				Eventually(func() v1.ComponentState {
					Expect(k8sClient.Get(ctx, key, managedOCS)).Should(Succeed())
					return managedOCS.Status.Components.Prometheus.State
				}, timeout, interval).Should(Equal(v1.ComponentReady))
			})
		})
		When("alertmanager has non-ready replicas", func() {
			It("should reflect that in the ManagedOCS resource status", func() {
				By("by setting Status.Components.Alertmanager.State to Pending")
				// Updating the status of the alertmanager statefulset should trigger a reconcile for managedocs
				amSts := amStsTemplate.DeepCopy()
				Expect(k8sClient.Create(ctx, amSts)).Should(Succeed())

				amSts.Status.Replicas = 0
				amSts.Status.ReadyReplicas = 0
				Expect(k8sClient.Status().Update(ctx, amSts)).Should(Succeed())

				managedOCS := managedFusionDeploymentTemplate.DeepCopy()
				key := utils.GetResourceKey(managedOCS)
				Eventually(func() v1.ComponentState {
					Expect(k8sClient.Get(ctx, key, managedOCS)).Should(Succeed())
					return managedOCS.Status.Components.Alertmanager.State
				}, timeout, interval).Should(Equal(v1.ComponentPending))
			})
		})
		When("all alertmanager replicas are ready", func() {
			It("should reflect that in the ManagedOCS resource status", func() {
				By("by setting Status.Components.Alertmanager.State to Ready")
				am := amTemplate.DeepCopy()
				Expect(k8sClient.Get(ctx, utils.GetResourceKey(am), am)).Should(Succeed())
				desiredReplicas := int32(1)
				if am.Spec.Replicas != nil {
					desiredReplicas = *am.Spec.Replicas
				}

				// Updating the status of the alertmanager statefulset should trigger a reconcile for managedocs
				amSts := amStsTemplate.DeepCopy()
				amSts.Status.Replicas = desiredReplicas
				amSts.Status.ReadyReplicas = desiredReplicas
				Expect(k8sClient.Status().Update(ctx, amSts)).Should(Succeed())

				managedOCS := managedFusionDeploymentTemplate.DeepCopy()
				key := utils.GetResourceKey(managedOCS)
				Eventually(func() v1.ComponentState {
					Expect(k8sClient.Get(ctx, key, managedOCS)).Should(Succeed())
					return managedOCS.Status.Components.Alertmanager.State
				}, timeout, interval).Should(Equal(v1.ComponentReady))
			})
		})
		When("the prometheus resource is modified", func() {
			It("should revert the changes and bring the resource back to its managed state", func() {
				// Get an updated prometheus
				prom := promTemplate.DeepCopy()
				promKey := utils.GetResourceKey(prom)
				Expect(k8sClient.Get(ctx, promKey, prom)).Should(Succeed())

				// Update to empty spec
				spec := prom.Spec.DeepCopy()
				prom.Spec = promv1.PrometheusSpec{}
				Expect(k8sClient.Update(ctx, prom)).Should(Succeed())

				// Wait for the spec changes to be reverted
				Eventually(func() *promv1.PrometheusSpec {
					prom := promTemplate.DeepCopy()
					Expect(k8sClient.Get(ctx, promKey, prom)).Should(Succeed())
					return &prom.Spec
				}, timeout, interval).Should(Equal(spec))
			})
		})
		When("the prometheus resource is deleted", func() {
			It("should create a new prometheus in the namespace", func() {
				// Delete the prometheus resource
				Expect(k8sClient.Delete(ctx, promTemplate.DeepCopy())).Should(Succeed())

				// Wait for the prometheus to be recreated
				utils.WaitForResource(k8sClient, ctx, promTemplate.DeepCopy(), timeout, interval)
			})
		})
		When("the alertmanager resource is modified", func() {
			It("should revert the changes and bring the resource back to its managed state", func() {
				// Get an updated alertmanager
				am := amTemplate.DeepCopy()
				amKey := utils.GetResourceKey(am)
				Expect(k8sClient.Get(ctx, amKey, am)).Should(Succeed())

				// Update to empty spec
				spec := am.Spec.DeepCopy()
				am.Spec = promv1.AlertmanagerSpec{}
				Expect(k8sClient.Update(ctx, am)).Should(Succeed())

				// Wait for the spec changes to be reverted
				Eventually(func() *promv1.AlertmanagerSpec {
					am := amTemplate.DeepCopy()
					Expect(k8sClient.Get(ctx, amKey, am)).Should(Succeed())
					return &am.Spec
				}, timeout, interval).Should(Equal(spec))
			})
		})
		When("the alertmanager resource is deleted", func() {
			It("should create a new alertmanager in the namespace", func() {
				// Delete the alertmanager resource
				Expect(k8sClient.Delete(ctx, amTemplate.DeepCopy())).Should(Succeed())

				// Wait for the alertmanager to be recreated
				utils.WaitForResource(k8sClient, ctx, promTemplate.DeepCopy(), timeout, interval)
			})
		})
		When("there is no pagerduty config in the managedFusionDeployment CR", func() {
			It("should not create alertmanager config", func() {
				setupAlertmanagerConfigConditions(false, true, true, true)

				// Ensure, over a period of time, that the resources are not created
				utils.EnsureNoResource(k8sClient, ctx, amConfigTemplate.DeepCopy(), timeout, interval)
			})
		})
		When("there is no smtp config in the managedFusionDeployment CR", func() {
			It("should not create alertmanager config", func() {
				setupAlertmanagerConfigConditions(true, false, true, true)

				// Ensure, over a period of time, that the resources are not created
				utils.EnsureNoResource(k8sClient, ctx, amConfigTemplate.DeepCopy(), timeout, interval)

			})
		})
		When("there is no value for pagerKey in the managedFusion secret", func() {
			It("should not create alertmanager config", func() {
				setupAlertmanagerConfigConditions(true, true, false, true)

				// Ensure, over a period of time, that the resources are not created
				utils.EnsureNoResource(k8sClient, ctx, amConfigTemplate.DeepCopy(), timeout, interval)
			})
		})
		When("there is no SMTP password in managedFusion secret", func() {
			It("should not create alertmanager config", func() {
				setupAlertmanagerConfigConditions(true, true, true, false)

				// Ensure, over a period of time, that the resources are not created
				utils.EnsureNoResource(k8sClient, ctx, amConfigTemplate.DeepCopy(), timeout, interval)

			})
		})
		When("All conditions for creating an alertmanager config are met", func() {
			It("should create alertmanager config", func() {
				setupAlertmanagerConfigConditions(true, true, true, true)

				utils.WaitForResource(k8sClient, ctx, amConfigTemplate.DeepCopy(), timeout, interval)
			})
		})
		When("notification email address in the add-on parameter is updated", func() {
			It("should update alertmanager config with the updated notification email", func() {
				deployment := managedFusionDeploymentTemplate.DeepCopy()
				deploymentKey := utils.GetResourceKey(deployment)
				Expect(k8sClient.Get(ctx, deploymentKey, deployment)).Should(Succeed())

				// Update notification email in addon param secret
				deployment.Spec.SMTP.NotificationEmails[0] = "test-new@email.com"
				Expect(k8sClient.Update(ctx, deployment)).Should(Succeed())

				// Wait for alertmanager to get updated with smtp details
				amconfig := amConfigTemplate.DeepCopy()
				amconfigKey := utils.GetResourceKey(amconfig)
				utils.WaitForAlertManagerSMTPReceiverEmailConfigToUpdate(
					k8sClient,
					ctx,
					amconfigKey,
					[]string{"test-new@email.com"},
					"SendGrid",
					timeout,
					interval,
				)
			})
		})
		When("second notification email addresses is added in add-on parameter", func() {
			It("should update alertmanager config with the added notification email id", func() {
				deployment := managedFusionDeploymentTemplate.DeepCopy()
				deploymentKey := utils.GetResourceKey(deployment)
				Expect(k8sClient.Get(ctx, deploymentKey, deployment)).Should(Succeed())

				// Update notification email in addon param secret
				deployment.Spec.SMTP.NotificationEmails[0] = "test-0@email.com"
				deployment.Spec.SMTP.NotificationEmails = append(deployment.Spec.SMTP.NotificationEmails, "test-1@email.com")
				// deployment.Spec.SMTP.NotificationEmails[1] = "test-1@email.com"
				Expect(k8sClient.Update(ctx, deployment)).Should(Succeed())

				// Wait for alertmanager to get updated with smtp details
				amconfig := amConfigTemplate.DeepCopy()
				amconfigKey := utils.GetResourceKey(amconfig)
				utils.WaitForAlertManagerSMTPReceiverEmailConfigToUpdate(
					k8sClient,
					ctx,
					amconfigKey,
					[]string{"test-0@email.com", "test-1@email.com"},
					"SendGrid",
					timeout,
					interval,
				)
			})
		})
		When("second notification email address in the add-on parameter is removed", func() {
			It("should update alertmanager config by removing the second email address", func() {
				deployment := managedFusionDeploymentTemplate.DeepCopy()
				deploymentKey := utils.GetResourceKey(deployment)
				Expect(k8sClient.Get(ctx, deploymentKey, deployment)).Should(Succeed())

				// remove notification email from addon param secret
				deployment.Spec.SMTP.NotificationEmails = make([]string, 1)
				deployment.Spec.SMTP.NotificationEmails[0] = "test-0@email.com"
				Expect(k8sClient.Update(ctx, deployment)).Should(Succeed())

				// Wait for alertmanager to get updated with smtp details
				amconfig := amConfigTemplate.DeepCopy()
				amconfigKey := utils.GetResourceKey(amconfig)
				utils.WaitForAlertManagerSMTPReceiverEmailConfigToUpdate(
					k8sClient,
					ctx,
					amconfigKey,
					[]string{"test-0@email.com"},
					"SendGrid",
					timeout,
					interval,
				)
			})
		})
		When("there is no notification email address in the add-on parameter", func() {
			It("should update alertmanager config by removing the SMTP email configs", func() {
				deployment := managedFusionDeploymentTemplate.DeepCopy()
				deploymentKey := utils.GetResourceKey(deployment)
				Expect(k8sClient.Get(ctx, deploymentKey, deployment)).Should(Succeed())

				// remove notification email from addon param secret
				deployment.Spec.SMTP.NotificationEmails = nil
				Expect(k8sClient.Update(ctx, deployment)).Should(Succeed())

				// Wait for alertmanager to remove the email configs
				amconfig := amConfigTemplate.DeepCopy()
				amconfigKey := utils.GetResourceKey(amconfig)
				utils.WaitForAlertManagerSMTPReceiverEmailConfigToUpdate(
					k8sClient,
					ctx,
					amconfigKey,
					[]string{},
					"SendGrid",
					timeout,
					interval,
				)
			})
		})
		When("prometheus is not ready while all other uninstall conditions are met", func() {
			It("should not delete the managedOCS resource", func() {
				setupUninstallConditions(false, true)

				// Initiate uninstall of managed fusion deployment CR
				managedFusionDeploymentCR := managedFusionDeploymentTemplate.DeepCopy()
				Expect(k8sClient.Delete(ctx, managedFusionDeploymentCR)).Should(Succeed())

				managedFusionDeployment := managedFusionDeploymentTemplate.DeepCopy()
				key := utils.GetResourceKey((managedFusionDeployment))
				Consistently(func() error {
					return k8sClient.Get(ctx, key, managedFusionDeployment)
				}, timeout, interval).Should(Succeed())
			})
		})
		When("alertmanager is not ready while all other uninstall conditions are met", func() {
			It("should not delete the managedOCS resource", func() {
				setupUninstallConditions(true, false)

				managedFusionDeployment := managedFusionDeploymentTemplate.DeepCopy()
				key := utils.GetResourceKey((managedFusionDeployment))
				Consistently(func() error {
					return k8sClient.Get(ctx, key, managedFusionDeployment)
				}, timeout, interval).Should(Succeed())
			})
		})
		When("All uninstall conditions are met", func() {
			It("should delete the managedOCS", func() {
				setupUninstallConditions(true, true)

				managedFusionDeployment := managedFusionDeploymentTemplate.DeepCopy()
				key := utils.GetResourceKey((managedFusionDeployment))
				Eventually(func() bool {
					err := k8sClient.Get(ctx, key, managedFusionDeployment)
					return err != nil && errors.IsNotFound(err)
				}, timeout, interval).Should(BeTrue())
			})
			It("should delete the deployer csv", func() {
				csv := csvTemplate.DeepCopy()
				key := utils.GetResourceKey(csv)
				Eventually(func() bool {
					err := k8sClient.Get(ctx, key, csv)
					return err != nil && errors.IsNotFound(err)
				}, timeout, interval).Should(BeTrue())
			})
		})
	})
})
