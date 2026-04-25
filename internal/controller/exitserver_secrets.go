package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Credentials holds the random secrets the operator generates for one
// ExitServer. They are NOT user-supplied; the controller writes them on
// first reconcile and reads them on subsequent reconciles via the Secret
// it created.
type Credentials struct {
	AdminPassword string
	AuthToken     string
}

// credentialsSecretName returns "<exit-name>-credentials".
func credentialsSecretName(exit *frpv1alpha1.ExitServer) string {
	return exit.Name + "-credentials"
}

// ensureCredentialsSecret returns the Credentials for the given ExitServer,
// creating the backing Secret with fresh random values if it doesn't yet
// exist. The Secret is owned by the ExitServer (via OwnerReferences) so
// it's garbage-collected when the ExitServer is deleted.
//
// On subsequent calls the existing Secret is read and its values returned
// unchanged — never rotated implicitly.
func ensureCredentialsSecret(ctx context.Context, c client.Client, exit *frpv1alpha1.ExitServer) (Credentials, error) {
	name := credentialsSecretName(exit)
	key := types.NamespacedName{Name: name, Namespace: exit.Namespace}

	var sec corev1.Secret
	err := c.Get(ctx, key, &sec)
	if err == nil {
		return Credentials{
			AdminPassword: string(sec.Data["admin-password"]),
			AuthToken:     string(sec.Data["auth-token"]),
		}, nil
	}
	if !apierrors.IsNotFound(err) {
		return Credentials{}, fmt.Errorf("get secret %s: %w", key, err)
	}

	creds := Credentials{
		AdminPassword: randomHex(32),
		AuthToken:     randomHex(32),
	}
	ownerRef := metav1.OwnerReference{
		APIVersion:         frpv1alpha1.GroupVersion.String(),
		Kind:               "ExitServer",
		Name:               exit.Name,
		UID:                exit.UID,
		BlockOwnerDeletion: ptr.To(true),
		Controller:         ptr.To(true),
	}
	sec = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: exit.Namespace,
			Labels: map[string]string{
				"frp-operator.io/exit": exit.Name,
			},
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"admin-password": []byte(creds.AdminPassword),
			"auth-token":     []byte(creds.AuthToken),
		},
	}
	if err := c.Create(ctx, &sec); err != nil {
		// Race: another reconcile created it concurrently. Re-read.
		if apierrors.IsAlreadyExists(err) {
			if err := c.Get(ctx, key, &sec); err == nil {
				return Credentials{
					AdminPassword: string(sec.Data["admin-password"]),
					AuthToken:     string(sec.Data["auth-token"]),
				}, nil
			}
		}
		return Credentials{}, fmt.Errorf("create secret %s: %w", key, err)
	}
	return creds, nil
}

// randomHex returns 2*n hex characters of crypto-random data.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("controller: rand: %v", err))
	}
	return hex.EncodeToString(b)
}
