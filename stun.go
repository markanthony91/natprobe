package main

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// Cliente STUN mínimo (RFC 5389) sem dependências: envia um Binding Request e
// extrai o XOR-MAPPED-ADDRESS (endereço server-reflexive = como a internet vê a
// origem). Usamos um ÚNICO socket local para vários servidores STUN: se a porta
// mapeada muda conforme o destino, o NAT é simétrico (hostil a hole punching).

const stunMagicCookie = 0x2112A442

// stunBinding envia um Binding Request por `conn` para `server` e devolve o
// endereço refletido (IP:porta público) observado pelo servidor.
func stunBinding(conn net.PacketConn, server *net.UDPAddr, timeout time.Duration) (*net.UDPAddr, error) {
	txID := make([]byte, 12)
	if _, err := rand.Read(txID); err != nil {
		return nil, err
	}
	req := make([]byte, 20)
	binary.BigEndian.PutUint16(req[0:], 0x0001) // Binding Request
	binary.BigEndian.PutUint16(req[2:], 0)      // length
	binary.BigEndian.PutUint32(req[4:], stunMagicCookie)
	copy(req[8:], txID)

	if _, err := conn.WriteTo(req, server); err != nil {
		return nil, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 1500)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		return nil, err
	}
	return parseXORMapped(buf[:n])
}

// parseXORMapped percorre os atributos da resposta STUN e devolve o endereço do
// XOR-MAPPED-ADDRESS (0x0020) ou, em fallback, do MAPPED-ADDRESS (0x0001).
func parseXORMapped(msg []byte) (*net.UDPAddr, error) {
	if len(msg) < 20 {
		return nil, fmt.Errorf("resposta STUN curta (%d bytes)", len(msg))
	}
	attrs := msg[20:]
	for len(attrs) >= 4 {
		atype := binary.BigEndian.Uint16(attrs[0:])
		alen := int(binary.BigEndian.Uint16(attrs[2:]))
		if 4+alen > len(attrs) {
			break
		}
		val := attrs[4 : 4+alen]
		switch atype {
		case 0x0020: // XOR-MAPPED-ADDRESS
			return decodeAddr(val, true, msg[8:20])
		case 0x0001: // MAPPED-ADDRESS (fallback)
			return decodeAddr(val, false, nil)
		}
		// atributos são alinhados em 4 bytes
		pad := (4 - alen%4) % 4
		attrs = attrs[4+alen+pad:]
	}
	return nil, fmt.Errorf("sem MAPPED-ADDRESS na resposta STUN")
}

func decodeAddr(val []byte, xor bool, txHeader []byte) (*net.UDPAddr, error) {
	if len(val) < 8 {
		return nil, fmt.Errorf("atributo de endereço curto")
	}
	family := val[1]
	port := binary.BigEndian.Uint16(val[2:])
	if xor {
		port ^= uint16(stunMagicCookie >> 16)
	}
	if family != 0x01 { // só IPv4 aqui (IPv6 tratado à parte)
		return nil, fmt.Errorf("família não-IPv4 (0x%02x)", family)
	}
	ip := make(net.IP, 4)
	copy(ip, val[4:8])
	if xor {
		cookie := make([]byte, 4)
		binary.BigEndian.PutUint32(cookie, stunMagicCookie)
		for i := 0; i < 4; i++ {
			ip[i] ^= cookie[i]
		}
	}
	return &net.UDPAddr{IP: ip, Port: int(port)}, nil
}
