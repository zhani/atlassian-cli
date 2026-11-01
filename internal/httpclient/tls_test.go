package httpclient

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func get(t *testing.T, rt http.RoundTripper, url string) error {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	resp, err := (&http.Client{Transport: rt}).Do(req)
	if err == nil {
		resp.Body.Close()
	}
	return err
}

func TestCAFile(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	dir := t.TempDir()
	der := srv.Certificate().Raw
	pemFile := filepath.Join(dir, "ca.pem")
	os.WriteFile(pemFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	derFile := filepath.Join(dir, "ca.crt")
	os.WriteFile(derFile, der, 0o600)

	if err := get(t, newTransport(""), srv.URL); err == nil {
		t.Fatal("the test server's certificate should not be trusted without a CA file")
	}
	for _, f := range []string{pemFile, derFile} {
		if err := get(t, newTransport(f), srv.URL); err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
		}
	}
}

func TestCAFileErrors(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.pem")
	os.WriteFile(bad, []byte("not a certificate"), 0o600)
	for file, want := range map[string]string{
		bad:              "no PEM or DER certificate found",
		bad + ".missing": "no such file",
	} {
		err := get(t, newTransport(file), "https://example.invalid/")
		if err == nil || !strings.Contains(err.Error(), CAFileEnv) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %s … %s", file, err, CAFileEnv, want)
		}
	}
}
