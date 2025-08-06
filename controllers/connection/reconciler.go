package connection

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	apiv1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/internal/sql"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	_ reconcile.Reconciler = (*kubegresReconciler)(nil)
	_ reconcile.Reconciler = (*secretReconciler)(nil)
)

const (
	appliesToTLS appliesTo = iota
	appliesToPassword
	appliesToUser
	appliesToHostAddr
	appliesToHost
	appliesToDatabase
	appliesToPort
)

type (
	// DBConnectionReconciler is responsible for reconciling the database connection state
	// based on the Kubegres resource and the associated secrets.
	// It manages the connections in the given connection store and updates the DSN data accordingly.
	DBConnectionReconciler struct {
		client client.Client
		logger logr.Logger

		connStore *sql.ConnectionStore
		dsnStore  *SyncMap[sql.ConnectionID, *sql.DSNData]
		secrets   *SyncMap[types.NamespacedName, secretReference]
	}

	kubegresReconciler struct {
		*DBConnectionReconciler
		logger logr.Logger
	}

	secretReconciler struct {
		*DBConnectionReconciler
		logger logr.Logger
	}

	secretReference struct {
		connID    sql.ConnectionID
		appliesTo appliesTo
		key       string
	}
	appliesTo int
)

// NewDBConnectionReconciler is a constructor.
func NewDBConnectionReconciler(c client.Client, logger logr.Logger, connStore *sql.ConnectionStore) *DBConnectionReconciler {
	return &DBConnectionReconciler{
		client:    c,
		logger:    logger.WithName("DBConnectionReconciler"),
		connStore: connStore,
		dsnStore:  NewSyncMap[sql.ConnectionID, *sql.DSNData](),
		secrets:   NewSyncMap[types.NamespacedName, secretReference](),
	}
}

func (r *DBConnectionReconciler) SetupWithManager(mgr manager.Manager) error {
	return errors.Join(
		ctrl.NewControllerManagedBy(mgr).
			For(&apiv1.Kubegres{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
			Complete(kubegresReconciler{
				DBConnectionReconciler: r,
				logger:                 r.logger.WithName("kubegres"),
			}),

		ctrl.NewControllerManagedBy(mgr).
			For(&corev1.Secret{}, builder.WithPredicates(TLSSecretNamePredicate(r.secrets), predicate.GenerationChangedPredicate{})).
			Complete(secretReconciler{
				DBConnectionReconciler: r,
				logger:                 r.logger.WithName("secret"),
			}),
	)
}

func (r kubegresReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	namespacedName := request.NamespacedName
	kubegres := &apiv1.Kubegres{}
	err := r.get(ctx, request.NamespacedName, kubegres)
	if k8serrors.IsNotFound(err) {
		return reconcile.Result{}, nil
	}

	if err != nil {
		r.logger.Error(err, "Error retrieving Kubegres resource.", "name", namespacedName)
		return reconcile.Result{}, err
	}

	connID := toConnectionID(namespacedName)

	dsnData, ok := r.dsnStore.Get(connID)
	if !ok {
		dsnData = sql.NewDSNData()
		r.dsnStore.Set(connID, dsnData)
		r.logger.Info("New DSN data created.", "connectionID", connID)
	}

	dbConn, ok := r.connStore.Get(connID)
	if !ok {
		dbConn, err = sql.NewDynamicDSNConnection(dsnData)
		if err != nil {
			r.logger.Error(err, "Failed to create new DB connection.", "connectionID", connID)
			return reconcile.Result{Requeue: true, RequeueAfter: time.Second * 15}, err
		}

		r.connStore.Set(connID, dbConn)
		r.logger.Info("New DB connection created.", "connectionID", connID)
	}

	secretRef := updateDSNData(dsnData, kubegres)
	for k, v := range secretRef {
		// first register the secret reference so the secret reconciler can find it
		r.secrets.Set(k, v)

		secret := &corev1.Secret{}
		err := r.get(ctx, k, secret)
		if k8serrors.IsNotFound(err) {
			r.logger.Info("Secret not found, skipping update", "connectionID", connID, "secret", k)
			continue
		}
		if updateDSNDataFromSecret(dsnData, secret, v) {
			r.logger.Info("Updated DSN data from secret", "connectionID", connID, "secret", k, "dsnData", dsnData)
		}
	}
	r.logger.Info("Updated DB connection.", "connectionID", connID, "dsn", dsnData)

	return reconcile.Result{}, nil
}

func (r secretReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	secretNamespacedName := request.NamespacedName
	secretRef, ok := r.secrets.Get(secretNamespacedName)
	if !ok {
		r.logger.Info("Secret not registered, skipping reconciliation", "secret", secretNamespacedName)
		return reconcile.Result{}, nil
	}

	secret := &corev1.Secret{}
	err := r.get(ctx, request.NamespacedName, secret)
	if k8serrors.IsNotFound(err) {
		return reconcile.Result{}, nil
	}

	if err != nil {
		r.logger.Error(err, "Error retrieving Secret.", "name", secretNamespacedName)
		return reconcile.Result{}, err
	}

	connID := secretRef.connID
	dsnData, ok := r.dsnStore.Get(connID)
	if !ok {
		r.logger.Info("No existing DSN data found, skipping secret reconciliation", "connectionID", connID)
		return reconcile.Result{}, nil
	}

	if updateDSNDataFromSecret(dsnData, secret, secretRef) {
		r.logger.Info("Updated DSN data from secret", "connectionID", connID, "dsnData", dsnData)
	}

	return reconcile.Result{}, nil
}

func updateDSNDataFromSecret(dsnData *sql.DSNData, secret *corev1.Secret, secretRef secretReference) bool {
	readKey := func(key string) string {
		value, ok := secret.StringData[key]
		if !ok {
			value = string(secret.Data[key])
		}
		return value
	}

	switch secretRef.appliesTo {
	case appliesToTLS:
	// TODO (sergicastro): save files
	case appliesToHost:
		dsnData.Host = readKey(secretRef.key)
	case appliesToHostAddr:
		dsnData.HostAddr = readKey(secretRef.key)
	case appliesToPort:
		dsnData.Port = readKey(secretRef.key)
	case appliesToDatabase:
		dsnData.Database = readKey(secretRef.key)
	case appliesToUser:
		dsnData.Username = readKey(secretRef.key)
	case appliesToPassword:
		dsnData.Password = readKey(secretRef.key)
	default:
		return false
	}

	return true
}

var (
	// This block contains the environment variable names that are used to configure the PostgreSQL connection.
	// These are going to be looked up in the environment variables of the Kubegres resource spec.
	// https://www.postgresql.org/docs/current/libpq-envars.html

	portEnvVars        = []string{"DB_PORT", "PGPORT", "POSTGRES_PORT"}
	hostnameEnvVars    = []string{"DB_HOST", "PGHOST", "POSTGRES_HOST"}
	hostAddressEnvVars = []string{"PGHOSTADDR"}
	databaseEnvVars    = []string{"DB_NAME", "PGDATABASE", "POSTGRES_DATABASE"}
	usernameEnvVars    = []string{"DB_USER", "PGUSER", "POSTGRES_USER"}
	passwordEnvVars    = []string{"DB_PASSWORD", "PGPASSWORD", "POSTGRES_PASSWORD"}
)

func updateDSNData(dsnData *sql.DSNData, kubegres *apiv1.Kubegres) map[types.NamespacedName]secretReference {
	connID := toConnectionID(types.NamespacedName{
		Namespace: kubegres.Namespace,
		Name:      kubegres.Name,
	})
	secretRef := make(map[types.NamespacedName]secretReference)

	if hostAddrEV, ok := findEnvVar(kubegres.Spec.Env, hostAddressEnvVars...); ok {
		if hostAddrEV.Value != "" {
			dsnData.HostAddr = hostAddrEV.Value
		} else if k, v, ok := secretRefFromEnvVar(hostAddrEV, connID, kubegres, appliesToHostAddr); ok {
			secretRef[k] = v
		}
	}

	if testHost := os.Getenv("TEST_PRIMARY_HOSTNAME"); testHost != "" {
		dsnData.Host = testHost
	} else if hostEV, ok := findEnvVar(kubegres.Spec.Env, hostnameEnvVars...); ok {
		if hostEV.Value != "" {
			dsnData.Host = hostEV.Value
		} else if k, v, ok := secretRefFromEnvVar(hostEV, connID, kubegres, appliesToHost); ok {
			secretRef[k] = v
		}
	}

	if testPort := os.Getenv("TEST_PRIMARY_PORT"); testPort != "" {
		dsnData.Port = testPort
	} else if portEV, ok := findEnvVar(kubegres.Spec.Env, portEnvVars...); ok {
		if portEV.Value != "" {
			dsnData.Port = portEV.Value
		} else if k, v, ok := secretRefFromEnvVar(portEV, connID, kubegres, appliesToPort); ok {
			secretRef[k] = v
		}
	}

	if dbNameEV, ok := findEnvVar(kubegres.Spec.Env, databaseEnvVars...); ok {
		if dbNameEV.Value != "" {
			dsnData.Database = dbNameEV.Value
		} else if k, v, ok := secretRefFromEnvVar(dbNameEV, connID, kubegres, appliesToDatabase); ok {
			secretRef[k] = v
		}
	}

	if usernameEV, ok := findEnvVar(kubegres.Spec.Env, usernameEnvVars...); ok {
		if usernameEV.Value != "" {
			dsnData.Username = usernameEV.Value
		} else if k, v, ok := secretRefFromEnvVar(usernameEV, connID, kubegres, appliesToUser); ok {
			secretRef[k] = v
		}
	}

	if passwordEV, ok := findEnvVar(kubegres.Spec.Env, passwordEnvVars...); ok {
		if passwordEV.Value != "" {
			dsnData.Password = passwordEV.Value
		} else if k, v, ok := secretRefFromEnvVar(passwordEV, connID, kubegres, appliesToPassword); ok {
			secretRef[k] = v
		}
	}

	if tls := kubegres.Spec.TLS; tls.SecretName != "" {

		if tls.SecretName != "" {
			secretRef[types.NamespacedName{
				Namespace: kubegres.Namespace,
				Name:      tls.SecretName,
			}] = secretReference{
				connID:    connID,
				appliesTo: appliesToTLS,
				key:       "",
			}
		}

		if tls.SSLMode != "" {
			dsnData.SSLMode = tls.SSLMode
		}

		dsnData.RootCertPath = tls.RootCertPath
		dsnData.ClientCertPath = tls.ClientCertPath
		dsnData.ClientKeyPath = tls.ClientKeyPath
	}

	return secretRef
}

func secretRefFromEnvVar(ev corev1.EnvVar, connID sql.ConnectionID, kubegres *apiv1.Kubegres, appliesTo appliesTo) (types.NamespacedName, secretReference, bool) {
	if ev.ValueFrom.SecretKeyRef == nil {
		return types.NamespacedName{}, secretReference{}, false
	}

	return types.NamespacedName{
			Namespace: kubegres.Namespace,
			Name:      ev.ValueFrom.SecretKeyRef.Name,
		}, secretReference{
			connID:    connID,
			appliesTo: appliesTo,
			key:       ev.ValueFrom.SecretKeyRef.Key,
		},
		true
}

func findEnvVar(env []corev1.EnvVar, wantFirstOf ...string) (corev1.EnvVar, bool) {
	for _, e := range env {
		for _, want := range wantFirstOf {
			if e.Name == want {
				return e, true
			}
		}
	}
	return corev1.EnvVar{}, false
}

func (r *DBConnectionReconciler) get(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	if err := r.client.Get(ctx, key, obj); err != nil {
		return err
	}
	return nil
}

func TLSSecretNamePredicate(secretMap *SyncMap[types.NamespacedName, secretReference]) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(object client.Object) bool {
		if secretMap == nil {
			return false
		}

		secret, ok := object.(*corev1.Secret)
		if !ok {
			return false
		}

		secretNames, _ := secretMap.KeysAndValues()
		for _, s := range secretNames {
			if s.Name == secret.Name && s.Namespace == secret.Namespace {
				return true
			}
		}

		return false
	})
}

func toConnectionID(namespacedName types.NamespacedName) sql.ConnectionID {
	return sql.ConnectionID{
		Namespace: namespacedName.Namespace,
		Name:      namespacedName.Name,
	}
}
