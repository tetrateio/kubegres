package tls

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

// rsaKeySize is the size of the RSA key to be generated for the self-signed certificates.
const rsaKeySize = 2048

// SelfSignedCerts holds the self-signed TLS certificates and keys in PEM format.
type SelfSignedCerts struct {
	RootCert   []byte
	RootKey    []byte
	ServerCert []byte
	ServerKey  []byte
	ClientCert []byte
	ClientKey  []byte
}

// NewSelfSignedCerts generates self-signed TLS certificates for the given hostnames for testing purposes.
// It returns the root CA certificate, server certificate, server key, client certificate, and client key.
func NewSelfSignedCerts(hostnames []string, ips []string) (SelfSignedCerts, error) {
	notBefore := time.Now()
	notAfter := notBefore.Add(12 * time.Hour) // Valid for 12 hours

	ipAddresses := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			return SelfSignedCerts{}, fmt.Errorf("invalid IP address: %s", ip)
		}
		ipAddresses = append(ipAddresses, parsedIP)
	}

	caTmpl := x509.Certificate{
		SerialNumber:          big.NewInt(2025),
		Subject:               pkix.Name{Organization: []string{"Kubegres"}, Country: []string{"US"}},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageAny, x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
		//DNSNames:              hostnames,
		//IPAddresses:           ipAddresses,
	}

	rootPriv, err := rsa.GenerateKey(rand.Reader, rsaKeySize)
	if err != nil {
		return SelfSignedCerts{}, fmt.Errorf("generate private RSA key: %w", err)
	}

	rootByes, err := x509.CreateCertificate(rand.Reader, &caTmpl, &caTmpl, rootPriv.Public(), rootPriv)
	if err != nil {
		return SelfSignedCerts{}, fmt.Errorf("create root certificate bytes: %w", err)
	}

	rootCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootByes})
	rootKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rootPriv)})

	rootCert, _ := x509.ParseCertificate(rootByes)

	serverTmpl := x509.Certificate{
		SerialNumber:          big.NewInt(2025),
		Subject:               pkix.Name{Organization: []string{"Kubegres"}, Country: []string{"US"}, CommonName: "kubegres-server"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              hostnames,
		IPAddresses:           ipAddresses,
	}

	serverPriv, err := rsa.GenerateKey(rand.Reader, rsaKeySize)
	if err != nil {
		return SelfSignedCerts{}, fmt.Errorf("generate server private RSA key: %w", err)
	}

	serverBytes, err := x509.CreateCertificate(rand.Reader, &serverTmpl, rootCert, serverPriv.Public(), rootPriv)
	if err != nil {
		return SelfSignedCerts{}, fmt.Errorf("create server certificate bytes: %w", err)
	}

	serverCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverBytes})
	serverKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverPriv)})

	clientTmpl := x509.Certificate{
		SerialNumber:          big.NewInt(2025),
		Subject:               pkix.Name{Organization: []string{"Kubegres"}, Country: []string{"US"}, CommonName: "kubegres-client"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IPAddresses:           ipAddresses,
	}
	for _, hostname := range hostnames {
		clientTmpl.DNSNames = append(clientTmpl.DNSNames, hostname)
	}

	clientPriv, err := rsa.GenerateKey(rand.Reader, rsaKeySize)
	if err != nil {
		return SelfSignedCerts{}, fmt.Errorf("generate client private RSA key: %w", err)
	}

	clientBytes, err := x509.CreateCertificate(rand.Reader, &clientTmpl, rootCert, clientPriv.Public(), rootPriv)
	if err != nil {
		return SelfSignedCerts{}, fmt.Errorf("create client certificate bytes: %w", err)
	}

	clientCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientBytes})
	clientKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(clientPriv)})

	return SelfSignedCerts{
		RootCert:   rootCertPEM,
		RootKey:    rootKeyPEM,
		ServerCert: serverCertPEM,
		ServerKey:  serverKeyPEM,
		ClientCert: clientCertPEM,
		ClientKey:  clientKeyPEM,
	}, nil
}
