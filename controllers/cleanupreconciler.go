package controllers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	kubegresv1 "reactive-tech.io/kubegres/api/v1"
	kubegresCtx "reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/ctx/log"
	"reactive-tech.io/kubegres/controllers/ctx/status"
	"reactive-tech.io/kubegres/controllers/states"
	replicationSlotRepo "reactive-tech.io/kubegres/internal/replicationslot/repo"
	sqladapters "reactive-tech.io/kubegres/internal/sql"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var reconciliationRetryInterval = 5 * time.Second

var (
	CleanupDisabledReason             = "CleanupDisabled"
	PrimaryNotReadyReason             = "PrimaryNotReady"
	ReplicationSlotDeleteFailedReason = "ReplicationSlotDeleteFailed"
	ReplicationSlotDeletedReason      = "ReplicationSlotDeleted"
)

type cleanupRoutine struct {
	cancelFunc context.CancelFunc
	settings   *settings
}

type cleanupRoutinesHandler struct {
	clock                  clock
	eventRecorder          record.EventRecorder
	dbProvider             *sqladapters.ConnectionStore
	runningCleanupRoutines map[ctrlclient.ObjectKey]cleanupRoutine
	repoFactory            repoFactory
}

func (h cleanupRoutinesHandler) stopCleanup(object ctrlclient.ObjectKey) {
	aRoutine, found := h.runningCleanupRoutines[object]
	if !found {
		return
	}
	if aRoutine.cancelFunc != nil {
		aRoutine.cancelFunc()
	}
	delete(h.runningCleanupRoutines, object)
}

func (h cleanupRoutinesHandler) HasActiveRoutine(
	objKey ctrlclient.ObjectKey,
	settings *settings,
) bool {
	aRoutine, found := h.runningCleanupRoutines[objKey]
	if !found {
		return false
	}
	if !aRoutine.settings.equal(settings) {
		return false
	}
	return aRoutine.cancelFunc != nil
}

type settings struct {
	kubegres                *kubegresv1.Kubegres
	healthCheckInterval     time.Duration
	inactiveSlotGracePeriod *time.Duration
}

func (s *settings) equal(other *settings) bool {
	if s.healthCheckInterval != other.healthCheckInterval {
		return false
	}
	if s.inactiveSlotGracePeriod == nil && other.inactiveSlotGracePeriod == nil {
		return true
	}
	if s.inactiveSlotGracePeriod == nil || other.inactiveSlotGracePeriod == nil {
		return false
	}
	return *s.inactiveSlotGracePeriod == *other.inactiveSlotGracePeriod
}

func (h cleanupRoutinesHandler) startCleanup(objKey ctrlclient.ObjectKey, settings *settings, funcToRun cleanupFunc) {
	aRoutine, found := h.runningCleanupRoutines[objKey]

	if found {
		if aRoutine.settings.equal(settings) {
			return
		}
		aRoutine.cancelFunc() // settings changed
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	go funcToRun(ctx, h.eventRecorder, h.clock, h.dbProvider, settings, h.repoFactory)
	newRoutine := cleanupRoutine{
		cancelFunc: cancelFunc,
		settings:   settings,
	}
	h.runningCleanupRoutines[objKey] = newRoutine
}

func NewCleanupReplicationSlotsReconciler(
	client ctrlclient.Client,
	logger logr.Logger,
	recorder record.EventRecorder,
	connectionStore *sqladapters.ConnectionStore,
) *CleanupReplicationSlotsReconciler {
	return &CleanupReplicationSlotsReconciler{
		Client:          client,
		Logger:          logger,
		Recorder:        recorder,
		ConnectionStore: connectionStore,
		cleanupRoutinesHandler: cleanupRoutinesHandler{
			clock:                  &realClock{},
			eventRecorder:          recorder,
			dbProvider:             connectionStore,
			runningCleanupRoutines: make(map[ctrlclient.ObjectKey]cleanupRoutine),
			repoFactory:            replicationSlotRepo.New,
		},
	}
}

type CleanupReplicationSlotsReconciler struct {
	Client                 ctrlclient.Client
	Logger                 logr.Logger
	Recorder               record.EventRecorder
	ConnectionStore        *sqladapters.ConnectionStore
	cleanupRoutinesHandler cleanupRoutinesHandler
}

func (r *CleanupReplicationSlotsReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	var kubegres kubegresv1.Kubegres
	if err := r.Client.Get(ctx, request.NamespacedName, &kubegres); err != nil {
		if ctrlclient.IgnoreNotFound(err) != nil {
			r.Logger.Error(err, "Failed to get Kubegres resource")
		}
		return reconcile.Result{}, ctrlclient.IgnoreNotFound(err)
	}

	if kubegres.Spec.ReplicationSlots.DisableCleanup || kubegres.Spec.ReplicationSlots.Enabled == false {
		r.cleanupRoutinesHandler.stopCleanup(ctrlclient.ObjectKeyFromObject(&kubegres))
		r.Recorder.Event(&kubegres, v1.EventTypeNormal, CleanupDisabledReason, "replication slots cleanup is disabled")
		return reconcile.Result{}, nil
	}

	wrappedLogger := log.LogWrapper{Kubegres: &kubegres, Logger: r.Logger, Recorder: r.Recorder}

	kubegresContext := kubegresCtx.KubegresContext{
		Kubegres:        &kubegres,
		Ctx:             ctx,
		Client:          r.Client,
		Log:             wrappedLogger,
		ConnectionStore: r.ConnectionStore,
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
	if !resourcesStates.StatefulSets.Primary.IsDeployed || !resourcesStates.StatefulSets.Primary.IsReady ||
		!resourcesStates.Services.Primary.IsDeployed {
		return reconcile.Result{Requeue: true, RequeueAfter: reconciliationRetryInterval}, nil
	}

	if r.cleanupRoutinesHandler.HasActiveRoutine(ctrlclient.ObjectKeyFromObject(&kubegres), r.getSettings(kubegres)) {
		return reconcile.Result{}, nil
	}

	r.Logger.Info("Starting replication slots cleanup", "kubegres", kubegres.Name)
	r.cleanupRoutinesHandler.startCleanup(
		ctrlclient.ObjectKeyFromObject(&kubegres),
		r.getSettings(kubegres),
		startReplicationSlotsCleanup,
	)

	return reconcile.Result{}, nil
}

func (r *CleanupReplicationSlotsReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubegresv1.Kubegres{}).
		Complete(r)
}

func (r *CleanupReplicationSlotsReconciler) getSettings(kubegres kubegresv1.Kubegres) *settings {
	s := &settings{
		kubegres:            &kubegres,
		healthCheckInterval: kubegres.Spec.ReplicationSlots.HealthCheckInterval.Duration,
	}
	period := kubegres.Spec.ReplicationSlots.InactiveSlotGracePeriod
	if period == nil {
		return s
	}
	s.inactiveSlotGracePeriod = &period.Duration
	return s
}

var _ clock = &realClock{}

type realClock struct{}

func (r realClock) Tick(d time.Duration) <-chan time.Time {
	return time.Tick(d)
}

func (r realClock) Now() time.Time {
	return time.Now()
}

func (r realClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

type clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
	Tick(d time.Duration) <-chan time.Time
}

type repoFactory func(querier replicationSlotRepo.Querier) replicationSlotRepo.Repository

type cleanupFunc func(context.Context, record.EventRecorder, clock, *sqladapters.ConnectionStore, *settings, repoFactory)

var _ cleanupFunc = startReplicationSlotsCleanup

func startReplicationSlotsCleanup(
	ctx context.Context,
	eventRecorder record.EventRecorder,
	clk clock,
	dbProvider *sqladapters.ConnectionStore,
	settings *settings,
	factory repoFactory,
) {
	kubegres := settings.kubegres
	if settings.healthCheckInterval <= 0 {
		settings.healthCheckInterval = time.Second
	}
	healthCheckTicker := clk.Tick(settings.healthCheckInterval)
	gracePeriodEnabled := true
	if settings.inactiveSlotGracePeriod == nil || *settings.inactiveSlotGracePeriod == 0 {
		gracePeriodEnabled = false
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-healthCheckTicker:
			db, found := dbProvider.Get(sqladapters.ConnectionID{
				Name:      kubegres.GetName(),
				Namespace: kubegres.GetNamespace(),
			})
			if !found {
				eventRecorder.Event(kubegres, v1.EventTypeWarning, PrimaryNotReadyReason, "database connection is not ready")
				continue
			}
			repo := factory(db.DB())
			replicationSlots, err := repo.ListAll(ctx)
			if err != nil {
				eventRecorder.Event(kubegres, v1.EventTypeWarning, "ReplicationSlotsListError", fmt.Sprintf("failed to list replication slots: %v", err))
				continue
			}
			for _, slot := range replicationSlots {
				if slot.Active {
					continue
				}
				if gracePeriodEnabled {
					go func() {
						select {
						case <-ctx.Done():
							return
						case <-clk.After(*settings.inactiveSlotGracePeriod):
							err = deleteIfInactive(ctx, slot.Name, repo)
							if err != nil {
								eventRecorder.Event(kubegres, v1.EventTypeWarning, ReplicationSlotDeletedReason,
									fmt.Sprintf("failed to delete replication slot %s: %v", slot.Name, err))
								return
							}
							eventRecorder.Event(kubegres, v1.EventTypeNormal, ReplicationSlotDeletedReason,
								fmt.Sprintf("successfully deleted replication slot %s", slot.Name))
						}
					}()
					continue
				}

				err := deleteIfInactive(ctx, slot.Name, repo)
				if err != nil {
					eventRecorder.Event(kubegres, v1.EventTypeWarning, ReplicationSlotDeleteFailedReason,
						fmt.Sprintf("failed to delete replication slot %s: %v", slot.Name, err.Error()))
					continue
				}
				eventRecorder.Event(kubegres, v1.EventTypeNormal, ReplicationSlotDeletedReason,
					fmt.Sprintf("successfully deleted replication slot %v", slot.Name))
			}
		}
	}
}

func deleteIfInactive(ctx context.Context, slot string, repo replicationSlotRepo.Repository) error {
	slotToCheck, err := repo.GetSlot(ctx, slot)
	if err != nil {
		if errors.Is(err, replicationSlotRepo.ErrNotFound) {
			return nil // Slot does not exist, nothing to delete
		}
		return err
	}
	if slotToCheck.Active {
		return nil
	}
	return repo.DeleteSlot(ctx, slotToCheck.Name)
}
