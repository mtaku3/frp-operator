package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mtaku3/frp-operator/pkg/cloudprovider/frps/admin"
)

func TestGetServerInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/serverinfo", r.URL.Path)
		_, _ = w.Write([]byte(`{"version":"v0.68.1","bindPort":7000}`))
	}))
	defer srv.Close()

	c := admin.New(srv.URL)
	info, err := c.GetServerInfo(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v0.68.1", info.Version)
	require.Equal(t, 7000, info.BindPort)
}

func TestListProxies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/proxy/tcp", r.URL.Path)
		_, _ = w.Write([]byte(`{"proxies":[{"name":"p1","type":"tcp","remotePort":6000}]}`))
	}))
	defer srv.Close()

	c := admin.New(srv.URL)
	out, err := c.ListProxies(context.Background())
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "p1", out[0].Name)
	require.Equal(t, 6000, out[0].RemotePort)
}

func TestGetServerInfo_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := admin.New(srv.URL)
	_, err := c.GetServerInfo(context.Background())
	require.Error(t, err)
}
