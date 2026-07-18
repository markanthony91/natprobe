package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

// Servidores STUN em IPs distintos — necessário para diferenciar mapeamento
// endpoint-independent (cone) de simétrico comparando a porta mapeada por destino.
var stunServers = []string{
	"stun.l.google.com:19302",
	"stun1.l.google.com:19302",
	"stun.cloudflare.com:3478",
}

// TypingResult é o JSON que a loja nos devolve (Nível 1).
type TypingResult struct {
	Store          string   `json:"store"`
	Timestamp      string   `json:"timestamp"`
	UDPEgressOK    bool     `json:"udp_egress_ok"`
	NATMapping     string   `json:"nat_mapping"` // endpoint-independent | address-or-port-dependent(symmetric) | unknown
	HolePunchLikely string  `json:"hole_punch_likely"` // sim | improvável | indeterminado
	MappedIPs      []string `json:"mapped_public_ips"`
	MappedPorts    []int    `json:"mapped_ports_per_server"`
	LocalIPs       []string `json:"local_ips"`
	HasPublicLocal bool     `json:"has_public_local_ip"` // true = sem NAT (IP público na própria máquina)
	CGNATNote      string   `json:"cgnat_note"`
	IPv6OK         bool     `json:"ipv6_ok"`
	IPv6Addr       string   `json:"ipv6_global_addr"`
	Errors         []string `json:"errors"`
}

func runTyping(store string) {
	res := TypingResult{
		Store:     store,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// Um único socket local para TODOS os servidores (chave da detecção de simétrico).
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		res.Errors = append(res.Errors, "não abriu socket UDP local: "+err.Error())
		emit(res)
		return
	}
	defer conn.Close()

	var mapped []*net.UDPAddr
	for _, s := range stunServers {
		addr, err := net.ResolveUDPAddr("udp4", s)
		if err != nil {
			res.Errors = append(res.Errors, s+": DNS falhou: "+err.Error())
			continue
		}
		m, err := stunBinding(conn, addr, 4*time.Second)
		if err != nil {
			res.Errors = append(res.Errors, s+": sem resposta: "+err.Error())
			continue
		}
		mapped = append(mapped, m)
		res.MappedIPs = append(res.MappedIPs, m.IP.String())
		res.MappedPorts = append(res.MappedPorts, m.Port)
	}

	res.UDPEgressOK = len(mapped) > 0
	res.LocalIPs, res.HasPublicLocal = localInterfaces()
	res.NATMapping, res.HolePunchLikely = classifyMapping(mapped, res.HasPublicLocal)
	res.CGNATNote = cgnatNote(mapped, res.HasPublicLocal)
	res.IPv6OK, res.IPv6Addr = checkIPv6()

	if !res.UDPEgressOK {
		res.HolePunchLikely = "improvável"
		res.CGNATNote = "UDP de saída bloqueado (nenhum STUN respondeu) — P2P direto inviável nesta rede sem liberar UDP no firewall."
	}
	emit(res)
}

// classifyMapping é PURA (testável): decide o tipo de mapeamento a partir dos
// endereços refletidos por servidores distintos.
func classifyMapping(mapped []*net.UDPAddr, hasPublicLocal bool) (mapping, holePunch string) {
	if hasPublicLocal {
		return "sem-nat (ip público na máquina)", "sim"
	}
	if len(mapped) == 0 {
		return "unknown", "indeterminado"
	}
	if len(mapped) == 1 {
		return "unknown (só 1 servidor respondeu)", "indeterminado"
	}
	first := mapped[0]
	for _, m := range mapped[1:] {
		if m.Port != first.Port || !m.IP.Equal(first.IP) {
			// porta/ip muda conforme o destino → NAT simétrico
			return "address-or-port-dependent (simétrico)", "improvável"
		}
	}
	return "endpoint-independent (cone)", "sim"
}

func cgnatNote(mapped []*net.UDPAddr, hasPublicLocal bool) string {
	if hasPublicLocal {
		return "Máquina tem IP público direto — sem NAT/CGNAT."
	}
	if len(mapped) == 0 {
		return "Indeterminado (sem endereço refletido)."
	}
	return "IP público refletido = " + mapped[0].IP.String() +
		". CGNAT não é confirmável só por STUN; o Nível 2 (conexão direta real) confirma se o hole punch fecha."
}

func localInterfaces() (ips []string, hasPublic bool) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, false
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		ip := ipnet.IP
		if ip.To4() != nil {
			ips = append(ips, ip.String())
			if ip.IsGlobalUnicast() && !isPrivate(ip) {
				hasPublic = true
			}
		}
	}
	return ips, hasPublic
}

func isPrivate(ip net.IP) bool {
	for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "169.254.0.0/16"} {
		_, n, _ := net.ParseCIDR(cidr)
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// checkIPv6 tenta um STUN sobre IPv6; sucesso = há caminho IPv6 global (evita NAT).
func checkIPv6() (bool, string) {
	conn, err := net.ListenPacket("udp6", ":0")
	if err != nil {
		return false, ""
	}
	defer conn.Close()
	addr, err := net.ResolveUDPAddr("udp6", "stun.l.google.com:19302")
	if err != nil || addr.IP.To4() != nil {
		return false, ""
	}
	if _, err := stunBinding(conn, addr, 4*time.Second); err != nil {
		return false, ""
	}
	// pega um IPv6 global local para reportar
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			ip := ipnet.IP
			if ip.To4() == nil && ip.IsGlobalUnicast() && !ip.IsPrivate() {
				return true, ip.String()
			}
		}
	}
	return true, "(global v6 presente)"
}

func emit(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
	// também grava em arquivo ao lado do binário, para facilitar o envio
	_ = os.WriteFile("natprobe-result.json", b, 0o644)
}
