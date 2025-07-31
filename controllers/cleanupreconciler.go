package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/record"
	kubegresv1 "reactive-tech.io/kubegres/api/v1"
	kubegresCtx "reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/ctx/log"
	"reactive-tech.io/kubegres/controllers/ctx/status"
	"reactive-tech.io/kubegres/controllers/states"
	adapters "reactive-tech.io/kubegres/internal/sql"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type CleanupReplicationSlotsReconciler struct {
	Client              client.Client
	Logger              logr.Logger
	Recorder            record.EventRecorder
	stopCleanupFn       func()
	primarySvcRetriever func() (string, int32, error)
}

func NewCleanupReplicationSlotsReconciler(
	client client.Client,
	logger logr.Logger,
	recorder record.EventRecorder,
	primarySvcRetriever func() (string, int32, error),
) *CleanupReplicationSlotsReconciler {
	return &CleanupReplicationSlotsReconciler{
		Client:              client,
		Logger:              logger,
		Recorder:            recorder,
		stopCleanupFn:       nil,
		primarySvcRetriever: primarySvcRetriever,
	}
}

func (r *CleanupReplicationSlotsReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	var kubegres kubegresv1.Kubegres
	if err := r.Client.Get(ctx, request.NamespacedName, &kubegres); err != nil {
		if client.IgnoreNotFound(err) != nil {
			r.Logger.Error(err, "Failed to get Kubegres resource")
		}
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	if kubegres.Spec.ReplicationSlots.DisableCleanup {
		if r.stopCleanupFn == nil {
			return reconcile.Result{}, nil
		}
		r.Logger.Info("Replication slots cleanup is disabled, stopping cleanup", "kubegres", kubegres.Name)
		r.stopCleanupFn()
		r.stopCleanupFn = nil
		return reconcile.Result{}, nil
	}

	if r.stopCleanupFn != nil {
		return reconcile.Result{}, nil
	}
	r.Logger.Info("Starting replication slots cleanup", "kubegres", kubegres.Name)

	wrappedLogger := log.LogWrapper{Kubegres: &kubegres, Logger: r.Logger, Recorder: r.Recorder}

	kubegresContext := kubegresCtx.KubegresContext{
		Kubegres: &kubegres,
		Ctx:      ctx,
		Client:   r.Client,
		Log:      wrappedLogger,
		Status: &status.KubegresStatusWrapper{
			Kubegres: &kubegres,
			Ctx:      ctx,
			Client:   r.Client,
			Log:      wrappedLogger,
		},
	}
	resourcesStates, err := states.LoadResourcesStates(kubegresContext)
	if err != nil {
		err = fmt.Errorf("loading resources states :%w", err)
		return reconcile.Result{}, err
	}

	err = r.updateDbConnection(kubegresContext, resourcesStates)
	if err != nil {
		return reconcile.Result{}, err
	}

	//ctx, cancelFunc := context.WithCancel(context.Background())
	//replicationslot.StartCleanupLoop(ctx, r.Client)
	return reconcile.Result{}, nil
}

func (r *CleanupReplicationSlotsReconciler) updateDbConnection(kubegresContext kubegresCtx.KubegresContext, states states.ResourcesStates) error {

	_, err := adapters.NewDBFrom(kubegresContext, states, r.primarySvcRetriever)
	if err != nil {
		return fmt.Errorf("creating database connection: %w", err)
	}
	return nil
}

func (r *CleanupReplicationSlotsReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubegresv1.Kubegres{}).
		Complete(r)
}
