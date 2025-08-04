package states

import (
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"reactive-tech.io/kubegres/controllers/ctx"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type TLSSecretStates struct {
	IsSecretDeployed bool
	SecretKeys       map[string]bool

	kubegresContext ctx.KubegresContext
}

func loadTLSSecretStates(kubegresContext ctx.KubegresContext) (TLSSecretStates, error) {
	tlsSecretStates := TLSSecretStates{kubegresContext: kubegresContext}
	err := tlsSecretStates.loadStates()
	return tlsSecretStates, err
}

func (r *TLSSecretStates) loadStates() (err error) {
	secret, err := r.getDeployedSecret()
	if err != nil {
		return err
	}

	r.IsSecretDeployed = secret != nil

	r.SecretKeys = r.getKeysFromSecret(secret)

	return nil
}

func (r *TLSSecretStates) getDeployedSecret() (*v1.Secret, error) {
	resourceKey := client.ObjectKey{
		Namespace: r.kubegresContext.Kubegres.Namespace,
		Name:      r.kubegresContext.Kubegres.Spec.TLS.SecretName}
	secret := &v1.Secret{}

	err := r.kubegresContext.Client.Get(r.kubegresContext.Ctx, resourceKey, secret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		r.kubegresContext.Log.ErrorEvent("TLSSecretLoadError", err, "Failed to load TLS secret.", "Secret name", resourceKey.Name)
		return nil, err
	}
	return secret, nil
}

func (r *TLSSecretStates) getKeysFromSecret(secret *v1.Secret) map[string]bool {
	if secret == nil {
		return make(map[string]bool)
	}

	keys := make(map[string]bool, len(secret.Data))
	for key, value := range secret.Data {
		if value != nil && len(value) > 0 {
			keys[key] = true
		}
	}
	return keys
}
