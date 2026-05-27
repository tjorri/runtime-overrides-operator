// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package main

import (
	"crypto/tls"
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	runtimeoverridesv1alpha1 "github.com/tjorri/runtime-overrides-operator/api/v1alpha1"
	"github.com/tjorri/runtime-overrides-operator/internal/config"
	"github.com/tjorri/runtime-overrides-operator/internal/controller"
	"github.com/tjorri/runtime-overrides-operator/internal/validate"

	"k8s.io/apimachinery/pkg/types"

	webhookv1alpha1 "github.com/tjorri/runtime-overrides-operator/internal/webhook/v1alpha1"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(runtimeoverridesv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	// Per-target config. One Loki + one Mimir per operator deployment.
	var lokiEnabled bool
	var lokiCMName, lokiCMNamespace string
	var mimirEnabled bool
	var mimirCMName, mimirCMNamespace string
	flag.BoolVar(&lokiEnabled, "loki-enabled", true,
		"Enable the Loki reconciler.")
	flag.StringVar(&lokiCMName, "loki-output-cm-name", "loki-runtime-tenants",
		"Name of the output ConfigMap the Loki reconciler owns.")
	flag.StringVar(&lokiCMNamespace, "loki-output-cm-namespace", "monitoring",
		"Namespace of the output ConfigMap the Loki reconciler owns.")
	flag.BoolVar(&mimirEnabled, "mimir-enabled", false,
		"Enable the Mimir reconciler.")
	var enableWebhook bool
	flag.BoolVar(&enableWebhook, "enable-webhook", false,
		"Enable the validating admission webhook. Requires --webhook-cert-path "+
			"to be set (or cert-manager wiring at deploy time). When disabled, "+
			"validation is enforced only by the controller.")
	flag.StringVar(&mimirCMName, "mimir-output-cm-name", "mimir-runtime-tenants",
		"Name of the output ConfigMap the Mimir reconciler owns.")
	flag.StringVar(&mimirCMNamespace, "mimir-output-cm-namespace", "monitoring",
		"Namespace of the output ConfigMap the Mimir reconciler owns.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	// Scope the cache's ConfigMap watch to only the configured target
	// namespaces — our RBAC narrows ConfigMap list/watch/create to those
	// namespaces, and the default cache would otherwise
	// try to list ConfigMaps cluster-wide and hit RBAC denials.
	cmCacheNamespaces := map[string]cache.Config{}
	if lokiEnabled {
		cmCacheNamespaces[lokiCMNamespace] = cache.Config{}
	}
	if mimirEnabled {
		cmCacheNamespaces[mimirCMNamespace] = cache.Config{}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "d9d7635e.runtimeoverrides.io",
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.ConfigMap{}: {Namespaces: cmCacheNamespaces},
			},
		},
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder
	// The kubebuilder-scaffolded webhook auto-registration blocks were
	// removed in favor of the explicit --enable-webhook-gated registration
	// further down. Having both registered both webhooks twice, which
	// panics with "can't register duplicate path".

	// Build the typed target configuration from flags and validate it.
	targets := config.Targets{
		Loki: config.TargetConfig{
			Enabled:  lokiEnabled,
			OutputCM: types.NamespacedName{Namespace: lokiCMNamespace, Name: lokiCMName},
		},
		Mimir: config.TargetConfig{
			Enabled:  mimirEnabled,
			OutputCM: types.NamespacedName{Namespace: mimirCMNamespace, Name: mimirCMName},
		},
	}
	if err := targets.Validate(); err != nil {
		setupLog.Error(err, "invalid target configuration")
		os.Exit(1)
	}

	// IMPORTANT: the upstream Loki/Mimir validator packages
	// store defaults in package-level globals consumed by Limits.UnmarshalYAML.
	// We must prime those globals exactly once at startup before any
	// validator runs concurrently. validate.InitDefaults is sync.Once-guarded.
	validate.InitDefaults()
	setupLog.Info("upstream Limits defaults initialized")

	hashCache := controller.NewHashCache()

	// Per-target reconciler registration. For each enabled target, register
	// the active reconciler + output-CM bootstrap; for each disabled target,
	// register a lightweight reconciler that surfaces TargetDisabled status
	// on any CR of that kind.
	bootstrap := &controller.BootstrapRunnable{
		Client:    mgr.GetClient(),
		HashCache: hashCache,
	}

	if targets.Loki.Enabled {
		lokiReconciler := &controller.LokiReconciler{
			Client:    mgr.GetClient(),
			OutputCM:  targets.Loki.OutputCM,
			Validator: validate.New(validate.TargetLoki),
			HashCache: hashCache,
		}
		if err := lokiReconciler.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to set up Loki reconciler")
			os.Exit(1)
		}
		bootstrap.Targets = append(bootstrap.Targets, controller.BootstrapTarget{
			Name: "loki", OutputCM: targets.Loki.OutputCM,
		})
		setupLog.Info("Loki reconciler registered",
			"output-configmap", targets.Loki.OutputCM.String())
	} else {
		disabled := &controller.LokiDisabledReconciler{Client: mgr.GetClient()}
		if err := disabled.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to set up Loki disabled-target reconciler")
			os.Exit(1)
		}
		setupLog.Info("Loki disabled — CRs will receive Applied=False, reason=TargetDisabled")
	}

	if targets.Mimir.Enabled {
		mimirReconciler := &controller.MimirReconciler{
			Client:    mgr.GetClient(),
			OutputCM:  targets.Mimir.OutputCM,
			Validator: validate.New(validate.TargetMimir),
			HashCache: hashCache,
		}
		if err := mimirReconciler.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to set up Mimir reconciler")
			os.Exit(1)
		}
		bootstrap.Targets = append(bootstrap.Targets, controller.BootstrapTarget{
			Name: "mimir", OutputCM: targets.Mimir.OutputCM,
		})
		setupLog.Info("Mimir reconciler registered",
			"output-configmap", targets.Mimir.OutputCM.String())
	} else {
		disabled := &controller.MimirDisabledReconciler{Client: mgr.GetClient()}
		if err := disabled.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to set up Mimir disabled-target reconciler")
			os.Exit(1)
		}
		setupLog.Info("Mimir disabled — CRs will receive Applied=False, reason=TargetDisabled")
	}

	// Register the bootstrap runnable so output CMs get a baseline empty
	// `overrides: {}` body at startup before any reconcile runs.
	if len(bootstrap.Targets) > 0 {
		if err := mgr.Add(bootstrap); err != nil {
			setupLog.Error(err, "unable to register bootstrap runnable")
			os.Exit(1)
		}
	}

	// Validating admission webhook. failurePolicy=Ignore is
	// set on the webhook configuration so outages don't block CR writes —
	// Layer 3 is the safety net. Per-kind registration is gated on the
	// corresponding target being enabled; we don't run a webhook for a
	// kind whose reconciler is in disabled-target mode.
	if enableWebhook {
		if targets.Loki.Enabled {
			if err := webhookv1alpha1.SetupLokiTenantOverrideWebhookWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to set up Loki validating webhook")
				os.Exit(1)
			}
			setupLog.Info("Loki validating webhook registered")
		}
		if targets.Mimir.Enabled {
			if err := webhookv1alpha1.SetupMimirTenantOverrideWebhookWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to set up Mimir validating webhook")
				os.Exit(1)
			}
			setupLog.Info("Mimir validating webhook registered")
		}
	} else {
		setupLog.Info("webhook disabled — relying on Layer-3 controller-side validation")
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// AGPL §13 belt-and-suspenders: emit a startup line identifying the
	// source repository, license, and verified-against upstream versions.
	// version is overridden via ldflags at release-build time;
	// "dev" is the default for `go run` / unreleased builds.
	writeBanner(os.Stdout, defaultVersion())

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
