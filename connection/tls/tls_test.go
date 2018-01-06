package tls

import (
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"

	"github.com/kelda/kelda/connection/tls/rsa"

	"github.com/stretchr/testify/assert"
)

const (
	ca = `
-----BEGIN CERTIFICATE-----
MIICzTCCAbWgAwIBAgIRAMPz3BAYstCTLbLQVVQiCOAwDQYJKoZIhvcNAQELBQAw
ADAeFw0xNjExMDEyMTAwMDRaFw0xNzExMDEyMTAwMDRaMAAwggEiMA0GCSqGSIb3
DQEBAQUAA4IBDwAwggEKAoIBAQDBxmhkJ0SVRwbPeGsQ7xsYA9X9yfPkqO3iPT7s
dXpFLkdFvcPZdF741lWiI2uNNfikbgBuSjoNfV4InwuyGYXZWzabHPq12zrnZ6RG
zpo/BFBQ3dDvBdw7tIYu74X79Ec+EUFgW0RS9FI9yBYbvuKNUc2Hgwg72Y+/+ZoY
lj34vpjk207fODWvmVyfX9yE6Y2TGxh0Y27+hQ9iWhulpp4QTBB2aNOWWASpcU96
xeRpUR+Yj+KWAoUL2UrKxgCpXG6pTd2ffMEa3mptBVUl5k9YKfJK+lGuxailulIT
ZwQDUBlGW99/PJp+hU7ry4TUkmtBRjwcxUkB3nZnUYb7IjUXAgMBAAGjQjBAMA4G
A1UdDwEB/wQEAwICpDAdBgNVHSUEFjAUBggrBgEFBQcDAgYIKwYBBQUHAwEwDwYD
VR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEATuoOc0Q+pNWxXoOki3Tq
DtPr3f8Mif+xjDbrbc1xP/Aw/ZbAXEcsqEYGo3s0+TqbZaxWNtuhV/1czCpFm9Li
ec+B67lS1Q9jzlxpuo56Y+a1Zi5IXyJfSUIQOkvFS4PP+hr4MCoLaqoC1aCh7faW
kYl192xJQpdSE/3W8j4QQLIsd44YOtRS2CT/xYjH27NuW+vYumzhBJXF+HjBbJOY
Eut4sppx9qRujo/6n8LGr8w+DvP8vQlONMbCf4eTSRe+t2uEW+X2it8kF94YATNW
aYRiI23Cev1RJGg41NxmaTNzFJTFYKKW8cOgoW/FZYlYgRYwkq64XPceQDS17ufC
Tw==
-----END CERTIFICATE-----`

	cert = `
-----BEGIN CERTIFICATE-----
MIICyTCCAbGgAwIBAgIQEhMJPzi2dcZUa18+VXRcSDANBgkqhkiG9w0BAQsFADAA
MB4XDTE2MTEwMTIxMDAwNFoXDTE3MTEwMTIxMDAwNFowADCCASIwDQYJKoZIhvcN
AQEBBQADggEPADCCAQoCggEBAMK3YN7JDM56x+9ULwvo8emiv9kTJh0aayZUIlUS
5ijObdPGswqsfetNvDkx8LO2xlgLgrnwxhsiYIbi4lW1CvsW51DT06yGFA4LrUSF
eV1fT/INM54W3395TvsvCtQVPmXM5hw+9cmLa/dNevqpU3oAQ4dZxWMamLobxuj4
8zy+iSxwIUjsSvABeqiyTy9UPGNXJYJjemXZZfYvYoIuKCqABMHejffdE/auV+XI
MWv16LXJIVLe8KPHhn3+rb209g2Q7YS0t4FyEI/UrLsQh3VhNEf73LQaCp3q1hUc
wckeCK++uEjb7imzDOzRIt7MemSniOZE1630fltuqMS1s7kCAwEAAaM/MD0wDgYD
VR0PAQH/BAQDAgKkMB0GA1UdJQQWMBQGCCsGAQUFBwMCBggrBgEFBQcDATAMBgNV
HRMBAf8EAjAAMA0GCSqGSIb3DQEBCwUAA4IBAQCI1QnBKgbrz1Vbfa+WCVh6Evpp
a3ED66wmSpW8T0ODD/seG2owx0vHJsZ7yDVxYus+mHU7GWW9czygsr0hmMk06X17
yUCdThQdhhItTTXsP/tUfOq7rjRvPDxZHENX0aa+amE2l4v0+0XG25y3mS2qSrp6
4pk1glvJH4iIBRT3GqQVmdQdWCeOAKrdm9ehvwP/7BFi4Xo9bCvYezUHwfntTVzY
5QV1CNQfwL5Dh1/lAnthJWsNZg8L3zlzRWJKm1MmESNckwFW0/J28QKhRfFXjq93
yxVaOTaN4z8znKo6wM6PCPir9OMNWJbaYuRkgq19uTDix4IpJwJnnPv+9VqS
-----END CERTIFICATE-----`

	key = `
-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEAwrdg3skMznrH71QvC+jx6aK/2RMmHRprJlQiVRLmKM5t08az
Cqx96028OTHws7bGWAuCufDGGyJghuLiVbUK+xbnUNPTrIYUDgutRIV5XV9P8g0z
nhbff3lO+y8K1BU+ZczmHD71yYtr9016+qlTegBDh1nFYxqYuhvG6PjzPL6JLHAh
SOxK8AF6qLJPL1Q8Y1clgmN6Zdll9i9igi4oKoAEwd6N990T9q5X5cgxa/Xotckh
Ut7wo8eGff6tvbT2DZDthLS3gXIQj9SsuxCHdWE0R/vctBoKnerWFRzByR4Ir764
SNvuKbMM7NEi3sx6ZKeI5kTXrfR+W26oxLWzuQIDAQABAoIBAQCTeRfhJByS9eMf
nH7VYmR2M1FiM2KWgD/PE8G89UdkeJQt5TwNRX9JC+MW3oATXMb0QCOOeJFSU8MP
5h7OEwRyD3K6gPS8of/mc2mTkBPPaDTAescxYNl9Tn9HNuXYow5TQ9C0a+rz7qii
8QfHeR9EM5bxmEgrOyWZLxiDsqlmwwU/2PKugH7dg5oe2qLN+skgwS6k2/Ct62Y3
st+7llU4ZhvHgszDm+zxrOGXIGaOx/JpHBmEXql+X2swVAZyGXqjDWLGdqrJYbFQ
+XqWfcoCvZlkcFr1DN+v0C4F1ggOCYorFhnIuZwGXeVoixuaJnX8hYWSCDl6iZCq
KvUrztRBAoGBAPA1ZcnwuZg+jlNhBB3eLRJWl+alGAPnnYWDIacXZcxiuj67L02B
GN8QDbup2f1rCKAJqmzw97tx0MVUjm/Vb28uQnnnPq+66w4njmCXshcbxR5TDWBS
f7fSFu7UGCAyJE59hs1zODztb6CbzheKAUz6YjVNAqDqyCRCAa2XYM/VAoGBAM+E
XdGKOwFDYAKwpJdPOeasiVzUXoRorMj6F91MwUx/+6NWokYlO3zPOSyf73Pw19KE
jxWW5rP7o9ApTE9z4F+MZMOZXmxJF+bkviPzYZJy8hJbSiDWfo0nzD11Bsmh8Unc
j3zsRhwkIwOlnRbQ1RRFrOZifzMDmzO6ghbZOOpVAoGBAKTZAHIF8li5FZPDEMAu
qV/cbYKr6j9DxKbLx1yUghgx6P8EFwJphlgO/F29wwxXWCP8fikldd39zfiefuHg
6Ai1BooCWNLgxE+CdgN0F5QkSrL07EkeVOgiFfrxM11lC+WR3+E/IWkuyVy/kEA3
RY0+iAdsQlGMzq2TXvNy383BAoGAXjagSZPSeh5WpqH/99o2VW4b5xNb3g2P9Kbm
0sgYMl0gp+WbQvGAcoe6U3JBSogb1C3usESUdT5X/xfg12mqgnbBALTO06bTvTY4
xSWoNM8O7BqaKxJ23islZPmOnVhyra//TR4QLpKRewRjr4ocU1nWx7oMOeL3QaL5
kNoKJwkCgYAeaqFH4VFGHe86nVWjKDWa+JVZL6q2X4Lv5iGwat/v9KNjiyIociE4
RqXBytHQLDZcPs+zbXClAbX33PvDONe1c9wQByXdWqrMwkkbcmWufcF6jP25HeIo
mtBaHDNS1vSAtNJh2xtZY85t0rMk3PWg9efe5bclsQF3Xa6hMyh8Bg==
-----END RSA PRIVATE KEY-----`
)

func TestNewError(t *testing.T) {
	t.Parallel()

	_, err := New(ca, cert, "key")
	assert.EqualError(t, err, "tls: failed to find any PEM data in key input")
}

func TestFromFileSuccess(t *testing.T) {
	t.Parallel()

	_, err := New(ca, cert, key)
	assert.NoError(t, err)
}

func TestVerifySignedByCA(t *testing.T) {
	t.Parallel()

	// Setup the fake client.
	validCA, err := rsa.NewCertificateAuthority()
	assert.NoError(t, err)

	validClient, err := rsa.NewSigned(validCA, pkix.Name{})
	assert.NoError(t, err)

	tlsCred, err := New(validCA.CertString(), validClient.CertString(),
		validClient.PrivateKeyString())
	assert.NoError(t, err)

	// Test that verification passes for servers with a certificate signed
	// by the same CA.
	validServer, err := rsa.NewSigned(validCA, pkix.Name{})
	assert.NoError(t, err)
	verifyErr := tryVerify(tlsCred, validServer.CertString())

	// Test that verification fails for servers with a different CA.
	otherCA, err := rsa.NewCertificateAuthority()
	assert.NoError(t, err)

	otherServer, err := rsa.NewSigned(otherCA, pkix.Name{})
	assert.NoError(t, err)

	verifyErr = tryVerify(tlsCred, otherServer.CertString())
	assert.Error(t, verifyErr)
	assert.Contains(t, verifyErr.Error(),
		"x509: certificate signed by unknown authority")
}

// tryVerify attempts to verify the given PEM-encoded certificate against
// the TLS credentials.
func tryVerify(tlsCred TLS, cert string) error {
	der, _ := pem.Decode([]byte(cert))
	return tlsCred.verifySignedByCA([][]byte{der.Bytes}, nil)
}
