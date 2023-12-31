package main

import (
	"flag"
	piv1alpha1api "github.com/heliannuuthus/privateca-issuer/api/v1alpha1"
	"github.com/heliannuuthus/privateca-issuer/internal/controllers"
	"github.com/heliannuuthus/privateca-issuer/internal/issuer/signer"
	"os"

	certmanager "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(certmanager.AddToScheme(scheme))
	utilruntime.Must(piv1alpha1api.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var disableApprovedCheck bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&disableApprovedCheck, "disable-approved-check", false,
		"Disables waiting for CertificateRequests to have an approved condition before signing.")

	opts := zap.Options{
		Development: false,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "b858308c.ca.cert-manager.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	genericIssuerController := &controllers.GenericIssuerReconciler{
		Client:   mgr.GetClient(),
		Log:      ctrl.Log.WithName("controllers").WithName("GenericIssuer"),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("caissuer-controller"),
	}
	if err = (&controllers.SelfSignedIssuerReconciler{
		Client:            mgr.GetClient(),
		Log:               ctrl.Log.WithName("controllers").WithName("SelfSignedIssuer"),
		Scheme:            mgr.GetScheme(),
		GenericController: genericIssuerController,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "SelfSignedIssuer")
		os.Exit(1)
	}
	if err = (&controllers.ClusterSelfSignedIssuerReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		GenericController: genericIssuerController,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterSelfSignedIssuer")
		os.Exit(1)
	}
	if err = (&controllers.CertificateRequestReconciler{
		Client:                 mgr.GetClient(),
		Log:                    ctrl.Log.WithName("controllers").WithName("CertificateRequest"),
		Scheme:                 mgr.GetScheme(),
		Recorder:               mgr.GetEventRecorderFor("caissuer-controller"),
		Builder:                signer.NewCASinger,
		Clock:                  clock.RealClock{},
		CheckApprovedCondition: !disableApprovedCheck,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CertificateRequest")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("health", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("check", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
