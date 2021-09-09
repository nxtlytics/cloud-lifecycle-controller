# cloud-lifecycle-controller

`cloud-controller-manager` too heavy for your cluster?  
Need a simple way to remove nodes from your cluster once they're shut down / removed in the cloud provider?

Look no further!

## Background information

Normally, in Kubernetes =<1.20 clusters, you would either:
* Run `kube-controller-manager` and `kubelet` with the `-cloud aws` option; or
* Run the `cloud-controller-manager` cluster component with `kubelet -cloud external` option

or use the appropriate `cloud-provider-<provider>` cluster component (if you want to avoid using deprecated code)

...and this would handle node lifecycles (and much more) for you.

This introduces a few complexities such as the necessity of tagging your VPC, subnets, and security groups with the cluster name -
and the cloud functionality of the controller managers also may include unneeded functionality (ALB management, etc) that administrators may wish to disable.

`Thunder` is a multi-cloud (AWS/Azure) Kubernetes stack and distribution that does not use either of those components 
and handles tagging on it's own, therefore `cloud-lifecycle-controller` was born.

Users can deploy this component to their cluster as a Deployment, or run it with `-leader-elect true` on all of the control plane instances in your cluster.

## How does this work?

`cloud-lifecycle-controller` places a watch on `APIGroup=core/v1,Kind=Node` and waits for any changes to happen.
Once a change is detected, the controller checks the status of the Node object to see if the `Ready` condition of the Node is `Unknown` or `False`.
If the node is in either of those statuses, the controller will call the cloud API to see if that instance ID exists in the provider.

If the node does not exist or is terminated in the cloud provider, the controller will delete the `Node` object from the Kubernetes API Server 
to prevent old Nodes from accumulating over time as nodes are rotated out of service.

### Graceful Node Shutdown

`cloud-lifecycle-controller` is designed to work in conjunction with the `GracefulNodeShutdown` kubelet feature gate (disabled by default in 1.20, enabled by default in 1.21).
The `GracefulNodeShutdown` kubelet feature will drain the node when an ACPI shutdown event is received by the node, allowing `cloud-lifecycle-controller`
to safely remove the Node object once the node is removed from the cloud provider.  

## Usage

```
Usage of cloud-lifecycle-controller:
  -cloud string
        Cloud provider to use (aws, azure, gcs, ...)
  -cloud-config string
        Path to cloud provider config file
  -dry-run
        Don't actually delete anything
  -health-probe-bind-address string
        The address the probe endpoint binds to. (default ":8081")
  -kubeconfig string
        Paths to a kubeconfig. Only required if out-of-cluster.
  -leader-elect
        Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.
  -leader-election-namespace string
        Namespace to use for leader election lease
  -metrics-bind-address string
        The address the metric endpoint binds to. (default ":8080")
  -zap-devel
        Development Mode defaults(encoder=consoleEncoder,logLevel=Debug,stackTraceLevel=Warn). Production Mode defaults(encoder=jsonEncoder,logLevel=Info,stackTraceLevel=Error) (default true)
  -zap-encoder value
        Zap log encoding (one of 'json' or 'console')
  -zap-log-level value
        Zap Level to configure the verbosity of logging. Can be one of 'debug', 'info', 'error', or any integer value > 0 which corresponds to custom debug levels of increasing verbosity
  -zap-stacktrace-level value
        Zap Level at and above which stacktraces are captured (one of 'info', 'error').
```

## Sample log output

```
2021-04-21T12:55:31.515-0500    INFO    controller-runtime.metrics      metrics server is starting to listen    {"addr": ":8080"}
2021-04-21T12:55:31.518-0500    INFO    setup   starting manager
2021-04-21T12:55:31.518-0500    INFO    controller-runtime.manager      starting metrics server {"path": "/metrics"}
2021-04-21T12:55:31.518-0500    INFO    controller-runtime.manager.controller.node      Starting EventSource    {"reconciler group": "", "reconciler kind": "Node", "source": "kind source: /, Kind="}
2021-04-21T12:55:31.623-0500    INFO    controller-runtime.manager.controller.node      Starting Controller     {"reconciler group": "", "reconciler kind": "Node"}
2021-04-21T12:55:31.623-0500    INFO    controller-runtime.manager.controller.node      Starting workers        {"reconciler group": "", "reconciler kind": "Node", "worker count": 1}
2021-04-21T12:55:31.623-0500    DEBUG   controllers.Node        Node has no ProviderID, falling back to extracting instance ID from node name   {"node": "/k8s-controllers-sandbox-i-08767046ceb620753"}
2021-04-21T12:55:31.623-0500    DEBUG   controllers.Node        Built ProviderID from node name {"node": "/k8s-controllers-sandbox-i-08767046ceb620753", "providerID": "aws:///i-08767046ceb620753"}
2021-04-21T12:55:31.623-0500    DEBUG   controllers.Node        Node status     {"node": "/k8s-controllers-sandbox-i-08767046ceb620753", "status": {"type":"Ready","status":"True","lastHeartbeatTime":"2021-04-21T17:50:57Z","lastTransitionTime":"2021-03-04T00:41:05Z","reason":"KubeletReady","message":"kubelet is posting ready status"}}
```

---

**This project is completely alpha, don't use it on your production cluster.** I won't take responsibility if it eats your cluster, your lunch, or both.