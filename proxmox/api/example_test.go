package api_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
)

func ExampleClient_DoRequest() {
	// A stand-in for a PVE node; in real use this is your cluster address.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"data":{"version":"9.2.1"}}`)
	}))
	defer srv.Close()

	c, _ := api.New(srv.URL, api.TokenCredentials("root@pam!sdk", "secret"))

	var out struct {
		Version string `json:"version"`
	}
	_ = c.DoRequest(context.Background(), http.MethodGet, "/version", nil, &out)
	fmt.Println(out.Version)
	// Output: 9.2.1
}
