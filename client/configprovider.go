package client

import (
	"context"
	"sync"
)

// Config carries the sidecar coordinates resolved by a ConfigProvider.
type Config struct {
	// Address is the sidecar's gRPC endpoint, in the same form as DAPR_GRPC_ENDPOINT.
	Address string

	// APIToken is the token sent as dapr-api-token. Empty means no token.
	APIToken string
}

// ConfigProvider supplies the sidecar address and API token when the environment does not.
//
// It exists for callers that only know where their sidecar is at runtime: a hosted Dapr offering
// that issues an endpoint and a token per application, a service discovery lookup, or a secret
// manager holding the token. Registering a provider lets a library supply that configuration
// without the application having to plumb it through its own code.
//
// Config is called at most once per process, while the default client is being built, with the
// caller's context. Returning an error fails client construction, so a provider that has to reach
// the network can report why instead of panicking.
type ConfigProvider interface {
	Config(ctx context.Context) (Config, error)
}

var (
	configProviderMu sync.RWMutex
	registeredConfig ConfigProvider
)

// RegisterConfigProvider registers p as the source of sidecar configuration for NewClient and
// NewClientWithContext, replacing any previously registered provider. Passing nil unregisters.
//
// The environment still wins: a provider is consulted only when DAPR_GRPC_ENDPOINT is unset, so
// running under `dapr run`, or under any tool that injects the endpoint, behaves exactly as it
// does today.
//
// Registration is expected from a package's init function, in the style of database/sql drivers,
// so that it happens before the first client is built. It must not do any work of its own:
// resolving the configuration is what Config is for.
func RegisterConfigProvider(p ConfigProvider) {
	configProviderMu.Lock()
	defer configProviderMu.Unlock()
	registeredConfig = p
}

func configProvider() ConfigProvider {
	configProviderMu.RLock()
	defer configProviderMu.RUnlock()
	return registeredConfig
}

// ConfigProviderFunc adapts a function to the ConfigProvider interface.
type ConfigProviderFunc func(ctx context.Context) (Config, error)

// Config calls f.
func (f ConfigProviderFunc) Config(ctx context.Context) (Config, error) {
	return f(ctx)
}
