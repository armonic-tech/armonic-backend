package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func request(t *testing.T, remoteAddr string, xff ...string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remoteAddr
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	return r
}

func TestClientIPIgnoresForwardedHeaderFromUntrustedPeer(t *testing.T) {
	r, err := NewIPResolver(nil)
	require.NoError(t, err)

	require.Equal(t, "203.0.113.9", r.Key(request(t, "203.0.113.9:5555", "1.2.3.4")))
}

func TestClientIPUsesForwardedHeaderFromTrustedProxy(t *testing.T) {
	r, err := NewIPResolver([]string{"172.17.0.0/16"})
	require.NoError(t, err)

	require.Equal(t, "198.51.100.7", r.Key(request(t, "172.17.0.1:5555", "198.51.100.7")))
}

func TestClientIPSkipsTrustedHopsRightToLeft(t *testing.T) {
	r, err := NewIPResolver([]string{"172.17.0.0/16", "10.0.0.0/8"})
	require.NoError(t, err)

	req := request(t, "172.17.0.1:5555", "1.1.1.1, 198.51.100.7, 10.0.0.5")
	require.Equal(t, "198.51.100.7", r.Key(req))
}

func TestClientIPAcrossSeparateForwardedHeaders(t *testing.T) {
	r, err := NewIPResolver([]string{"172.17.0.0/16", "10.0.0.0/8"})
	require.NoError(t, err)

	req := request(t, "172.17.0.1:5555", "198.51.100.7", "10.0.0.5")
	require.Equal(t, "198.51.100.7", r.Key(req))
}

func TestClientIPFallsBackToPeerWhenEveryHopIsOurs(t *testing.T) {
	r, err := NewIPResolver([]string{"172.17.0.0/16"})
	require.NoError(t, err)

	require.Equal(t, "172.17.0.1", r.Key(request(t, "172.17.0.1:5555", "172.17.0.9")))
}

func TestClientIPFallsBackToPeerWithNoForwardedHeader(t *testing.T) {
	r, err := NewIPResolver([]string{"172.17.0.0/16"})
	require.NoError(t, err)

	require.Equal(t, "172.17.0.1", r.Key(request(t, "172.17.0.1:5555")))
}

func TestClientIPRejectsMalformedHop(t *testing.T) {
	r, err := NewIPResolver([]string{"172.17.0.0/16"})
	require.NoError(t, err)

	require.Equal(t, unknownKey, r.Key(request(t, "172.17.0.1:5555", "not-an-ip")))
}

func TestKeyBucketsIPv6BySlash64(t *testing.T) {
	r, err := NewIPResolver(nil)
	require.NoError(t, err)

	first := r.Key(request(t, "[2001:db8:1:2::1]:5555"))
	second := r.Key(request(t, "[2001:db8:1:2:ffff:ffff:ffff:ffff]:5555"))
	require.Equal(t, first, second)

	other := r.Key(request(t, "[2001:db8:1:3::1]:5555"))
	require.NotEqual(t, first, other)
}

func TestKeyTreatsMappedIPv4AsIPv4(t *testing.T) {
	r, err := NewIPResolver(nil)
	require.NoError(t, err)

	require.Equal(t, "203.0.113.9", r.Key(request(t, "[::ffff:203.0.113.9]:5555")))
}

func TestResolverAcceptsBareAddressesAsTrusted(t *testing.T) {
	r, err := NewIPResolver([]string{"172.17.0.1"})
	require.NoError(t, err)

	require.Equal(t, "198.51.100.7", r.Key(request(t, "172.17.0.1:5555", "198.51.100.7")))
	require.Equal(t, "172.17.0.2", r.Key(request(t, "172.17.0.2:5555", "198.51.100.7")))
}

func TestResolverRejectsInvalidTrustedProxy(t *testing.T) {
	_, err := NewIPResolver([]string{"not a cidr"})
	require.Error(t, err)
}

func TestResolverIgnoresBlankEntries(t *testing.T) {
	r, err := NewIPResolver([]string{"", "  ", "172.17.0.0/16"})
	require.NoError(t, err)
	require.Len(t, r.trusted, 1)
}

func TestKeyOnUnparseableRemoteAddr(t *testing.T) {
	r, err := NewIPResolver(nil)
	require.NoError(t, err)
	require.Equal(t, unknownKey, r.Key(request(t, "pipe")))
}
