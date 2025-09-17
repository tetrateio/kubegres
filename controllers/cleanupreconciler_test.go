package controllers

import (
	"context"
	_ "embed"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/record"
	kubegresv1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/internal/replicationslot"
	replicationSlotRepo "reactive-tech.io/kubegres/internal/replicationslot/repo"
	sqladapters "reactive-tech.io/kubegres/internal/sql"
	"reactive-tech.io/kubegres/test/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestCleanupRoutinesHandler(t *testing.T) {
	h := cleanupRoutinesHandler{
		runningCleanupRoutines: make(map[client.ObjectKey]cleanupRoutine),
	}

	object := client.ObjectKeyFromObject(kubegres(t))
	mockFunc := &mockFuncRun{}
	h.startCleanup(object, &settings{}, mockFunc.Run)

	assert.Eventually(t, func() bool { return mockFunc.wasCalled }, time.Second, time.Millisecond)

	assert.True(t, h.HasActiveRoutine(object, &settings{}))

	h.stopCleanup(object)
	assert.False(t, h.HasActiveRoutine(object, &settings{}))
	// TODO(piotrkpc): what happens if background function is down
}

func TestCleanupReplicationSlotsReconciler_Reconcile(t *testing.T) {

	scheme := runtime.NewScheme()
	require.NoError(t, kubegresv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, storage.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, batchv1.AddToScheme(scheme))

	tests := []struct {
		name                  string
		clientSetupFn         func(*testing.T) client.Client
		setupMockRepo         func(m *mockRepo)
		setupCleanupRoutines  func(t *testing.T, m map[client.ObjectKey]cleanupRoutine) *mockFuncRun // returns the mockFuncRun to allow further assertions in assertCleanupFunc
		req                   reconcile.Request
		assertEventRecorder   func(*testing.T, *record.FakeRecorder)
		assertClient          func(*testing.T, client.Client)
		assertReconcileOutput func(*testing.T, reconcile.Result, error)
		assertCleanupFunc     func(*testing.T, map[client.ObjectKey]cleanupRoutine, *mockFuncRun)
		assertRepo            func(*testing.T, *mockRepo, *mockClock)
		setupDbConnStore      func(t *testing.T, cs *sqladapters.ConnectionStore)
	}{
		{
			name: "no kubegres object",
			clientSetupFn: func(*testing.T) client.Client {
				return fake.NewClientBuilder().WithScheme(scheme).Build()
			},
			req: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "does-not-exist",
					Name:      "does-not-exist",
				},
			},
			assertReconcileOutput: func(t *testing.T, result reconcile.Result, err error) {
				require.NoError(t, err)
				assert.Equal(t, reconcile.Result{}, result)
			},
		},
		{
			name: "replication slots are not enabled - stopFunc is called",
			clientSetupFn: func(t *testing.T) client.Client {
				k := kubegres(t)
				k.Spec.ReplicationSlots = kubegresv1.ReplicationSlots{
					Enabled: false,
				}
				return fake.NewClientBuilder().WithScheme(scheme).WithObjects(k).Build()
			},
			setupCleanupRoutines: func(t *testing.T, m map[client.ObjectKey]cleanupRoutine) *mockFuncRun {
				mockFunc := &mockFuncRun{}
				m[types.NamespacedName{Name: "my-kubegres", Namespace: "default"}] = cleanupRoutine{
					cancelFunc: mockFunc.CancelContext,
				}
				return mockFunc
			},
			req: reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "my-kubegres", Namespace: "default"},
			},
			assertReconcileOutput: func(t *testing.T, result reconcile.Result, err error) {
				require.NoError(t, err)
				assert.Equal(t, reconcile.Result{}, result)
			},
			assertCleanupFunc: func(t *testing.T, m map[client.ObjectKey]cleanupRoutine, mockFunc *mockFuncRun) {
				require.True(t, mockFunc.cancelContextRun, "cancel context should be called")
				assert.NotContains(t, m, types.NamespacedName{Name: "my-kubegres", Namespace: "default"}, "cleanup routine should be removed")
			},
			assertEventRecorder: func(t *testing.T, recorder *record.FakeRecorder) {
				select {
				case event := <-recorder.Events:
					assert.Contains(t, event, corev1.EventTypeNormal)
					assert.Contains(t, event, CleanupDisabledReason)
				default:
					require.FailNow(t, "expected event to be reported but was not")
				}
			},
		},
		{
			name: "replication slots enabled - primary not deployed",
			clientSetupFn: func(*testing.T) client.Client {
				k := kubegres(t)
				k.Spec.ReplicationSlots = kubegresv1.ReplicationSlots{
					Enabled: true,
				}
				return fake.NewClientBuilder().WithScheme(scheme).WithObjects(
					k,
				).Build()
			},
			req: reconcile.Request{NamespacedName: types.NamespacedName{Name: "my-kubegres", Namespace: "default"}},
			assertReconcileOutput: func(t *testing.T, result reconcile.Result, err error) {
				require.NoError(t, err)
				assert.Equal(t, reconcile.Result{
					Requeue: true, RequeueAfter: 5 * time.Second,
				}, result)
			},
		},
		{
			name: "replication slots enabled - primary not ready",
			clientSetupFn: func(*testing.T) client.Client {
				k := kubegres(t)
				k.Spec.ReplicationSlots = kubegresv1.ReplicationSlots{
					Enabled: true,
				}
				primarySS := primaryStatefulSet(t)
				primarySS.Status = appsv1.StatefulSetStatus{}
				return fake.NewClientBuilder().WithScheme(scheme).WithObjects(
					k, primarySS,
				).Build()
			},
			req: reconcile.Request{NamespacedName: types.NamespacedName{Name: "my-kubegres", Namespace: "default"}},
			assertReconcileOutput: func(t *testing.T, result reconcile.Result, err error) {
				require.NoError(t, err)
				assert.Equal(t, reconcile.Result{
					Requeue: true, RequeueAfter: 5 * time.Second,
				}, result)
			},
		},
		{
			name: "replication slots enabled - primary svc not deployed",
			clientSetupFn: func(*testing.T) client.Client {
				k := kubegres(t)
				k.Spec.ReplicationSlots = kubegresv1.ReplicationSlots{
					Enabled: true,
				}
				primarySS := primaryStatefulSet(t)

				return fake.NewClientBuilder().WithScheme(scheme).WithObjects(
					k, primarySS).Build()
			},
			req: reconcile.Request{NamespacedName: types.NamespacedName{Name: "my-kubegres", Namespace: "default"}},
			assertReconcileOutput: func(t *testing.T, result reconcile.Result, err error) {
				require.NoError(t, err)
				assert.Equal(t, reconcile.Result{
					Requeue: true, RequeueAfter: 5 * time.Second,
				}, result)
			},
		},
		{
			name: "replication slots enabled - resources are ready, db is not ready",
			clientSetupFn: func(*testing.T) client.Client {
				k := kubegres(t)
				k.Spec.ReplicationSlots = kubegresv1.ReplicationSlots{
					Enabled: true,
				}
				return fake.NewClientBuilder().WithScheme(scheme).WithObjects(
					k, primaryStatefulSet(t), primarySvc(t)).Build()
			},
			setupDbConnStore: func(t *testing.T, cs *sqladapters.ConnectionStore) {
				cs.Set(sqladapters.ConnectionID{Name: "my-kubegres", Namespace: "default"}, &sqladapters.Connection{})
			},
			setupMockRepo: func(m *mockRepo) {
				m.slots["inactive"] = replicationslot.ReplicationSlot{
					Name: "inactive",
				}
			},
			req: reconcile.Request{NamespacedName: types.NamespacedName{Name: "my-kubegres", Namespace: "default"}},
			assertReconcileOutput: func(t *testing.T, result reconcile.Result, err error) {
				require.NoError(t, err)
				assert.Equal(t, reconcile.Result{}, result)
			},
			assertCleanupFunc: func(t *testing.T, registeredRoutines map[client.ObjectKey]cleanupRoutine, _ *mockFuncRun) {
				assert.Contains(t, registeredRoutines, types.NamespacedName{Name: "my-kubegres", Namespace: "default"})
			},
			assertRepo: func(t *testing.T, m *mockRepo, clk *mockClock) {
				clk.waitForTicker(time.Second, 3*time.Second)
				clk.tickHealthCheck(time.Second, time.Now())
				assertReplicationSlotsNbre(t, m, 0)
			},
		},
		{
			name: "settings changed - cancel and start a new routine",
			clientSetupFn: func(*testing.T) client.Client {
				k := kubegres(t)
				k.Spec.ReplicationSlots = kubegresv1.ReplicationSlots{
					Enabled:                 true,
					InactiveSlotGracePeriod: &metav1.Duration{Duration: time.Second}, // this is new
				}
				return fake.NewClientBuilder().WithScheme(scheme).WithObjects(
					k, primaryStatefulSet(t), primarySvc(t)).Build()
			},
			setupDbConnStore: func(t *testing.T, cs *sqladapters.ConnectionStore) {
				cs.Set(sqladapters.ConnectionID{Name: "my-kubegres", Namespace: "default"}, &sqladapters.Connection{})
			},
			setupCleanupRoutines: func(t *testing.T, m map[client.ObjectKey]cleanupRoutine) *mockFuncRun {
				mockFunc := &mockFuncRun{}
				m[types.NamespacedName{Name: "my-kubegres", Namespace: "default"}] = cleanupRoutine{
					cancelFunc: mockFunc.CancelContext,
					settings: &settings{
						inactiveSlotGracePeriod: nil, // this setting is old - disabled, not set
					},
				}
				return mockFunc
			},
			req: reconcile.Request{NamespacedName: types.NamespacedName{Name: "my-kubegres", Namespace: "default"}},
			assertReconcileOutput: func(t *testing.T, result reconcile.Result, err error) {
				require.NoError(t, err)
				assert.Equal(t, reconcile.Result{}, result)
			},
			assertCleanupFunc: func(t *testing.T, registeredRoutines map[client.ObjectKey]cleanupRoutine, cleanupFunc *mockFuncRun) {
				assert.True(t, cleanupFunc.cancelContextRun, "cleanup cancel should be called")
				assert.Contains(t, registeredRoutines, types.NamespacedName{Name: "my-kubegres", Namespace: "default"})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mocklogger := util.CreateMockLogger()
			fakeRecorder := record.NewFakeRecorder(10)

			dbConnStore := sqladapters.NewConnectionStore()
			if tt.setupDbConnStore != nil {
				tt.setupDbConnStore(t, dbConnStore)
			}

			c := tt.clientSetupFn(t)
			runningCleanupRoutines := make(map[client.ObjectKey]cleanupRoutine)
			var mockCleanupFunc *mockFuncRun
			if tt.setupCleanupRoutines != nil {
				mockCleanupFunc = tt.setupCleanupRoutines(t, runningCleanupRoutines)
			}

			clk := newMockClock(t)

			repo := mockRepo{
				slots: make(map[string]replicationslot.ReplicationSlot),
			}

			if tt.setupMockRepo != nil {
				tt.setupMockRepo(&repo)
			}

			reconciler := CleanupReplicationSlotsReconciler{
				Client:          c,
				Logger:          mocklogger,
				Recorder:        fakeRecorder,
				ConnectionStore: dbConnStore,
				//cleanupFunction: startReplicationSlotsCleanup,
				cleanupRoutinesHandler: cleanupRoutinesHandler{
					clock:                  clk,
					runningCleanupRoutines: runningCleanupRoutines,
					eventRecorder:          fakeRecorder,
					dbProvider:             dbConnStore,
					repoFactory: func(replicationSlotRepo.Querier) replicationSlotRepo.Repository {
						return &repo
					},
				},
			}
			result, err := reconciler.Reconcile(t.Context(), tt.req)
			tt.assertReconcileOutput(t, result, err)

			if tt.assertCleanupFunc != nil {
				tt.assertCleanupFunc(t, runningCleanupRoutines, mockCleanupFunc)
			}

			if tt.assertEventRecorder != nil {
				tt.assertEventRecorder(t, fakeRecorder)
			}
			if tt.assertRepo != nil {
				tt.assertRepo(t, &repo, clk)
			}
		})
	}
}

func TestStartReplicationSlotsCleanupRoutine(t *testing.T) {

	const timeout = 3 * time.Second

	clk := newMockClock(t)
	eventRecorder := record.NewFakeRecorder(10)

	connectionStore := sqladapters.NewConnectionStore()

	repo := &mockRepo{
		slots: map[string]replicationslot.ReplicationSlot{
			"inactive": {
				Name: "inactive",
			},
			"active": {
				Name:   "active",
				Active: true,
			},
		},
	}

	var rf repoFactory = func(querier replicationSlotRepo.Querier) replicationSlotRepo.Repository {
		return repo
	}

	s := &settings{
		kubegres: kubegres(t),
	}

	go startReplicationSlotsCleanup(t.Context(), eventRecorder, realClock{}, connectionStore, s, rf)

	gracePeriod := 5 * time.Second
	healthCheckInterval := 1 * time.Second
	s = &settings{
		kubegres:                kubegres(t),
		healthCheckInterval:     healthCheckInterval,
		inactiveSlotGracePeriod: &gracePeriod,
	}

	go startReplicationSlotsCleanup(t.Context(), eventRecorder, clk, connectionStore, s, rf)

	clk.waitForTicker(healthCheckInterval, timeout)
	clk.tickHealthCheck(healthCheckInterval, time.Now())

	// Database connection is not ready
	assertEventRecorded(t, eventRecorder, PrimaryNotReadyReason)

	// Simulate that the database connection is now ready
	connectionStore.Set(sqladapters.ConnectionID{
		Name:      s.kubegres.GetName(),
		Namespace: s.kubegres.GetNamespace(),
	}, &sqladapters.Connection{})

	// 1st tick - one inactive slot but grace period is set, so it should not be deleted yet
	clk.tickHealthCheck(healthCheckInterval, time.Now())

	assertReplicationSlotsNbre(t, repo, 2)
	slots, err := repo.ListAll(t.Context())
	require.NoError(t, err)
	require.Contains(t, slots, replicationslot.ReplicationSlot{Name: "inactive"}, "inactive slot should be here because of grace period")

	repo.slots["inactive"] = replicationslot.ReplicationSlot{
		Name:   "inactive",
		Active: true, // Simulate that the slot is now active
	}

	clk.waitForGracePeriodTimer(timeout)
	clk.endGracePeriodTimerWith(time.Now())

	assertReplicationSlotsNbre(t, repo, 2)
	slots, err = repo.ListAll(t.Context())
	require.NoError(t, err)
	require.Contains(t, slots, replicationslot.ReplicationSlot{Name: "inactive", Active: true})

	// Simulate that the slot is now inactive again
	repo.slots["inactive"] = replicationslot.ReplicationSlot{
		Name:   "inactive",
		Active: false,
	}

	// 2nd tick - inactive slot should be deleted after grace period
	clk.tickHealthCheck(healthCheckInterval, time.Now())
	clk.waitForGracePeriodTimer(timeout)
	clk.endGracePeriodTimerWith(time.Now())

	assertReplicationSlotsNbre(t, repo, 1)
	slots, err = repo.ListAll(t.Context())
	require.NoError(t, err)
	require.Contains(t, slots, replicationslot.ReplicationSlot{Name: "active", Active: true}, "active slot should remain after cleanup")
}

//go:embed testdata/primary-ss.yaml
var primaryStatefulSetYaml string

//go:embed testdata/primary-svc.yaml
var primarySvcYaml string

//go:embed testdata/kubegres.yaml
var kubegresYaml string

func primaryStatefulSet(t *testing.T) *appsv1.StatefulSet {
	ss := &appsv1.StatefulSet{}
	require.NoError(t, yaml.Unmarshal([]byte(primaryStatefulSetYaml), ss))
	return ss
}

func primarySvc(t *testing.T) *corev1.Service {
	svc := &corev1.Service{}
	require.NoError(t, yaml.Unmarshal([]byte(primarySvcYaml), svc))
	return svc
}

func kubegres(t *testing.T) *kubegresv1.Kubegres {
	k := &kubegresv1.Kubegres{}
	require.NoError(t, yaml.Unmarshal([]byte(kubegresYaml), k))
	return k
}

type mockFuncRun struct {
	wasCalled        bool
	cancelContextRun bool
}

func (f *mockFuncRun) Run(_ context.Context, _ record.EventRecorder, c clock, _ *sqladapters.ConnectionStore, _ *settings, _ repoFactory) {
	f.wasCalled = true
}

func (f *mockFuncRun) CancelContext() {
	f.cancelContextRun = true
}

type mockClock struct {
	t                      *testing.T
	now                    time.Time
	gracePeriodTimerCh     chan time.Time
	tickers                map[time.Duration]chan time.Time
	notifyTickerRegistered chan time.Duration
	tickerRegistered       map[time.Duration]bool
	notifyTimerRegistered  chan struct{}
}

func (m *mockClock) Tick(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time)
	m.tickers[d] = ch
	go func() {
		m.notifyTickerRegistered <- d
	}()
	return ch
}

func (m *mockClock) Now() time.Time {
	return m.now
}

func (m *mockClock) After(_ time.Duration) <-chan time.Time {
	m.gracePeriodTimerCh = make(chan time.Time, 1)
	m.notifyTimerRegistered <- struct{}{}
	return m.gracePeriodTimerCh
}

func (m *mockClock) waitForGracePeriodTimer(timeout time.Duration) {
	select {
	case <-m.notifyTimerRegistered:
		return
	case <-time.After(timeout):
		require.FailNow(m.t, "timed out waiting for grace period timer")
	}
}

func (m *mockClock) tickHealthCheck(durationTicker time.Duration, t time.Time) {
	times, found := m.tickers[durationTicker]
	if !found {
		require.FailNow(m.t, "no ticker found for duration: ", durationTicker.String(), "available tickers: ", slices.Collect(maps.Keys(m.tickers)))
		return
	}
	times <- t
}

func (m *mockClock) endGracePeriodTimerWith(t time.Time) {
	m.gracePeriodTimerCh <- t
	close(m.gracePeriodTimerCh)
}

func (m *mockClock) waitForTicker(interval time.Duration, timeout time.Duration) {
	if _, found := m.tickerRegistered[interval]; found {
		return
	}
	for {
		select {
		case i := <-m.notifyTickerRegistered:
			if i == interval {
				m.tickerRegistered[i] = true
				return
			}
			m.tickerRegistered[i] = true

		case <-time.After(timeout):
			require.FailNow(m.t, "timed out waiting for ticker registration for interval: ")
		}
	}
}

type mockRepo struct {
	slots map[string]replicationslot.ReplicationSlot
	mu    sync.RWMutex
}

func (m *mockRepo) CreateSlot(ctx context.Context, name string) (replicationslot.ReplicationSlot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.slots[name]; exists {
		return replicationslot.ReplicationSlot{}, replicationSlotRepo.ErrAlreadyExist
	}
	slot := replicationslot.ReplicationSlot{
		Name: name,
	}
	m.slots[name] = slot
	return slot, nil
}

func (m *mockRepo) GetSlot(ctx context.Context, name string) (replicationslot.ReplicationSlot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if slot, exists := m.slots[name]; exists {
		return slot, nil
	}
	return replicationslot.ReplicationSlot{}, replicationSlotRepo.ErrNotFound
}

func (m *mockRepo) DeleteSlot(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.slots[name]; !exists {
		return replicationSlotRepo.ErrNotFound
	}
	delete(m.slots, name)
	return nil
}

func (m *mockRepo) ListAll(ctx context.Context) ([]replicationslot.ReplicationSlot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	values := maps.Values(m.slots)
	slots := slices.Collect(values)
	return slots, nil
}

func newMockClock(t *testing.T) *mockClock {
	return &mockClock{
		t:                      t,
		tickers:                make(map[time.Duration]chan time.Time),
		gracePeriodTimerCh:     make(chan time.Time),
		now:                    time.Now(),
		notifyTickerRegistered: make(chan time.Duration),
		notifyTimerRegistered:  make(chan struct{}),
		tickerRegistered:       make(map[time.Duration]bool),
	}
}

func assertReplicationSlotsNbre(t *testing.T, repo *mockRepo, wantLength int) {
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		allReplicationSlots, err := repo.ListAll(t.Context())
		require.NoError(c, err)
		require.Len(c, allReplicationSlots, wantLength, "only one slot should remain after cleanup")
	}, 3*time.Second, 10*time.Millisecond)
}

func assertEventRecorded(t *testing.T, eventRecorder *record.FakeRecorder, reason string) {
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		select {
		case event := <-eventRecorder.Events:
			require.Contains(c, event, reason)
		default:
			require.FailNow(c, "expected event to be recorded but was not")
		}
	}, 3*time.Second, 10*time.Millisecond)
}
