/*
Copyright 2023 The Kubernetes Authors.

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
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/helpers"
)

const (
	DefaultSocketDir = "/tmp/klog-socks" // Default directory storing socket files
	SocketSuffix     = "-klog.sock"
	SocketDirEnvName = "DEBUG_SOCKET_DIR"
	// The HTTP request patterns
	setLogLevelPath  = "/setlevel"
	getLogLevelPath  = "/getlevel"
	exampleSocketCli = "\"Failed to change klog log level, because got wrong value from level argument\\n\"+\n\t\t\t\t\"example: curl --unix-socket /tmp/klog-socks/componentName-klog.sock \\\"http://localhost/setlevel?level=8&duration=60s\\\"\\n\"+\n\t\t\t\t\"level=8 means changing klog log level to 8\\n\"+\n\t\t\t\t\"duration=60s means maintaining level=8 for 60 seconds[60m -> 60 minutes; 60h -> 60 hours]\""
)

var (
	// When users frequently make request to change klog log level, the previously registered timer may not expire.
	// To improve performance, cancel the previous timer. prevCtx, prevCtxCancelFunc is used to achieve this target.
	prevCtx           context.Context
	prevCtxCancelFunc context.CancelFunc

	// currentLogLevel stores current log level
	currentLogLevel string
	// startupLogLevel stores start-up log level
	startupLogLevel string
	// mutex is used to avoid data race about prevCtx, prevCtxCancelFunc and currentLogLevel
	mutex sync.RWMutex
)

// responseOk returns a statusOK response to client
func responseOk(w *http.ResponseWriter, okMsg string) {
	(*w).Header().Set("Content-Type", "text/plain; charset=utf-8")
	(*w).Header().Set("X-Content-Type-Options", "nosniff")
	_, err := fmt.Fprint(*w, okMsg)
	if err != nil {
		klog.Error(err)
		return
	}
}

// responseError returns an error response containing specific httpCode and errMsg to client
func responseError(w *http.ResponseWriter, errMsg string, httpCode int) {
	http.Error(*w, errMsg, httpCode)
}

// modifyLoglevel will try to change current klog's log level to newLogLevel and assign it to currentLogLevel.
// After prevCtxCancelFunc function corresponding to last timer executed, prevCtx and prevCtxCancelFunc will be reassigned
// in order to represent brand-new timer.
func modifyLoglevel(newLogLevel string) error {
	mutex.Lock()
	defer mutex.Unlock()

	// Change klog log level to new value
	var loglevel klog.Level
	if err := loglevel.Set(newLogLevel); err != nil {
		return err
	}
	currentLogLevel = newLogLevel

	// Cancel the previous timer.
	if prevCtxCancelFunc != nil {
		prevCtxCancelFunc()
	}
	prevCtx, prevCtxCancelFunc = context.WithCancel(context.Background())
	return nil
}

// reset creates a timer to make klog recover to start-up log level.
func reset(ctx context.Context, duration time.Duration) {
	defer runtime.HandleCrash()
	select {
	// Create a timer
	case <-time.After(duration):
		var loglevel klog.Level
		mutex.Lock()
		defer mutex.Unlock()
		if err := loglevel.Set(startupLogLevel); err != nil {
			klog.Error(err)
			return
		}
		currentLogLevel = startupLogLevel
		klog.InfoS("Klog recover to start-up log level successfully", "startupLogLevel", startupLogLevel)
	// Cancel previous timer
	case <-ctx.Done():
		klog.InfoS("Cancel previous timer successfully")
	}
}

// installKlogLogLevelHandler registers the HTTP request patterns that can set/get current klog log level
func installKlogLogLevelHandler(mux *http.ServeMux, startup string) {
	currentLogLevel, startupLogLevel = startup, startup
	// Register the HTTP request patterns that can change klog log level
	mux.Handle(setLogLevelPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		rawLevel := values.Get("level")
		rawDuration := values.Get("duration")
		// Escape the data that needs to be output to the log, Prevent Reflected cross-site scripting
		rawLevel = html.EscapeString(rawLevel)
		rawDuration = html.EscapeString(rawDuration)
		var duration time.Duration
		var err error
		// Validate argument in request
		if level, err := strconv.ParseInt(rawLevel, 10, 64); err != nil || level <= 0 {
			responseError(&w, exampleSocketCli, http.StatusBadRequest)
			return
		}
		if duration, err = time.ParseDuration(rawDuration); err != nil || duration.Milliseconds() <= 0 {
			responseError(&w, exampleSocketCli, http.StatusBadRequest)
			return
		}

		if err := modifyLoglevel(rawLevel); err != nil {
			responseError(&w, fmt.Sprintf("Failed to change klog log level. Error: %v\n", err.Error()), http.StatusInternalServerError)
			return
		}

		mutex.RLock()
		// Create a timer to make klog recover to start-up log level.
		// There will be more than one timer using same prevCtx variable under extreme conditions.
		// Therefore, put reset function in mutex range.
		go reset(prevCtx, duration)
		responseOk(&w, fmt.Sprintf("Change klog log level to %s successfully and  for %v\n", currentLogLevel, duration))
		mutex.RUnlock()
	}))

	// Register the HTTP request patterns that can get current klog log level
	mux.Handle(getLogLevelPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.RLock()
		responseOk(&w, fmt.Sprintf("Current klog log level: %s\n", currentLogLevel))
		mutex.RUnlock()
	}))
}

// listenUnix does net.Listen for a unix socket
func listenUnix(componentName string, socketDir string) (net.Listener, error) {
	// Use default directory to store socket files
	if len(socketDir) == 0 {
		socketDir = DefaultSocketDir
	}

	// Check whether KlogLogLevelSocketDir exists
	if _, err := os.Stat(socketDir); os.IsNotExist(err) {
		if err = os.MkdirAll(socketDir, 0750); err != nil {
			return nil, fmt.Errorf("error creating klog log level socket dir: %v", err)
		}
	}

	// Specify socket file full path
	socketFileFullName := componentName + SocketSuffix
	socketFileFullPath := filepath.Join(socketDir, socketFileFullName)

	// Remove any socket, stale or not, but fall through for other files
	fi, err := os.Stat(socketFileFullPath)
	if err == nil && (fi.Mode()&os.ModeSocket) != 0 {
		err := os.Remove(socketFileFullPath)
		if err != nil {
			klog.ErrorS(err, "failed to remote socket file", "file", socketFileFullPath)
			return nil, err
		}
	}

	// Default to only user accessible socket, caller can open up later if desired
	// Result perm: 777 - 077 = 700
	oldmask := unix.Umask(0077)
	l, err := net.Listen("unix", socketFileFullPath)
	unix.Umask(oldmask)

	return l, err
}

// serveOnListener starts the server using given listener, loops forever.
func serveOnListener(l net.Listener, m *http.ServeMux) error {
	server := http.Server{
		Handler:           m,
		ReadHeaderTimeout: helpers.DefaultReadHeaderTimeout,
		ReadTimeout:       helpers.DefaultReadTimeout,
		WriteTimeout:      helpers.DefaultWriteTimeout,
	}
	return server.Serve(l)
}

// ListenAndServeKlogLogLevel registers a server on specific component to handle the HTTP request which set/get klog log level
func ListenAndServeKlogLogLevel(componentName string, startupLogLevel string, socketDir string) {
	var err error
	defer runtime.HandleCrash()

	mux := http.NewServeMux()
	installKlogLogLevelHandler(mux, startupLogLevel)

	var listener net.Listener
	listener, err = listenUnix(componentName, socketDir)
	if err != nil {
		return
	}

	if err = serveOnListener(listener, mux); err != nil {
		return
	}
}
