package access_test

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/access"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
)

// Example creates a user and mints an API token for it, printing the one-time
// secret's owner. On a live cluster you would store secret.Value immediately —
// PVE never shows it again.
func Example() {
	// mockpve stands in for a live cluster so the example is self-contained;
	// against a real node, pass its URL and a real token to proxmox.NewClient.
	mock := mockpve.New()
	ts := mock.Serve()
	defer ts.Close()

	ctx := context.Background()
	c, err := proxmox.NewClient(ctx, ts.URL, api.TokenCredentials("root@pam!sdk", "secret"))
	if err != nil {
		fmt.Println("connect:", err)
		return
	}
	a := c.Access()

	if err := a.CreateUser(ctx, &access.UserSpec{UserID: "ci@pve"}); err != nil {
		fmt.Println("create user:", err)
		return
	}
	secret, err := a.CreateToken(ctx, "ci@pve", "deploy", &access.TokenSpec{Comment: "ci pipeline"})
	if err != nil {
		fmt.Println("create token:", err)
		return
	}
	fmt.Printf("token %s issued (secret len %d)\n", secret.FullTokenID, len(secret.Value))
	// Output:
	// token ci@pve!deploy issued (secret len 13)
}
