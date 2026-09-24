package connection

import (
	"context"
	"os"
	"testing"

	"github.com/go-logr/logr"
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
	apiv1 "reactive-tech.io/kubegres/api/v1"
	kubegresCtx "reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/internal/sql"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TestKubegresReconcilerRetriesUntilPrimaryIsDeployed covers a fresh cluster: the reconciler first runs before
// the Primary exists and must retry, rather than leave the connection on its defaults until the spec changes.
func TestKubegresReconcilerRetriesUntilPrimaryIsDeployed(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, apiv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, storage.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, batchv1.AddToScheme(scheme))

	kubegres := &apiv1.Kubegres{}
	loadYAML(t, "../testdata/kubegres.yaml", kubegres)
	kubegres.Spec.Env = append(kubegres.Spec.Env, corev1.EnvVar{Name: "POSTGRES_USER", Value: "app"})
	kubegres.Spec.TLS = apiv1.TLS{
		SecretName:     "my-kubegres-certs",
		SSLMode:        "verify-ca",
		RootCertPath:   "/certs/ca.crt",
		ClientCertPath: "/certs/tls.crt",
		ClientKeyPath:  "/certs/tls.key",
	}

	// Loading the cluster state lists objects by owner, through the index the operator registers at startup.
	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(kubegres)
	for _, obj := range []client.Object{&appsv1.StatefulSet{}, &corev1.Service{}, &corev1.ConfigMap{}, &corev1.Pod{}} {
		builder = builder.WithIndex(obj, kubegresCtx.DeploymentOwnerKey, ownerKubegresName)
	}
	k8sClient := builder.Build()
	connStore := sql.NewConnectionStore()
	r := kubegresReconciler{
		DBConnectionReconciler: NewDBConnectionReconciler(k8sClient, logr.Discard(), connStore, record.NewFakeRecorder(10)),
		logger:                 logr.Discard(),
	}
	req := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "my-kubegres"}}

	result, err := r.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, connectionDetailsRetryInterval, result.RequeueAfter, "must retry while the Primary is not deployed")

	primarySS := &appsv1.StatefulSet{}
	loadYAML(t, "../testdata/primary-ss.yaml", primarySS)
	primarySvc := &corev1.Service{}
	loadYAML(t, "../testdata/primary-svc.yaml", primarySvc)
	createAll(t, k8sClient, primarySS, primarySvc)

	result, err = r.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.Zero(t, result.RequeueAfter, "must stop retrying once the Primary is deployed")

	dsnData, ok := r.dsnStore.Get(toConnectionID(req.NamespacedName))
	require.True(t, ok)
	dsn := dsnData.Snapshot()
	assert.Equal(t, primarySvc.Name, dsn.Host)
	assert.Equal(t, "app", dsn.Username)
	assert.Equal(t, "verify-ca", dsn.SSLMode)
	assert.Equal(t, "/certs/ca.crt", dsn.RootCertPath)
	assert.Equal(t, "/certs/tls.crt", dsn.ClientCertPath)
	assert.Equal(t, "/certs/tls.key", dsn.ClientKeyPath)
}

func ownerKubegresName(obj client.Object) []string {
	owner := metav1.GetControllerOf(obj)
	if owner == nil || owner.APIVersion != apiv1.GroupVersion.String() || owner.Kind != kubegresCtx.KindKubegres {
		return nil
	}
	return []string{owner.Name}
}

func loadYAML(t *testing.T, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, into))
}

func createAll(t *testing.T, c client.Client, objects ...client.Object) {
	t.Helper()
	for _, obj := range objects {
		obj.SetResourceVersion("")
		require.NoError(t, c.Create(context.Background(), obj))
	}
}
