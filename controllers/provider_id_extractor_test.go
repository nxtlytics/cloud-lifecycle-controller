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
	"testing"

	"k8s.io/legacy-cloud-providers/azure"

	corev1 "k8s.io/api/core/v1"
)

func TestAwsProviderIDBuilder(t *testing.T) {
	tests := []struct {
		have    string
		want    string
		wantErr error
	}{
		{have: "k8s-controllers-i-042988b09f6a493cc", want: "aws:///i-042988b09f6a493cc"},
		{have: "042988b09f6a493cc", wantErr: ErrInvalidVMName},
	}

	node := &corev1.Node{}

	for _, test := range tests {
		node.Name = test.have
		got, err := awsProviderIDBuilder(nil, node)
		if err != test.wantErr || got != test.want {
			t.Fatalf(
				"awsProviderIDBuilder(_, name=%q) got %q, %v; want %q, %v",
				test.have,
				got,
				err,
				test.want,
				test.wantErr,
			)
		}
	}
}

func TestExtractAzureScaleSet(t *testing.T) {
	tests := []struct {
		have    string
		want    string
		wantErr error
	}{
		{have: "aks-agentpool-34751183-vmss000001", want: "aks-agentpool-34751183-vmss"},
		{have: "aks-agentpool-34751183-vmss999999", want: "aks-agentpool-34751183-vmss"},
		{have: "1234", want: "", wantErr: ErrInvalidVMName},
	}

	for _, test := range tests {
		got, err := extractAzureScaleSet(test.have)
		if err != test.wantErr || got != test.want {
			t.Fatalf("extractAzureScaleSet(%q) = %q, %v; want %q, %v", test.have, got, err, test.want, test.wantErr)
		}
	}
}

func TestExtractAzureVMID(t *testing.T) {
	tests := []struct {
		have    string
		want    string
		wantErr error
	}{
		{have: "aks-agentpool-34751183-vmss000001", want: "1"},
		{have: "aks-agentpool-34751183-vmss001001", want: "1001"},
		{have: "aks-agentpool-34751183-vmss999999", want: "999999"},
		{have: "aks-agentpool-34751183-vmssMyCustomName", want: "", wantErr: ErrInvalidVMName},
	}

	for _, test := range tests {
		got, err := extractAzureVMID(test.have)
		if err != test.wantErr || got != test.want {
			t.Fatalf("extractAzureVMID(%q) = %q, %v; want %q, %v", test.have, got, err, test.want, test.wantErr)
		}
	}
}

func TestAzureProviderIDBuilder(t *testing.T) {
	tests := []struct {
		haveResourceGroup  string
		haveSubscriptionID string
		haveVMType         string
		haveNodeName       string
		want               string
	}{
		{
			haveResourceGroup:  "mc_aks-my_kube-cluster_eastus2",
			haveSubscriptionID: "76786c64-3d1b-4f99-a9b5-40a79689adac",
			haveVMType:         "vmss",
			haveNodeName:       "aks-agentpool-34751183-vmss000001",
			want:               "azure:///subscriptions/76786c64-3d1b-4f99-a9b5-40a79689adac/resourceGroups/mc_aks-my_kube-cluster_eastus2/providers/Microsoft.Compute/virtualMachineScaleSets/aks-agentpool-34751183-vmss/virtualMachines/1",
		},
	}

	cloud := &azure.Cloud{}
	node := &corev1.Node{}

	for _, test := range tests {
		cloud.ResourceGroup = test.haveResourceGroup
		cloud.SubscriptionID = test.haveSubscriptionID
		cloud.VMType = test.haveVMType
		node.Name = test.haveNodeName

		got, err := azureProviderIDBuilder(cloud, node)
		if err != nil || got != test.want {
			t.Fatalf(
				"azureProviderIDBuilder({ResourceGroup=%q, SubscriptionID=%q}, %q) got %q, %v; want %q, nil",
				cloud.ResourceGroup,
				cloud.SubscriptionID,
				node.Name,
				got,
				err,
				test.want,
			)
		}
	}
}
