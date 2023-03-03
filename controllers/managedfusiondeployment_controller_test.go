package controllers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promv1a1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	utils "github.com/red-hat-storage/managed-fusion-agent/testutils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gopkg.in/yaml.v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ManagedFusionDeployment controller", func() {
	// Define utility constants for object names and testing timeouts/durations and intervals.
	const (
		timeout  = time.Second * 3
		interval = time.Millisecond * 250
	)

	ctx := context.Background()
	managedFusionAgentSecretTemplate := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      managedFusionSecretName,
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
	amTemplate := promv1.Alertmanager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      alertmanagerName,
			Namespace: testPrimaryNamespace,
		},
	}
	amConfigTemplate := promv1a1.AlertmanagerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      alertmanagerConfigName,
			Namespace: testPrimaryNamespace,
		},
	}
	amConfigSecretTemplate := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      alertmanagerSecretName,
			Namespace: testPrimaryNamespace,
		},
	}
	csvTemplate := opv1a1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testAgentCSVName,
			Namespace: testPrimaryNamespace,
		},
	}

	setupManagedFusionSecretConditions := func(
		hasPagerConfig bool,
		hasSMTPConfig bool,
		hasPagerKey bool,
		hasSMTPPassword bool,
	) {
		managedFusionAgentSecret := managedFusionAgentSecretTemplate.DeepCopy()
		var managedFusionAgentSecretExists bool
		if err := k8sClient.Get(ctx, utils.GetResourceKey(managedFusionAgentSecret), managedFusionAgentSecret); err == nil {
			managedFusionAgentSecretExists = true
		} else if errors.IsNotFound(err) {
			managedFusionAgentSecretExists = false
		} else {
			Expect(err).ToNot(HaveOccurred())
		}
		if managedFusionAgentSecret.Data == nil {
			managedFusionAgentSecret.Data = make(map[string][]byte)
		}
		pagerDutyData := pagerDutyConfig{}
		if hasPagerKey {
			pagerDutyData.ServiceKey = "test-key"
		} else {
			pagerDutyData.ServiceKey = ""
		}
		if hasPagerConfig {
			pagerDutyData.SOPEndpoint = "https://red-hat-storage.github.io/ocs-sop/sop/OSD"
		} else {
			pagerDutyData.SOPEndpoint = ""
		}
		smtpData := smtpConfig{}
		if hasSMTPPassword {
			smtpData.Password = "test-key"
		} else {
			smtpData.Password = ""
		}
		if hasSMTPConfig {
			smtpData.Endpoint = "smtp.sendgrid.net:587"
			smtpData.FromAddress = "noreply-test@test.com"
			smtpData.Username = "test"
			smtpData.NotificationEmails = []string{"test@test.com"}
		} else {
			smtpData.Endpoint = ""
			smtpData.FromAddress = ""
			smtpData.Username = ""
			smtpData.NotificationEmails = []string{}
		}
		pdYAMLData, _ := yaml.Marshal(&pagerDutyData)
		managedFusionAgentSecret.Data["pager_duty_config"] = pdYAMLData
		smtpYAMLData, _ := yaml.Marshal(&smtpData)
		managedFusionAgentSecret.Data["smtp_config"] = smtpYAMLData
		if managedFusionAgentSecretExists {
			Expect(k8sClient.Update(ctx, managedFusionAgentSecret)).Should(Succeed())
		} else {
			Expect(k8sClient.Create(ctx, managedFusionAgentSecret)).Should(Succeed())
		}
	}
	// Valid secret structure - correct smtp and pager duty keys and data
	// Invalid secret structure - incorrect smtp and pager duty keys and data
	// Complete smtp data
	// Incomplete smtp data
	// Complete pager data
	// Incomplete pager data

	Context("reconcile()", Ordered, func() {
		When("there is no managedFusionAgent Secret in the cluster", func() {
			It("should not create a reconciled resources", func() {
				// Verify that a secret is not present
				agentSecret := managedFusionAgentSecretTemplate.DeepCopy()
				Expect(k8sClient.Get(ctx, utils.GetResourceKey(agentSecret), agentSecret)).Should(
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
		When("there is a invalid managedFusionAgent Secret in the cluster", func() {
			It("should not create reconciled resources", func() {
				// Create a secret with no keys
				agentSecret := managedFusionAgentSecretTemplate.DeepCopy()
				Expect(k8sClient.Create(ctx, agentSecret)).Should(Succeed())

				// Ensure, over a period of time, that the resources are not created
				resList := []client.Object{
					promTemplate.DeepCopy(),
					amTemplate.DeepCopy(),
				}
				utils.EnsureNoResources(k8sClient, ctx, resList, timeout, interval)
			})
		})
		When("there is a valid managedFusionAgent Secret in the cluster", func() {
			It("should create reconciled resources", func() {
				// Create a valid add-on parameters secret but with empty values
				setupManagedFusionSecretConditions(false, false, false, false)

				By("Creating a prometheus resource")
				utils.WaitForResource(k8sClient, ctx, promTemplate.DeepCopy(), timeout, interval)

				By("Creating an alertmanager resource")
				utils.WaitForResource(k8sClient, ctx, amTemplate.DeepCopy(), timeout, interval)
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
		When("there is no value for pagerKey in the managedFusion secret", func() {
			It("should not create alertmanager config", func() {
				setupManagedFusionSecretConditions(true, true, false, true)

				// Ensure, over a period of time, that the resources are not created
				utils.EnsureNoResource(k8sClient, ctx, amConfigSecretTemplate.DeepCopy(), timeout, interval)
			})
		})
		When("there is no SMTP password in managedFusion secret", func() {
			It("should not create alertmanager config", func() {
				setupManagedFusionSecretConditions(true, true, true, false)

				// Ensure, over a period of time, that the resources are not created
				utils.EnsureNoResource(k8sClient, ctx, amConfigSecretTemplate.DeepCopy(), timeout, interval)
			})
		})
		When("there is no pagerduty config in the managedFusion secret", func() {
			It("should not create alertmanager config", func() {
				setupManagedFusionSecretConditions(false, true, true, true)

				// Ensure, over a period of time, that the resources are not created
				utils.EnsureNoResource(k8sClient, ctx, amConfigTemplate.DeepCopy(), timeout, interval)
			})
		})
		When("there is no smtp config in the managedFusion secret", func() {
			It("should not create alertmanager config", func() {
				setupManagedFusionSecretConditions(true, false, true, true)

				// Ensure, over a period of time, that the resources are not created
				utils.EnsureNoResource(k8sClient, ctx, amConfigTemplate.DeepCopy(), timeout, interval)

			})
		})
		When("All conditions for creating an alertmanager config are met", func() {
			It("should create alertmanager config", func() {
				setupManagedFusionSecretConditions(true, true, true, true)

				utils.WaitForResource(k8sClient, ctx, amConfigSecretTemplate.DeepCopy(), timeout, interval)
				utils.WaitForResource(k8sClient, ctx, amConfigTemplate.DeepCopy(), timeout, interval)
			})
		})
		When("notification email address in the add-on parameter is updated", func() {
			It("should update alertmanager config with the updated notification email", func() {
				agentSecret := managedFusionAgentSecretTemplate.DeepCopy()
				agentSecretKey := utils.GetResourceKey(agentSecret)
				Expect(k8sClient.Get(ctx, agentSecretKey, agentSecret)).Should(Succeed())

				// Update notification email in addon param secret
				smtpData := smtpConfig{
					Endpoint:           "smtp.sendgrid.net:587",
					FromAddress:        "noreply-test@test.com",
					Username:           "test",
					NotificationEmails: []string{"test-new@email.com"},
					Password:           "test-key",
				}
				smtpYAMLData, _ := yaml.Marshal(&smtpData)
				agentSecret.Data["smtp_config"] = smtpYAMLData
				Expect(k8sClient.Update(ctx, agentSecret)).Should(Succeed())

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
		When("second notification email addresses is added in managedFusion secret", func() {
			It("should update alertmanager config with the added notification email id", func() {
				agentSecret := managedFusionAgentSecretTemplate.DeepCopy()
				agentSecretKey := utils.GetResourceKey(agentSecret)
				Expect(k8sClient.Get(ctx, agentSecretKey, agentSecret)).Should(Succeed())

				// Update notification email in addon param secret
				smtpData := smtpConfig{
					Endpoint:           "smtp.sendgrid.net:587",
					FromAddress:        "noreply-test@test.com",
					Username:           "test",
					NotificationEmails: []string{"test-0@email.com", "test-1@email.com"},
					Password:           "test-key",
				}
				smtpYAMLData, _ := yaml.Marshal(&smtpData)
				agentSecret.Data["smtp_config"] = smtpYAMLData
				Expect(k8sClient.Update(ctx, agentSecret)).Should(Succeed())

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
		When("second notification email address in the managedFusion secret is removed", func() {
			It("should update alertmanager config by removing the second email address", func() {
				agentSecret := managedFusionAgentSecretTemplate.DeepCopy()
				agentSecretKey := utils.GetResourceKey(agentSecret)
				Expect(k8sClient.Get(ctx, agentSecretKey, agentSecret)).Should(Succeed())

				// remove notification email from addon param secret
				smtpData := smtpConfig{
					Endpoint:           "smtp.sendgrid.net:587",
					FromAddress:        "noreply-test@test.com",
					Username:           "test",
					NotificationEmails: []string{"test-0@email.com"},
					Password:           "test-key",
				}
				smtpYAMLData, _ := yaml.Marshal(&smtpData)
				agentSecret.Data["smtp_config"] = smtpYAMLData
				Expect(k8sClient.Update(ctx, agentSecret)).Should(Succeed())

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
		When("there is no notification email address in the managedFusion secret", func() {
			It("should update alertmanager config by removing the SMTP email configs", func() {
				agentSecret := managedFusionAgentSecretTemplate.DeepCopy()
				agentSecretKey := utils.GetResourceKey(agentSecret)
				Expect(k8sClient.Get(ctx, agentSecretKey, agentSecret)).Should(Succeed())

				// remove notification email from addon param secret
				smtpData := smtpConfig{
					Endpoint:           "smtp.sendgrid.net:587",
					FromAddress:        "noreply-test@test.com",
					Username:           "test",
					NotificationEmails: []string{},
					Password:           "test-key",
				}
				smtpYAMLData, _ := yaml.Marshal(&smtpData)
				agentSecret.Data["smtp_config"] = smtpYAMLData
				Expect(k8sClient.Update(ctx, agentSecret)).Should(Succeed())

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
		When("All uninstall conditions are met", func() {
			It("should delete the managedFusion secret", func() {
				Expect(k8sClient.Delete(ctx, managedFusionAgentSecretTemplate.DeepCopy())).Should(Succeed())

				agentSecret := managedFusionAgentSecretTemplate.DeepCopy()
				key := utils.GetResourceKey((agentSecret))
				Eventually(func() bool {
					err := k8sClient.Get(ctx, key, agentSecret)
					return err != nil && errors.IsNotFound(err)
				}, timeout, interval).Should(BeTrue())
			})
			It("should delete the agent csv", func() {
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
