package webserver

import (
	"testing"
)

func TestGenerateCertificate(t *testing.T) {
	_, err := generateCertificate()
	if err != nil {
		t.Errorf("generateCertificate: %v", err)
	}
}

func BenchmarkGenerateCertificate(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := generateCertificate()
		if err != nil {
			b.Errorf("generateCertificate: %v", err)
		}
	}
}

func TestGetCertificate(t *testing.T) {
	cert1, err := getCertificate("/tmp/no/such/file")
	if err != nil {
		t.Errorf("getCertificate: %v", err)
	}

	cert2, err := getCertificate("/tmp/no/such/file")
	if err != nil {
		t.Errorf("getCertificate: %v", err)
	}

	if cert1 != cert2 {
		t.Errorf("cert1 != cert2")
	}
}

func BenchmarkGetCertificate(b *testing.B) {
	_, err := getCertificate("/tmp/no/such/file")
	if err != nil {
		b.Errorf("getCertificate: %v", err)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		_, err := getCertificate("/tmp/no/such/file")
		if err != nil {
			b.Errorf("getCertificate: %v", err)
		}
	}
}
