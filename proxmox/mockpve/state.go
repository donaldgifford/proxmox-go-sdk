package mockpve

import (
	"sync"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// state is the mock's mutable model. Every field is guarded by mu; handlers
// lock, copy what they need, unlock, then write the response outside the lock.
type state struct {
	mu       sync.Mutex
	version  versionData
	nodes    map[string]*nodeState   // keyed by node name
	tickets  map[string]ticketRecord // keyed by minted ticket value
	users    map[string]string       // username -> password, for /access/ticket
	qemu     qemuState
	lxc      lxcState
	storage  storageState
	ha       haState
	sdn      sdnState
	firewall firewallState
	cluster  clusterState
	access   accessState
	// acmeAccounts is cluster-scoped (/cluster/acme/account), keyed by handle.
	acmeAccounts map[string]*acmeAccountRecord
}

// versionData backs GET /version.
type versionData struct {
	Version string
	Release string
	RepoID  string
	Console string
}

// ticketRecord is a ticket minted by POST /access/ticket.
type ticketRecord struct {
	Username string
	CSRF     string
}

// nodeState holds per-node data.
type nodeState struct {
	tasks     map[string]*taskRecord     // keyed by UPID
	netIfaces map[string]*netIfaceRecord // keyed by interface name
	// Node administration (task 4).
	aptUpdates []aptUpdateRecord
	repos      []nodeRepoFileRecord
	disks      []nodeDiskRecord
	certs      []nodeCertRecord
}

// taskRecord models one asynchronous PVE task. Stopped=false is "running";
// once stopped, ExitStatus is "OK" on success or an error string on failure.
type taskRecord struct {
	UPID       string
	Node       string
	Type       string
	ID         string
	User       string
	ExitStatus string
	Stopped    bool
	StartTime  time.Time
	PID        int
	Log        []tasks.LogLine
}
