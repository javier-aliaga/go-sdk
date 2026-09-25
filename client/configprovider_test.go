package client

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// startTestServer returns the address of a gRPC server with no services registered. It serves no
// calls, which is enough: these tests are about how the address is resolved, and the dial is
// blocking, so something has to complete the handshake.
func startTestServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}

// resetClientState clears the package singletons so each case builds its own client.
func resetClientState(t *testing.T) {
	t.Helper()
	lock.Lock()
	defaultClient = nil
	lock.Unlock()
	RegisterConfigProvider(nil)
	t.Cleanup(func() {
		lock.Lock()
		defaultClient = nil
		lock.Unlock()
		RegisterConfigProvider(nil)
	})
}

func TestConfigProviderIsIgnoredWhenTheEndpointIsInTheEnvironment(t *testing.T) {
	resetClientState(t)
	t.Setenv(daprGRPCEndpointEnvVarName, startTestServer(t))

	called := false
	RegisterConfigProvider(ConfigProviderFunc(func(context.Context) (Config, error) {
		called = true
		return Config{Address: "unused"}, nil
	}))

	c, err := NewClientWithContext(context.Background())

	require.NoError(t, err)
	require.NotNil(t, c)
	// The environment is what `dapr run` and hosted CLIs inject. A provider must never take it
	// over, or registering one would silently change where an existing app connects.
	assert.False(t, called, "provider must not be consulted when DAPR_GRPC_ENDPOINT is set")
}

func TestConfigProviderSuppliesAddressAndTokenWhenTheEnvironmentIsEmpty(t *testing.T) {
	resetClientState(t)

	address := startTestServer(t)
	RegisterConfigProvider(ConfigProviderFunc(func(ctx context.Context) (Config, error) {
		require.NotNil(t, ctx, "provider receives the caller's context")
		return Config{Address: address, APIToken: "a-token"}, nil
	}))

	c, err := NewClientWithContext(context.Background())

	require.NoError(t, err)
	grpcClient, ok := c.(*GRPCClient)
	require.True(t, ok)
	assert.Equal(t, "a-token", grpcClient.authToken.get())
}

func TestConfigProviderErrorFailsClientCreation(t *testing.T) {
	resetClientState(t)

	want := errors.New("no project configured")
	RegisterConfigProvider(ConfigProviderFunc(func(context.Context) (Config, error) {
		return Config{}, want
	}))

	c, err := NewClientWithContext(context.Background())

	// The reason a provider beats an init function: a failure here is returned, not a panic.
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
	assert.Nil(t, c)
}

func TestConfigProviderCancellationIsHonoured(t *testing.T) {
	resetClientState(t)

	RegisterConfigProvider(ConfigProviderFunc(func(ctx context.Context) (Config, error) {
		<-ctx.Done()
		return Config{}, ctx.Err()
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewClientWithContext(ctx)

	require.ErrorIs(t, err, context.Canceled)
}

func TestWithoutAProviderNothingChanges(t *testing.T) {
	resetClientState(t)
	t.Setenv(daprPortEnvVarName, "4003")
	t.Setenv(clientTimeoutSecondsEnvVarName, "1")

	_, err := NewClientWithContext(context.Background())

	// Nothing is listening on 4003, so this cannot succeed. What it proves is the routing: with no
	// provider registered, resolution still falls through to DAPR_GRPC_PORT exactly as before.
	require.ErrorContains(t, err, "127.0.0.1:4003")
}

func TestAnEmptyAddressFallsThroughToThePortDefault(t *testing.T) {
	resetClientState(t)
	t.Setenv(daprPortEnvVarName, "4003")
	t.Setenv(clientTimeoutSecondsEnvVarName, "1")

	RegisterConfigProvider(ConfigProviderFunc(func(context.Context) (Config, error) {
		return Config{}, nil
	}))

	_, err := NewClientWithContext(context.Background())

	// A provider that has nothing to say is not an error: it steps aside and the port is used.
	require.ErrorContains(t, err, "127.0.0.1:4003")
}
