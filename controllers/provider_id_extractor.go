/*
Copyright 2021.

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
	"errors"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/legacy-cloud-providers/azure"

	corev1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
)

var providerIDBuilders = map[string]func(cloud cloudprovider.Interface, node *corev1.Node) (string, error){
	"azure": azureProviderIDBuilder,
	"aws":   awsProviderIDBuilder,
}

var (
	// ErrProviderNotSupported is returned when an attempt is made
	// to generate a provider id for an unsupported provider.
	ErrProviderNotSupported = errors.New("provider not supported")

	// ErrInvalidVMName is returned when an invalid VM name is found.
	ErrInvalidVMName = errors.New("vm id is invalid")
)

func generateProviderID(cloud cloudprovider.Interface, node *corev1.Node) (string, error) {
	f, ok := providerIDBuilders[cloud.ProviderName()]
	if !ok {
		return "", ErrProviderNotSupported
	}
	return f(cloud, node)
}

// awsProviderIDBuilder takes a node name and returns a provider id.
// For example:
//   k8s-controllers-i-042988b09f6a493cc
// becomes:
//   aws:///i-042988b09f6a493cc
// error will always be ErrInvalidVMName.
func awsProviderIDBuilder(_ cloudprovider.Interface, node *corev1.Node) (string, error) {
	parts := strings.Split(node.Name, "-")
	if len(parts) != 4 || parts[2] != "i" {
		return "", ErrInvalidVMName
	}
	return fmt.Sprintf("aws:///%s-%s", parts[2], parts[3]), nil
}

func azureProviderIDBuilder(cloud cloudprovider.Interface, node *corev1.Node) (string, error) {
	name := node.Name
	azCloud, ok := cloud.(*azure.Cloud)
	if !ok {
		return "", errors.New("cloud provider is not azure")
	}

	scaleset, err := extractAzureScaleSet(name)
	if err != nil {
		return "", err
	}

	vmID, err := extractAzureVMID(name)
	if err != nil {
		return "", err
	}

	if azCloud.Config.VMType == "vmss" {
		return fmt.Sprintf(
			"azure:///subscriptions/%s/resourceGroups/%s/providers/Microsoft.Compute/virtualMachineScaleSets/%s/virtualMachines/%s",
			azCloud.SubscriptionID,
			azCloud.ResourceGroup,
			scaleset,
			vmID,
		), nil
	}
	return fmt.Sprintf(
		"azure:///subscriptions/%s/resourceGroups/%s/virtualMachines/%s",
		azCloud.SubscriptionID,
		azCloud.ResourceGroup,
		vmID,
	), nil
}

// extractAzureVMID takes a machine name and returns the ID. For example:
//   aks-agentpool-34751183-vmss001001
// becomes:
//    1001
// error will always be ErrInvalidVMName.
func extractAzureVMID(name string) (string, error) {
	u, err := strconv.ParseUint(name[len(name)-6:], 10, 64)
	if err != nil {
		return "", ErrInvalidVMName
	}
	return strconv.FormatUint(u, 10), nil
}

// extractAzureScaleSet takes a machine name and returns the scale set.
// For example:
//   aks-agentpool-34751183-vmss001001
// becomes:
//   aks-agentpool-34751183-vmss
// error will always be ErrInvalidVMName.
func extractAzureScaleSet(name string) (string, error) {
	if len(name) <= 6 {
		return "", ErrInvalidVMName
	}
	name = name[:len(name)-6]
	return name, nil
}
