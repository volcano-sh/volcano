/*
Copyright 2025 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResponseHelpers(t *testing.T) {
	t.Run("responseOk", func(t *testing.T) {
		rec := httptest.NewRecorder()
		var w http.ResponseWriter = rec

		responseOk(&w, "ok-msg")
		res := rec.Result()
		defer res.Body.Close()

		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("ReadAll error: %v", err)
		}
		if res.StatusCode != http.StatusOK {
			t.Errorf("Status = %d; want %d", res.StatusCode, http.StatusOK)
		}
		if got := string(body); got != "ok-msg" {
			t.Errorf("Body = %q; want %q", got, "ok-msg")
		}
		if ct := res.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
			t.Errorf("Content-Type = %q; want %q", ct, "text/plain; charset=utf-8")
		}
	})

	t.Run("responseError", func(t *testing.T) {
		rec := httptest.NewRecorder()
		var w http.ResponseWriter = rec

		responseError(&w, "bad", http.StatusTeapot)
		if rec.Code != http.StatusTeapot {
			t.Errorf("Status = %d; want %d", rec.Code, http.StatusTeapot)
		}
		if rec.Body.Len() == 0 {
			t.Error("expected non-empty error body")
		}
	})
}

func TestModifyLoglevel(t *testing.T) {
	// save and restore
	mutex.Lock()
	origLevel := currentLogLevel
	origCancel := prevCtxCancelFunc
	mutex.Unlock()

	t.Cleanup(func() {
		mutex.Lock()
		defer mutex.Unlock()
		if prevCtxCancelFunc != nil {
			prevCtxCancelFunc()
		}
		currentLogLevel = origLevel
		prevCtxCancelFunc = origCancel
	})

	t.Run("valid", func(t *testing.T) {
		if err := modifyLoglevel("4"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		mutex.RLock()
		defer mutex.RUnlock()
		if currentLogLevel != "4" {
			t.Errorf("currentLogLevel = %q; want \"4\"", currentLogLevel)
		}
	})

	t.Run("invalid non-numeric", func(t *testing.T) {
		if err := modifyLoglevel("foo"); err == nil {
			t.Error("expected error for non-numeric level")
		}
	})
}

func TestListenUnix(t *testing.T) {
	t.Run("custom dir", func(t *testing.T) {
		tmp := t.TempDir()
		l, err := listenUnix("cmp", tmp)
		if err != nil {
			if strings.Contains(err.Error(), "operation not supported") {
				t.Skip("unix sockets not supported on this platform")
			}
			t.Fatalf("listenUnix error: %v", err)
		}
		defer l.Close()

		path := filepath.Join(tmp, "cmp"+SocketSuffix)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("socket %q not created", path)
		}
	})

	t.Run("default dir", func(t *testing.T) {
		defer os.RemoveAll(DefaultSocketDir)
		l, err := listenUnix("cmp2", "")
		if err != nil {
			if strings.Contains(err.Error(), "operation not supported") {
				t.Skip("unix sockets not supported on this platform")
			}
			t.Fatalf("listenUnix(default) error: %v", err)
		}
		defer l.Close()

		path := filepath.Join(DefaultSocketDir, "cmp2"+SocketSuffix)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("default socket %q not created", path)
		}
	})

	t.Run("cleanup existing socket", func(t *testing.T) {
		dir := t.TempDir()
		l1, err := listenUnix("clean", dir)
		if err != nil {
			if strings.Contains(err.Error(), "operation not supported") {
				t.Skip("unix sockets not supported on this platform")
			}
			t.Fatalf("first listenUnix error: %v", err)
		}
		l1.Close()

		// second call should remove stale socket and succeed
		l2, err := listenUnix("clean", dir)
		if err != nil {
			t.Fatalf("second listenUnix error: %v", err)
		}
		defer l2.Close()
	})
}

func TestKlogLogLevelHandlers(t *testing.T) {
	t.Cleanup(func() {
		mutex.Lock()
		defer mutex.Unlock()
		if prevCtxCancelFunc != nil {
			prevCtxCancelFunc()
		}
		startupLogLevel = ""
		currentLogLevel = ""
	})

	mux := http.NewServeMux()
	installKlogLogLevelHandler(mux, "5")

	t.Run("GET /getlevel", func(t *testing.T) {
		req := httptest.NewRequest("GET", getLogLevelPath, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d; want %d", rec.Code, http.StatusOK)
		}
		exp := "Current klog log level: 5\n"
		if rec.Body.String() != exp {
			t.Errorf("body = %q; want %q", rec.Body.String(), exp)
		}
	})

	t.Run("GET /setlevel valid", func(t *testing.T) {
		req := httptest.NewRequest("GET", setLogLevelPath+"?level=6&duration=1s", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d; want %d", rec.Code, http.StatusOK)
		}
		mutex.RLock()
		if currentLogLevel != "6" {
			t.Errorf("currentLogLevel = %q; want \"6\"", currentLogLevel)
		}
		mutex.RUnlock()
	})

	t.Run("GET /setlevel bad level", func(t *testing.T) {
		req := httptest.NewRequest("GET", setLogLevelPath+"?level=abc&duration=1s", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d; want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("GET /setlevel bad duration", func(t *testing.T) {
		req := httptest.NewRequest("GET", setLogLevelPath+"?level=7&duration=xyz", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d; want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("GET /setlevel XSS guard", func(t *testing.T) {
		req := httptest.NewRequest("GET", setLogLevelPath+"?level=<script>&duration=1s", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d; want %d", rec.Code, http.StatusBadRequest)
		}
		if strings.Contains(rec.Body.String(), "<script>") {
			t.Error("response contains unescaped script tag")
		}
	})
}

func TestResetPaths(t *testing.T) {
	// snapshot and restore
	mutex.Lock()
	origStartup := startupLogLevel
	origCurrent := currentLogLevel
	mutex.Unlock()

	t.Cleanup(func() {
		mutex.Lock()
		defer mutex.Unlock()
		startupLogLevel = origStartup
		currentLogLevel = origCurrent
	})

	t.Run("zero duration reset", func(t *testing.T) {
		// use a valid numeric level so Set succeeds
		mutex.Lock()
		startupLogLevel = "2"
		currentLogLevel = "5"
		mutex.Unlock()

		reset(context.Background(), 0)

		mutex.RLock()
		defer mutex.RUnlock()
		if currentLogLevel != "2" {
			t.Errorf("after zero reset, currentLogLevel = %q; want \"2\"", currentLogLevel)
		}
	})

	t.Run("cancellation reset", func(t *testing.T) {
		mutex.Lock()
		startupLogLevel = "2"
		currentLogLevel = "5"
		mutex.Unlock()

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			reset(ctx, 5*time.Second)
			close(done)
		}()

		cancel() // immediate
		select {
		case <-done:
		case <-time.After(1 * time.Second):
			t.Fatal("reset did not return after cancellation")
		}

		mutex.RLock()
		if currentLogLevel != "5" {
			t.Errorf("after cancel reset, currentLogLevel = %q; want \"5\"", currentLogLevel)
		}
		mutex.RUnlock()
	})
}
