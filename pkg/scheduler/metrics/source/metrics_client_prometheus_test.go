package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	basicAuthUser   = "username"
	basicAuthPwd    = "password"
	bearerAuthToken = "bearertoken"
)

func TestPrometheusMetricsClient_NodeMetricsAvg(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/basic-auth") {
			userName, password, ok := req.BasicAuth()
			if !ok || userName != basicAuthUser || password != basicAuthPwd {
				t.Errorf("basic auth missmatch, ok: %v, userName: %v, password: %v", ok, userName, password)
				res.WriteHeader(http.StatusUnauthorized)
			}

		} else if strings.HasPrefix(req.URL.Path, "/token-auth") {
			if auth := req.Header.Get("Authorization"); auth != "Bearer "+bearerAuthToken {
				t.Errorf("bearer token missmatch, token: %s", auth)
				res.WriteHeader(http.StatusUnauthorized)
			}
		}
		res.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
		res.WriteHeader(http.StatusOK)
	}))
	defer testServer.Close()
	// test basic auth
	conf := map[string]string{"username": basicAuthUser, "password": basicAuthPwd}
	metricsClient, _ := NewPrometheusMetricsClient(testServer.URL+"/basic-auth", conf)
	if _, err := metricsClient.NodeMetricsAvg(context.TODO(), "node-name", "5m"); err != nil {
		t.Errorf("Get Node Metric Avg with basic auth err %v", err)
	}
	// test token auth
	conf = map[string]string{"bearertoken": bearerAuthToken}
	metricsClient, _ = NewPrometheusMetricsClient(testServer.URL+"/token-auth", conf)
	if _, err := metricsClient.NodeMetricsAvg(context.TODO(), "node-name", "5m"); err != nil {
		t.Errorf("Get Node Metric Avg with token auth err %v", err)
	}
}
