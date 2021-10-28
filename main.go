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

package main

import (
	"flag"
	"io"
	"os"
	"strings"

	"github.com/nxtlytics/cloud-lifecycle-controller/controllers"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	cloudprovider "k8s.io/cloud-provider"
	ctrl "sigs.k8s.io/controller-runtime"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	_ "k8s.io/legacy-cloud-providers/aws"
	_ "k8s.io/legacy-cloud-providers/azure"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

// CLI flags
var (
	metricsAddr             string
	enableLeaderElection    bool
	leaderElectionNamespace string
	probeAddr               string
	cloudProvider           string
	cloudConfig             string
	dryRun                  bool
	opts                    zap.Options
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	// CLI flags
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&leaderElectionNamespace, "leader-election-namespace", "", "Namespace to use for leader election lease")
	flag.StringVar(&cloudProvider, "cloud", "", "Cloud provider to use (aws, azure, gcs, ...)")
	flag.StringVar(&cloudConfig, "cloud-config", "", "Path to cloud provider config file")
	flag.BoolVar(&dryRun, "dry-run", false, "Don't actually delete anything")
	opts = zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
}

func main() {
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	ctrlOpts := ctrl.Options{
		Scheme:                  scheme,
		MetricsBindAddress:      metricsAddr,
		Port:                    9443,
		HealthProbeBindAddress:  probeAddr,
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "cloud-lifecycle-controller.nxtlytics.com",
		LeaderElectionNamespace: leaderElectionNamespace,
		DryRunClient:            dryRun,
	}
	mgr, err := newManager(ctrlOpts)
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	var cloudConfigReader io.Reader
	if cloudProvider == "aws" && cloudConfig == "" {
		cloudConfigReader = strings.NewReader(awsConfig())
	} else if cloudConfig != "" {
		// read the cloud config file from disk per usual
		cloudConfigReader, err = os.Open(cloudConfig)
		if err != nil {
			setupLog.Error(err, "Unable to read cloud provider configuration", "config", cloudConfig)
			os.Exit(1)
		}
	} else {
		// no cloud config specified, no zone override... let the library automatically init, and propagagte errors up
		setupLog.Info("Proceeding without cloud config, relying on underlying cloud library for init")
	}

	cloud, err := cloudprovider.GetCloudProvider(cloudProvider, cloudConfigReader)
	if err != nil {
		setupLog.Error(err, "Unable to initialize cloud provider", "provider", cloudProvider)
		os.Exit(1)
	}

	if err := controllers.RegisterNodeReconciler(mgr, cloud, dryRun); err != nil {
		setupLog.Error(err, "unable to register reconciler", "controller", "Node")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// awsConfig is basically just a mock config so aws will continue without a config.
func awsConfig() string {
	return `
[global]
zone=
KubernetesClusterID=FakeClusterID
VPC=FakeVPC
SubnetID=FakeSubnet
`
}

func newManager(opts ctrl.Options) (manager.Manager, error) {
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), opts)
	if err != nil {
		return nil, err
	}
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return nil, err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return nil, err
	}
	return mgr, nil
}
