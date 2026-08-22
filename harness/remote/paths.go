package remote

import "semantix/harness/config"

// defaultManagedKnownHosts is the Semantix-managed known_hosts path. It is a
// thin indirection over config so tests can leave HostKeyPolicy.ManagedPath
// empty and still get an isolated file under SEMANTIX_HOME.
func defaultManagedKnownHosts() string {
	return config.RemoteKnownHostsPath()
}
