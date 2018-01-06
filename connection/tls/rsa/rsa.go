package rsa

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"
)

// KeyPair represents an RSA private key and certificate. The private key is
// kept secret and signs outgoing traffic and decrypts incoming traffic. The
// certificate can be shared publicly and is used to prove the holder's
// identity to peers. The keys and certificates it generates are compatible with
// our TLS scheme.
type KeyPair struct {
	key  *rsa.PrivateKey
	cert *x509.Certificate
}

// PrivateKeyString returns the PEM-encoded string representing the private key. This
// string format can be written to disk, and later read using FromFile.
func (keyPair KeyPair) PrivateKeyString() string {
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(keyPair.key),
	}))
}

// CertString returns the PEM-encoded string representing the certificate. This
// string format can be written to disk, and later read using FromFile.
func (keyPair KeyPair) CertString() string {
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: keyPair.cert.Raw,
	}))
}

// New loads the KeyPair defined by the given PEM-encoded cert and key.
func New(certStr, keyStr string) (KeyPair, error) {
	keyDER, err := getDER(keyStr)
	if err != nil {
		return KeyPair{}, fmt.Errorf("read key: %s", err)
	}

	key, err := x509.ParsePKCS1PrivateKey(keyDER)
	if err != nil {
		return KeyPair{}, fmt.Errorf("parse key: %s", err)
	}

	certDER, err := getDER(certStr)
	if err != nil {
		return KeyPair{}, fmt.Errorf("read cert: %s", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return KeyPair{}, fmt.Errorf("parse cert: %s", err)
	}

	return KeyPair{key, cert}, nil
}

// getDER decodes the DER (Distinguished Encoding Rules)-encoded bytes from a
// PEM-encoded string. In other words, it decodes the bytes encoded
// by the KeyString and CertString methods.
func getDER(pemStr string) ([]byte, error) {
	der, _ := pem.Decode([]byte(pemStr))
	if der == nil {
		return nil, errors.New("no key PEM data found")
	}

	return der.Bytes, nil
}

// NewCertificateAuthority generates a KeyPair that can be used as a
// certificate authority. Specifically, it has a self-signed certificate that
// is allowed to sign other certificates.
func NewCertificateAuthority() (KeyPair, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return KeyPair{}, fmt.Errorf("create key: %s", err)
	}

	template, err := certTemplate()
	if err != nil {
		return KeyPair{}, fmt.Errorf("create template: %s", err)
	}
	template.KeyUsage = x509.KeyUsageCertSign
	template.IsCA = true

	certBytes, err := x509.CreateCertificate(rand.Reader, &template,
		&template, key.Public(), crypto.PrivateKey(key))
	if err != nil {
		return KeyPair{}, fmt.Errorf("create cert: %s", err)
	}

	cert, err := x509.ParseCertificate(certBytes)
	return KeyPair{key, cert}, err
}

// NewSigned generates a KeyPair signed by `signer`.
func NewSigned(signer KeyPair, subject pkix.Name, ips ...net.IP) (KeyPair, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return KeyPair{}, fmt.Errorf("create key: %s", err)
	}

	template, err := certTemplate()
	if err != nil {
		return KeyPair{}, fmt.Errorf("create template: %s", err)
	}
	template.ExtKeyUsage = []x509.ExtKeyUsage{
		x509.ExtKeyUsageClientAuth,
		x509.ExtKeyUsageServerAuth,
	}
	template.IPAddresses = ips
	template.Subject = subject

	certBytes, err := x509.CreateCertificate(rand.Reader, &template,
		signer.cert, key.Public(), signer.key)
	if err != nil {
		return KeyPair{}, fmt.Errorf("create cert: %s", err)
	}

	cert, err := x509.ParseCertificate(certBytes)
	return KeyPair{key, cert}, err
}

func certTemplate() (x509.Certificate, error) {
	// Pick a random serial number.
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return x509.Certificate{}, err
	}

	now := time.Now()
	template := x509.Certificate{
		SerialNumber:          serialNumber,
		BasicConstraintsValid: true,
		NotBefore:             now,
		// Expire after one year.
		NotAfter: now.Add(1 * 365 * 24 * time.Hour),
	}

	return template, nil
}
