package main

import (
	"net"
	"testing"
)

func addr(ip string, port int) *net.UDPAddr {
	return &net.UDPAddr{IP: net.ParseIP(ip), Port: port}
}

func TestClassifyMapping(t *testing.T) {
	cases := []struct {
		name         string
		mapped       []*net.UDPAddr
		hasPublic    bool
		wantMapping  string
		wantHolePnch string
	}{
		{
			name:         "ip publico na maquina",
			mapped:       []*net.UDPAddr{addr("189.1.1.1", 5000)},
			hasPublic:    true,
			wantMapping:  "sem-nat (ip público na máquina)",
			wantHolePnch: "sim",
		},
		{
			name:         "cone: mesma porta em todos os servidores",
			mapped:       []*net.UDPAddr{addr("201.56.206.202", 40001), addr("201.56.206.202", 40001), addr("201.56.206.202", 40001)},
			hasPublic:    false,
			wantMapping:  "endpoint-independent (cone)",
			wantHolePnch: "sim",
		},
		{
			name:         "simetrico: porta muda por destino",
			mapped:       []*net.UDPAddr{addr("201.56.206.202", 40001), addr("201.56.206.202", 40002), addr("201.56.206.202", 40003)},
			hasPublic:    false,
			wantMapping:  "address-or-port-dependent (simétrico)",
			wantHolePnch: "improvável",
		},
		{
			name:         "so um servidor respondeu",
			mapped:       []*net.UDPAddr{addr("201.56.206.202", 40001)},
			hasPublic:    false,
			wantMapping:  "unknown (só 1 servidor respondeu)",
			wantHolePnch: "indeterminado",
		},
		{
			name:         "nenhuma resposta",
			mapped:       nil,
			hasPublic:    false,
			wantMapping:  "unknown",
			wantHolePnch: "indeterminado",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, h := classifyMapping(c.mapped, c.hasPublic)
			if m != c.wantMapping || h != c.wantHolePnch {
				t.Fatalf("classifyMapping = (%q,%q); quer (%q,%q)", m, h, c.wantMapping, c.wantHolePnch)
			}
		})
	}
}

func TestIsPrivate(t *testing.T) {
	priv := []string{"10.1.2.3", "192.168.0.1", "172.16.5.5", "100.64.0.1", "169.254.1.1"}
	pub := []string{"189.86.234.230", "201.56.206.202", "8.8.8.8"}
	for _, p := range priv {
		if !isPrivate(net.ParseIP(p)) {
			t.Errorf("%s deveria ser privado", p)
		}
	}
	for _, p := range pub {
		if isPrivate(net.ParseIP(p)) {
			t.Errorf("%s deveria ser público", p)
		}
	}
}
