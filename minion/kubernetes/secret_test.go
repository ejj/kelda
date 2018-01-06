package kubernetes

import (
	"errors"
	"testing"

	"github.com/kelda/kelda/minion/kubernetes/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSecretSet(t *testing.T) {
	t.Parallel()

	kubeClient := &mocks.SecretInterface{}
	secretClient := secretClientImpl{kubeClient}

	secretName := "secretName"
	secretVal := "secretVal"
	kubeSecretName, _ := secretRef(secretName)
	kubeSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: kubeSecretName,
		},
		Data: map[string][]byte{
			"value": []byte(secretVal),
		},
	}

	// Test creating a new secret.
	kubeClient.On("Get", kubeSecretName, mock.Anything).
		Return(nil, errors.New("does not exist")).Once()
	kubeClient.On("Create", &kubeSecret).Return(nil, nil).Once()
	err := secretClient.Set(secretName, secretVal)
	assert.NoError(t, err)
	kubeClient.AssertExpectations(t)

	// Test updating a secret that's already been created.
	secretVal = "changed"
	changedKubeSecret := copySecret(kubeSecret)
	changedKubeSecret.Data["value"] = []byte(secretVal)

	kubeClient.On("Get", kubeSecretName, mock.Anything).
		Return(&kubeSecret, nil).Once()
	kubeClient.On("Update", &changedKubeSecret).Return(nil, nil).Once()
	err = secretClient.Set(secretName, secretVal)
	assert.NoError(t, err)
	kubeClient.AssertExpectations(t)
}

func TestSecretGet(t *testing.T) {
	t.Parallel()

	kubeClient := &mocks.SecretInterface{}
	secretClient := secretClientImpl{kubeClient}

	secretName := "secretName"
	secretVal := "secretVal"
	kubeSecretName, _ := secretRef(secretName)

	// Test getting a secret that hasn't been created.
	kubeClient.On("Get", kubeSecretName, mock.Anything).
		Return(nil, errors.New("does not exist")).Once()
	_, err := secretClient.Get(secretName)
	assert.NotNil(t, err)
	kubeClient.AssertExpectations(t)

	// Test getting a secret that has been properly set.
	kubeSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: kubeSecretName,
		},
		Data: map[string][]byte{
			"value": []byte(secretVal),
		},
	}
	kubeClient.On("Get", kubeSecretName, mock.Anything).
		Return(&kubeSecret, nil).Once()
	actualSecret, err := secretClient.Get(secretName)
	assert.NoError(t, err)
	assert.Equal(t, secretVal, actualSecret)
	kubeClient.AssertExpectations(t)

	// Test getting a secret that was not inserted in the proper format.
	kubeSecret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: kubeSecretName,
		},
		Data: map[string][]byte{
			"foo": []byte("bar"),
		},
	}
	kubeClient.On("Get", kubeSecretName, mock.Anything).
		Return(&kubeSecret, nil).Once()
	_, err = secretClient.Get(secretName)
	assert.NotNil(t, err)
	kubeClient.AssertExpectations(t)
}

func TestSecretExists(t *testing.T) {
	t.Parallel()

	kubeClient := &mocks.SecretInterface{}
	secretClient := secretClientImpl{kubeClient}

	secretName := "secretName"
	secretVal := "secretVal"
	kubeSecretName, _ := secretRef(secretName)

	// Test when the secret doesn't exists.
	kubeClient.On("Get", kubeSecretName, mock.Anything).
		Return(nil, errors.New("does not exist")).Once()
	assert.False(t, secretClient.Exists(secretName))
	kubeClient.AssertExpectations(t)

	// Test when the secret exists.
	kubeSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: kubeSecretName,
		},
		Data: map[string][]byte{
			"value": []byte(secretVal),
		},
	}
	kubeClient.On("Get", kubeSecretName, mock.Anything).
		Return(&kubeSecret, nil).Once()
	assert.True(t, secretClient.Exists(secretName))
	kubeClient.AssertExpectations(t)
}

func copySecret(src corev1.Secret) (copy corev1.Secret) {
	dataCopy := map[string][]byte{}
	for k, v := range src.Data {
		dataCopy[k] = v
	}
	copy.ObjectMeta = src.ObjectMeta
	copy.Data = dataCopy
	return copy
}
