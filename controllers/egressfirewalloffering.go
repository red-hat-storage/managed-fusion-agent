package controllers

import (
	"fmt"
	"os"
	"strings"

	openshiftv1 "github.com/openshift/api/network/v1"

	ovnv1 "github.com/ovn-org/ovn-kubernetes/go-controller/pkg/crd/egressfirewall/v1"
	"github.com/red-hat-storage/managed-fusion-agent/templates"
	"github.com/red-hat-storage/managed-fusion-agent/utils"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (r *ManagedFusionOfferingReconciler) reconcileEgressFirewall() error {
	if !r.AvailableCRDs[EgressFirewallCRD] {
		r.Log.Info("EgressFirewall CRD not present, skipping reconcile for egress firewall")
		// return nil
	}

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.egressFirewall, func() error {
		// if err := r.own(r.egressFirewall); err != nil {
		// 	return err
		// }
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

func (r *ManagedFusionOfferingReconciler) reconcileEgressNetworkPolicy() error {
	if !r.AvailableCRDs[EgressNetworkPolicyCRD] {
		r.Log.Info("EgressNetworkPolicy CRD not present, skipping reconcile for egress network policy")
		// return nil
	}
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.egressNetworkPolicy, func() error {
		// if err := r.own(r.egressNetworkPolicy); err != nil {
		// 	return err
		// }
		desiredEgressFirewallSpec, err := r.getEgressFirewallDesiredState()
		if err != nil {
			return err
		}

		r.egressNetworkPolicy.Spec = *egressFirewallSpecToEgressNetworkPolicySpecOffering(desiredEgressFirewallSpec)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update egressNetworkPolicy: %v", err)
	}
	return nil
}

func (r *ManagedFusionOfferingReconciler) getEgressFirewallDesiredState() (*ovnv1.EgressFirewallSpec, error) {
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
	imdsConfigMap.Namespace = os.Getenv("NAMESPACE")
	if err := r.get(imdsConfigMap); err != nil {
		r.Log.Info("unable to get AWS IMDS ConfigMap")
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

	allowEgressRules = append(
		allowEgressRules,
	)

	// The order or rules matter
	// https://docs.openshift.com/container-platform/4.10/networking/openshift_sdn/configuring-egress-firewall.html#policy-rule-order_openshift-sdn-egress-firewall
	// Inserting EgressNetworkPolicyRuleAllow rule in the front ensures they come before the EgressNetworkPolicyRuleDeny rule.
	desired.Spec.Egress = append(allowEgressRules, denyEgressRules...)
	return &desired.Spec, nil
}

func egressFirewallSpecToEgressNetworkPolicySpecOffering(firewallSpec *ovnv1.EgressFirewallSpec) *openshiftv1.EgressNetworkPolicySpec {
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
