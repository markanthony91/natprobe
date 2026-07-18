package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
)

// Nível 2 — prova de conexão DIRETA. Duas pontas (gateway na loja, viewer numa
// rede externa) montam um WebRTC DataChannel usando SÓ STUN (nenhum TURN
// configurado → nenhum candidato relay pode existir). Sinalização por COPY-PASTE:
// cada lado imprime um blob base64 (SDP com ICE já coletado) e cola o do outro.
// Se conectar, verificamos que o par selecionado NÃO é relay e medimos.

// só STUN, jamais TURN → relay impossível por construção
var webrtcConfig = webrtc.Configuration{
	ICEServers: []webrtc.ICEServer{
		{URLs: []string{"stun:stun.l.google.com:19302"}},
		{URLs: []string{"stun:stun1.l.google.com:19302"}},
	},
}

// DirectResult é o JSON de saída do teste de conexão direta.
type DirectResult struct {
	Role             string `json:"role"`
	Store            string `json:"store"`
	Timestamp        string `json:"timestamp"`
	Connected        bool   `json:"connected"`
	Result           string `json:"result"` // DIRECT_OK | DIRECT_CONNECTION_UNAVAILABLE
	LocalCandidate   string `json:"selected_local_candidate_type"`
	RemoteCandidate  string `json:"selected_remote_candidate_type"`
	UsedRelay        bool   `json:"used_relay"`
	GatheringMS      int64  `json:"ice_gathering_ms"`
	ConnectionMS     int64  `json:"ice_connection_ms"`
	Note             string `json:"note"`
}

func encodeSD(sd webrtc.SessionDescription) string {
	b, _ := json.Marshal(sd)
	return base64.StdEncoding.EncodeToString(b)
}

func decodeSD(s string) (webrtc.SessionDescription, error) {
	var sd webrtc.SessionDescription
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return sd, err
	}
	err = json.Unmarshal(raw, &sd)
	return sd, err
}

func readBlob(prompt string) string {
	fmt.Fprintln(os.Stderr, prompt)
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			return line
		}
	}
	return ""
}

func runDirect(role, store, offerBlob string) {
	res := DirectResult{Role: role, Store: store, Timestamp: time.Now().UTC().Format(time.RFC3339)}

	pc, err := webrtc.NewPeerConnection(webrtcConfig)
	if err != nil {
		res.Note = "NewPeerConnection: " + err.Error()
		emit(res)
		return
	}
	defer pc.Close()

	connected := make(chan bool, 1)
	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		fmt.Fprintln(os.Stderr, "[ice]", s.String())
		if s == webrtc.ICEConnectionStateConnected {
			select {
			case connected <- true:
			default:
			}
		}
		if s == webrtc.ICEConnectionStateFailed || s == webrtc.ICEConnectionStateClosed {
			select {
			case connected <- false:
			default:
			}
		}
	})

	gatherStart := time.Now()

	if role == "gateway" {
		// gateway = offerer: cria o datachannel e a oferta
		dc, err := pc.CreateDataChannel("probe", nil)
		if err != nil {
			res.Note = "CreateDataChannel: " + err.Error()
			emit(res)
			return
		}
		dc.OnOpen(func() { fmt.Fprintln(os.Stderr, "[dc] aberto — canal direto estabelecido") })

		offer, _ := pc.CreateOffer(nil)
		gc := webrtc.GatheringCompletePromise(pc)
		pc.SetLocalDescription(offer)
		<-gc
		res.GatheringMS = time.Since(gatherStart).Milliseconds()

		fmt.Fprintln(os.Stderr, "\n=== COPIE o blob OFFER abaixo e envie ao operador do VIEWER ===")
		fmt.Println(encodeSD(*pc.LocalDescription()))
		answer := readBlob("\n=== COLE aqui o blob ANSWER recebido do VIEWER e tecle Enter ===")
		sd, err := decodeSD(answer)
		if err != nil {
			res.Note = "answer inválido: " + err.Error()
			emit(res)
			return
		}
		pc.SetRemoteDescription(sd)
	} else {
		// viewer = answerer: recebe a oferta e responde
		if offerBlob == "" {
			offerBlob = readBlob("=== COLE aqui o blob OFFER recebido do GATEWAY e tecle Enter ===")
		}
		sd, err := decodeSD(offerBlob)
		if err != nil {
			res.Note = "offer inválido: " + err.Error()
			emit(res)
			return
		}
		pc.SetRemoteDescription(sd)
		answer, _ := pc.CreateAnswer(nil)
		gc := webrtc.GatheringCompletePromise(pc)
		pc.SetLocalDescription(answer)
		<-gc
		res.GatheringMS = time.Since(gatherStart).Milliseconds()

		fmt.Fprintln(os.Stderr, "\n=== COPIE o blob ANSWER abaixo e envie ao operador do GATEWAY ===")
		fmt.Println(encodeSD(*pc.LocalDescription()))
	}

	connStart := time.Now()
	select {
	case ok := <-connected:
		res.Connected = ok
	case <-time.After(30 * time.Second):
		res.Connected = false
	}
	res.ConnectionMS = time.Since(connStart).Milliseconds()

	if res.Connected {
		if pair, err := selectedPair(pc); err == nil && pair != nil {
			res.LocalCandidate = pair.Local.Typ.String()
			res.RemoteCandidate = pair.Remote.Typ.String()
			res.UsedRelay = pair.Local.Typ == webrtc.ICECandidateTypeRelay ||
				pair.Remote.Typ == webrtc.ICECandidateTypeRelay
		}
		if res.UsedRelay {
			res.Result = "DIRECT_CONNECTION_UNAVAILABLE"
			res.Note = "Conectou por RELAY — proibido. (Não deveria ocorrer: nenhum TURN configurado.)"
		} else {
			res.Result = "DIRECT_OK"
			res.Note = fmt.Sprintf("Conexão DIRETA confirmada (%s ↔ %s).", res.LocalCandidate, res.RemoteCandidate)
		}
	} else {
		res.Result = "DIRECT_CONNECTION_UNAVAILABLE"
		res.Note = "Nenhum par de candidatos direto validou nesta combinação de redes."
	}
	emit(res)
}

func selectedPair(pc *webrtc.PeerConnection) (*webrtc.ICECandidatePair, error) {
	sctp := pc.SCTP()
	if sctp == nil {
		return nil, fmt.Errorf("sem SCTP")
	}
	t := sctp.Transport()
	if t == nil {
		return nil, fmt.Errorf("sem DTLS transport")
	}
	it := t.ICETransport()
	if it == nil {
		return nil, fmt.Errorf("sem ICE transport")
	}
	return it.GetSelectedCandidatePair()
}
