package webserver

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

type certInfo struct {
	certificate *tls.Certificate
	keyTime     time.Time
	certTime    time.Time
}

var certificate atomic.Value

func generateCertificate(dataDir string) (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	now := time.Now()

	template := x509.Certificate{
		SerialNumber:          serialNumber,
		NotBefore:             now,
		NotAfter:              now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	bytes, err := x509.CreateCertificate(
		rand.Reader, &template, &template, &priv.PublicKey, priv,
	)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{
		Certificate: [][]byte{bytes},
		PrivateKey:  priv,
	}, nil
}

func fileTime(filename string) time.Time {
	fi, err := os.Stat(filename)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("%v: %v", filename, err)
		}
		return time.Time{}
	}
	return fi.ModTime()
}

func getCertificate(dataDir string) (*tls.Certificate, error) {
	info, ok := certificate.Load().(*certInfo)

	certFile := filepath.Join(dataDir, "cert.pem")
	keyFile := filepath.Join(dataDir, "key.pem")
	certTime := fileTime(certFile)
	keyTime := fileTime(keyFile)

	if !ok || !info.certTime.Equal(certTime) || !info.keyTime.Equal(keyTime) {
		var cert tls.Certificate
		nocert := certTime.Equal(time.Time{})
		nokey := keyTime.Equal(time.Time{})
		if nocert != nokey {
			return nil, errors.New("only one of cert.pem and key.pem exists")
		} else if nokey {
			log.Printf("Generating self-signed certificate")
			var err error
			cert, err = generateCertificate(dataDir)
			if err != nil {
				return nil, err
			}
		} else {
			var err error
			cert, err = tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				return nil, err
			}
		}
		info = &certInfo{
			certificate: &cert,
			certTime:    certTime,
			keyTime:     keyTime,
		}
		certificate.Store(info)
	}
	return info.certificate, nil
}
