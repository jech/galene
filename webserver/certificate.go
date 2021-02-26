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
	"sync"
	"sync/atomic"
	"time"
)

type certInfo struct {
	certificate *tls.Certificate
	keyTime     time.Time
	certTime    time.Time
}

// certMu protects writing to certificate
var certMu sync.Mutex

// certificate holds our current certificate, of type certInfo
var certificate atomic.Value

// generateCertificate generates a self-signed certficate
func generateCertificate() (tls.Certificate, error) {
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

func modTime(filename string) time.Time {
	fi, err := os.Stat(filename)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("%v: %v", filename, err)
		}
		return time.Time{}
	}
	return fi.ModTime()
}

// loadCertificate returns the current certificate if it is still valid.
func loadCertificate(certFile string, certTime time.Time, keyFile string, keyTime time.Time) *certInfo {
	info, ok := certificate.Load().(*certInfo)
	if !ok {
		return nil
	}

	if !info.certTime.Equal(certTime) || !info.keyTime.Equal(keyTime) {
		return nil
	}

	return info
}

// storeCertificate returns the current certificate if it is still valid,
// and either reads or generates a new one otherwise.
func storeCertificate(certFile string, certTime time.Time, keyFile string, keyTime time.Time) (info *certInfo, err error) {
	certMu.Lock()
	defer certMu.Unlock()

	// the certificate may have been updated since we checked
	info = loadCertificate(certFile, certTime, keyFile, keyTime)
	if info != nil {
		return
	}

	var cert tls.Certificate
	nocert := certTime.Equal(time.Time{})
	nokey := keyTime.Equal(time.Time{})

	if nocert != nokey {
		err = errors.New("only one of cert.pem and key.pem exists")
		return
	} else if nokey {
		log.Printf("Generating self-signed certificate")
		cert, err = generateCertificate()
		if err != nil {
			return
		}
	} else {
		cert, err = tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return
		}
	}
	info = &certInfo{
		certificate: &cert,
		certTime:    certTime,
		keyTime:     keyTime,
	}
	certificate.Store(info)
	return
}

func getCertificate(dataDir string) (*tls.Certificate, error) {
	certFile := filepath.Join(dataDir, "cert.pem")
	keyFile := filepath.Join(dataDir, "key.pem")
	certTime := modTime(certFile)
	keyTime := modTime(keyFile)

	info := loadCertificate(certFile, certTime, keyFile, keyTime)

	if info == nil {
		var err error
		info, err = storeCertificate(
			certFile, certTime, keyFile, keyTime,
		)
		if info == nil || err != nil {
			return nil, err
		}
	}
	return info.certificate, nil
}
