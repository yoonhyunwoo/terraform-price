package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeRegistry serves the registry endpoints for ns/name/provider at the
// given version; the module tarball contains exactly main.tf.
func fakeRegistry(t *testing.T, ns, name, provider, version, mainTf string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/v1/modules/%s/%s/%s/versions", ns, name, provider), func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"modules":[{"versions":[{"version":"1.0.0"},{"version":"%s"}]}]}`, version)
	})
	mux.HandleFunc(fmt.Sprintf("/v1/modules/%s/%s/%s/%s/download", ns, name, provider, version), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Terraform-Get", "http://"+r.Host+"/tarball/mod.tar.gz")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/tarball/mod.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		main := []byte(mainTf)
		tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("%s-%s-%s-%s/main.tf", ns, name, provider, version), Mode: 0o644, Size: int64(len(main))})
		tw.Write(main)
		tw.Close()
		gz.Close()
		w.Write(buf.Bytes())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}
