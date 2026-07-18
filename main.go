// natprobe — mede a viabilidade de P2P direto de uma loja.
//
//	natprobe typing --store 27466
//	natprobe direct --role gateway --store 27466
//	natprobe direct --role viewer  --store 27466 [--offer <blob>]
//
// Nível 1 (typing): tipo de NAT, UDP de saída, IPv6 — roda sozinho na loja.
// Nível 2 (direct): prova de conexão direta real entre gateway (loja) e viewer
// (rede externa), via copy-paste de blobs SDP. Só STUN, nunca relay.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	store := fs.String("store", "", "código da loja (ex.: 27466)")
	role := fs.String("role", "gateway", "direct: gateway (loja) ou viewer (externo)")
	offer := fs.String("offer", "", "direct/viewer: blob OFFER (opcional; senão pede na entrada)")
	_ = fs.Parse(os.Args[2:])

	switch cmd {
	case "typing":
		runTyping(*store)
	case "direct":
		if *role != "gateway" && *role != "viewer" {
			fmt.Fprintln(os.Stderr, "role deve ser gateway ou viewer")
			os.Exit(2)
		}
		runDirect(*role, *store, *offer)
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `natprobe — mede P2P direto de uma loja

Uso:
  natprobe typing --store <cod>
  natprobe direct --role gateway --store <cod>
  natprobe direct --role viewer  --store <cod> [--offer <blob>]

Saída: JSON no terminal e em natprobe-result.json`)
	os.Exit(2)
}
