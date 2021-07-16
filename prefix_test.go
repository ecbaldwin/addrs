package ipv4

import (
	"fmt"
	"net"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func unsafeParsePrefix(cidr string) Prefix {
	prefix, _ := ParsePrefix(cidr)
	return prefix
}

func unsafePrefixFromUint32(ip, length uint32) Prefix {
	prefix, _ := PrefixFromUint32(ip, length)
	return prefix
}

func unsafeParseNet(prefix string) *net.IPNet {
	ipNet, _ := parseNet(prefix)
	return ipNet
}

func TestParsePrefix(t *testing.T) {
	tests := []struct {
		description string
		cidr        string
		expected    Prefix
		isErr       bool
	}{
		{
			description: "success",
			cidr:        "10.224.24.1/27",
			expected:    unsafePrefixFromUint32(0x0ae01801, 27),
		},
		{
			description: "ipv6",
			cidr:        "2001::1/64",
			isErr:       true,
		},
		{
			description: "bogus",
			cidr:        "bogus",
			isErr:       true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			net, err := ParsePrefix(tt.cidr)
			if tt.isErr {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tt.expected, net)
			}
		})
	}
}

func TestPrefixFromStdIPNet(t *testing.T) {
	tests := []struct {
		description string
		net         *net.IPNet
		expected    Prefix
		isErr       bool
	}{
		{
			description: "nil",
			net:         nil,
			isErr:       true,
		},
		{
			description: "ipv4",
			net:         unsafeParseNet("10.224.24.1/22"),
			expected:    unsafePrefixFromUint32(0x0ae01801, 22),
		},
		{
			description: "ipv6",
			net:         unsafeParseNet("2001::/56"),
			isErr:       true,
		},
		{
			description: "mixed up IPv4/6 IPNet",
			net: &net.IPNet{
				IP:   unsafeParseNet("2001::/56").IP,
				Mask: unsafeParseNet("10.0.0.0/16").Mask,
			},
			isErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			net, err := PrefixFromStdIPNet(tt.net)
			if tt.isErr {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tt.expected, net)
			}
		})
	}
}

func TestPrefixFromBytes(t *testing.T) {
	prefix, err := PrefixFromBytes(10, 224, 24, 1, 22)
	assert.Equal(t, unsafePrefixFromUint32(0x0ae01801, 22), prefix)
	assert.Nil(t, err)

	prefix, err = PrefixFromBytes(10, 224, 24, 1, 33)
	assert.Equal(t, unsafePrefixFromUint32(0x0ae01801, 33), prefix)
	assert.NotNil(t, err)
}

func TestPrefixFromUint32(t *testing.T) {
	prefix, err := PrefixFromUint32(0x0ae01801, 22)
	assert.Equal(t, unsafeParsePrefix("10.224.24.1/22"), prefix)
	assert.Nil(t, err)

	prefix, err = PrefixFromUint32(0x0ae01801, 33)
	assert.NotNil(t, err)
}

func TestPrefixEqual(t *testing.T) {
	first, second := unsafePrefixFromUint32(0x0ae01801, 24), unsafePrefixFromUint32(0x0ae01801, 24)
	assert.Equal(t, first, second)
	assert.True(t, first.Equal(second))
	assert.True(t, first == second)
	assert.True(t, reflect.DeepEqual(first, second))

	third := unsafePrefixFromUint32(0x0ae01701, 24)
	assert.NotEqual(t, third, second)
	assert.False(t, third.Equal(first))
	assert.False(t, third == first)
	assert.False(t, reflect.DeepEqual(third, first))
}

func TestPrefixLessThan(t *testing.T) {
	prefixes := []Prefix{
		unsafePrefixFromUint32(0x0, 0),
		unsafePrefixFromUint32(0x0, 16),
		unsafePrefixFromUint32(0x0, 31),
		unsafePrefixFromUint32(0x0, 32),
		unsafePrefixFromUint32(0x0ae01701, 16),
		unsafePrefixFromUint32(0x0ae0ffff, 16),
		unsafePrefixFromUint32(0x0ae01701, 24),
		unsafePrefixFromUint32(0x0ae01702, 24),
		unsafePrefixFromUint32(0x0ae017ff, 24),
		unsafePrefixFromUint32(0x0ae01701, 32),
		unsafePrefixFromUint32(0x0ae01702, 32),
		unsafePrefixFromUint32(0x0ae017ff, 32),
		unsafePrefixFromUint32(0x0ae01801, 24),
	}

	for a := 0; a < len(prefixes); a++ {
		for b := a; b < len(prefixes); b++ {
			t.Run(fmt.Sprintf("%d < %d", a, b), func(t *testing.T) {
				if a == b {
					assert.False(t, prefixes[a].LessThan(prefixes[b]))
				} else {
					assert.True(t, prefixes[a].LessThan(prefixes[b]))
				}
				assert.False(t, prefixes[b].LessThan(prefixes[a]))
			})
		}
	}
}

func TestNetworkHostBroadcast(t *testing.T) {
	tests := []struct {
		description              string
		prefix                   Prefix
		network, host, broadcast Prefix
	}{
		{
			description: "0",
			prefix:      unsafeParsePrefix("10.224.24.1/0"),
			network:     unsafeParsePrefix("0.0.0.0/0"),
			host:        unsafeParsePrefix("10.224.24.1/0"),
			broadcast:   unsafeParsePrefix("255.255.255.255/0"),
		},
		{
			description: "8",
			prefix:      unsafeParsePrefix("10.224.24.1/8"),
			network:     unsafeParsePrefix("10.0.0.0/8"),
			host:        unsafeParsePrefix("0.224.24.1/8"),
			broadcast:   unsafeParsePrefix("10.255.255.255/8"),
		},
		{
			description: "22",
			prefix:      unsafeParsePrefix("10.224.24.1/22"),
			network:     unsafeParsePrefix("10.224.24.0/22"),
			host:        unsafeParsePrefix("0.0.0.1/22"),
			broadcast:   unsafeParsePrefix("10.224.27.255/22"),
		},
		{
			description: "32",
			prefix:      unsafeParsePrefix("10.224.24.1/32"),
			network:     unsafeParsePrefix("10.224.24.1/32"),
			host:        unsafeParsePrefix("0.0.0.0/32"),
			broadcast:   unsafeParsePrefix("10.224.24.1/32"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			assert.Equal(t, tt.network, tt.prefix.Network())
			assert.Equal(t, tt.host, tt.prefix.Host())
			assert.Equal(t, tt.broadcast, tt.prefix.Broadcast())
		})
	}
}
