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
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	cloudprovider "k8s.io/cloud-provider"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	deleteNodeEvent = "DeletingNode"
)

type providerNodeStatus int

func (s providerNodeStatus) String() string {
	switch s {
	case providerNodeStatusShutdown:
		return "Shutdown"
	case providerNodeStatusNotFound:
		return "Not Found"
	default:
		return "Unknown"
	}
}

const (
	providerNodeStatusUnknown providerNodeStatus = iota
	providerNodeStatusShutdown
	providerNodeStatusNotFound
)

// nodeReconciler reconciles a Node object
type nodeReconciler struct {
	cloud          cloudprovider.Interface
	client         client.Client
	recorder       record.EventRecorder
	cloudInstances cloudprovider.Instances
	log            logr.Logger
	scheme         *runtime.Scheme
	dryRun         bool
}

func RegisterNodeReconciler(mgr manager.Manager, cloud cloudprovider.Interface, dryRun bool) error {
	instances, success := cloud.Instances()
	if !success {
		return errors.New("unable to set up cloud instances provider")
	}

	r := &nodeReconciler{
		cloud:          cloud,
		recorder:       mgr.GetEventRecorderFor("cloud-lifecycle-controller"),
		client:         mgr.GetClient(),
		cloudInstances: instances,
		log:            ctrl.Log.WithName("controllers").WithName("Node"),
		scheme:         mgr.GetScheme(),
		dryRun:         dryRun,
	}

	return r.setupWithManager(mgr)
}

// Recursively check the list of nodes for any nodes that need to be removed from the cluster
func (r *nodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.log.WithValues("node", req.NamespacedName).V(1)

	node := &corev1.Node{}
	err := r.client.Get(ctx, req.NamespacedName, node)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Return and don't requeue
			logger.Info("Node deleted while performing reconciliation step")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Error fetching Node object from api client")
		return ctrl.Result{}, err
	}

	status, err := getNodeReadyCondition(node.Status.Conditions)
	if err != nil {
		logger.Error(err, "Unable to get node ready condition.")
		return ctrl.Result{}, err
	}

	logger.Info("Node status", "status", status)

	// Operate on nodes that are not ready (ready=false) or conspicuously missing (ready=unknown)
	// TODO: does NodeTermination feature gate change the status to 'Shutdown'? If so, where's the value for that in corev1?
	switch status.Status {
	case corev1.ConditionFalse, corev1.ConditionUnknown:
		logger.Info("Node appears down according to APIServer, investigating", "status", status.Status)
		return r.reconcileNode(ctx, node, logger)
	default:
		logger.Info("Node is up according to APIServer, ignoring.")
	}

	return ctrl.Result{}, nil
}

// setupWithManager sets up the controller with the Manager.
func (r *nodeReconciler) setupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Complete(r)
}

func (r *nodeReconciler) getProviderID(node *corev1.Node) (string, error) {
	id := node.Spec.ProviderID
	if id != "" {
		return id, nil
	}

	return generateProviderID(r.cloud, node)
}

func (r *nodeReconciler) nodeStatus(ctx context.Context, node *corev1.Node) (providerNodeStatus, error) {
	providerID, err := r.getProviderID(node)
	if err != nil {
		return providerNodeStatusUnknown, err
	}

	nodeExists, err := r.cloudInstances.InstanceExistsByProviderID(ctx, providerID)
	if err != nil && !isAWSNotFoundErr(err) { // This is a hack to work around aws bug
		return providerNodeStatusUnknown, err
	}
	if !nodeExists {
		return providerNodeStatusNotFound, nil
	}

	nodeShutdown, err := r.cloudInstances.InstanceShutdownByProviderID(ctx, providerID)
	if err != nil && !isAWSNotFoundErr(err) { // This is a hack to work around aws bug
		return providerNodeStatusUnknown, err
	}
	if nodeShutdown {
		return providerNodeStatusShutdown, nil
	}
	return providerNodeStatusUnknown, nil
}

func (r *nodeReconciler) reconcileNode(ctx context.Context, node *corev1.Node, logger logr.Logger) (ctrl.Result, error) {
	nodeStatus, err := r.nodeStatus(ctx, node)
	if err != nil {
		logger.Error(err, "Unable to get node status")
	}

	if nodeStatus == providerNodeStatusUnknown {
		// If kubelet on a node is turned off as part of a shutdown, the health check may mark the node as
		// unreachable/unhealthy before the node is actually shut down in the cloud provider.
		// If this happens, we need to schedule another check on this node in a few minutes to see if the cloud provider
		// says the instance is missing
		logger.Info("Requeuing reconciliation for node to let cloud status settle (node may be shutting down)")
		return ctrl.Result{Requeue: true}, nil
	}

	logger.Info(
		"Node condition matches unhealthy criteria",
		"nodeStatus", nodeStatus.String(),
	)

	ref := newNodeRef(node)
	msg := fmt.Sprintf("Deleting node %s because node status is %s", node.Name, nodeStatus.String())
	logger.Info(msg)
	r.recorder.Event(ref, corev1.EventTypeNormal, deleteNodeEvent, msg)

	// Nuke 'em, captain.
	if !r.dryRun {
		err := r.client.Delete(ctx, node)
		if err != nil {
			logger.Error(err, "Unable to delete node")
		}
		return ctrl.Result{}, err
	}
	logger.Info("Dry run: skipping node deletion")
	return ctrl.Result{}, nil
}

func isAWSNotFoundErr(err error) bool {
	return strings.Contains(err.Error(), "does not exist")
}

// Filter to only the NodeReady condition
func getNodeReadyCondition(status []corev1.NodeCondition) (corev1.NodeCondition, error) {
	for _, condition := range status {
		if condition.Type == corev1.NodeReady {
			return condition, nil
		}
	}
	return corev1.NodeCondition{}, errors.New("unable to find NodeReady condition. something is wrong, bruh")
}

func newNodeRef(node *corev1.Node) *corev1.ObjectReference {
	return &corev1.ObjectReference{
		Kind:      "Node",
		Name:      node.Name,
		UID:       node.UID,
		Namespace: "",
	}
}
