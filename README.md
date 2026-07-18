# natprobe — spike de medição de NAT / viabilidade P2P direto

Mede se uma loja consegue **vídeo P2P direto** com um usuário externo, **sem
relay**. Feito para a auditoria em [`../docs/p2p/`](../docs/p2p/).

- **Nível 1 (`typing`)** — roda sozinho na loja em ~10s: tipo de NAT (cone vs
  simétrico), se **UDP de saída** passa, e se há **IPv6**. Responde "esta loja tem
  chance?".
- **Nível 2 (`direct`)** — prova real: um WebRTC DataChannel entre o **gateway**
  (loja) e o **viewer** (rede externa que representa o usuário). Usa **só STUN,
  nunca TURN** → nenhum candidato relay pode existir. Se conectar, confirma que o
  par selecionado **não é relay** e mede tempos. Sinalização por **copy-paste**
  (sem servidor).

## Binários prontos

- `dist/natprobe.exe` — Windows x64 (endpoints BK/Itarian). **Sem instalação, sem
  dependências.**
- `dist/natprobe-linux` — Linux x64.

Recompilar: `go build -o dist/natprobe.exe .` (com `GOOS=windows GOARCH=amd64`).

## Nível 1 — typing (roda na loja, sozinho)

No PC da loja (mesma LAN da câmera), abrir o Prompt/PowerShell e rodar:

```
natprobe.exe typing --store 27466
```

Sai um JSON no terminal **e** grava `natprobe-result.json` ao lado do .exe. **Me
envie esse JSON.** Campos que importam:

| Campo | O que significa |
|---|---|
| `udp_egress_ok` | `false` = firewall bloqueia UDP → **P2P direto inviável** nessa rede |
| `nat_mapping` | `endpoint-independent (cone)` = bom; `simétrico` = hole punch improvável |
| `hole_punch_likely` | resumo: `sim` / `improvável` / `indeterminado` |
| `mapped_public_ips` | IP público visto pela internet (deve bater com o rDNS Embratel) |
| `ipv6_ok` | `true` = há caminho IPv6 (mata o problema de NAT) |

## Nível 2 — direct (prova de conexão direta real)

Precisa de **duas pontas ao mesmo tempo**, coordenadas (ex.: você numa ligação com
o operador da loja). O **viewer** deve estar numa rede que represente o usuário
final: **4G/5G**, **fibra residencial** ou **wifi corporativo** — repita o teste
em cada uma para preencher a matriz de [`../docs/p2p/12-test-plan.md`](../docs/p2p/12-test-plan.md).

**Passo 1 — na loja (gateway):**
```
natprobe.exe direct --role gateway --store 27466
```
Ele imprime um **blob OFFER** (base64). Copie e envie ao operador do viewer.

**Passo 2 — no viewer (rede externa):**
```
natprobe.exe direct --role viewer --store 27466
```
Cole o blob OFFER quando pedir. Ele imprime um **blob ANSWER**. Envie de volta ao
operador da loja.

**Passo 3 — na loja:** cole o blob ANSWER. Em segundos as duas pontas mostram o
resultado JSON.

Resultado esperado:
```json
{
  "result": "DIRECT_OK",
  "used_relay": false,
  "selected_local_candidate_type": "srflx",
  "selected_remote_candidate_type": "srflx",
  "ice_connection_ms": 350
}
```
- `DIRECT_OK` + `used_relay:false` → **vídeo direto é viável para essa loja/rede**.
- `DIRECT_CONNECTION_UNAVAILABLE` → não fechou par direto nessa combinação (é o
  comportamento correto da regra "direto ou nada"; anote a combinação de redes).

> **Sem relay por construção:** o programa não configura nenhum servidor TURN, logo
> candidatos `relay` não existem. Se `used_relay` viesse `true`, seria um bug — e o
> resultado é forçado a `DIRECT_CONNECTION_UNAVAILABLE`.

## O que este spike NÃO faz (de propósito)

- Não instala nada, não abre porta de entrada, não toca na câmera nem em produção.
- Não transporta mídia real — usa um DataChannel para **provar o caminho direto**.
  O bitrate real de vídeo é o próximo passo (agente Pion), não este spike.
- Copy-paste é a sinalização do spike manual. Para automatizar em escala (frota
  Itarian), o próximo passo é um **rendezvous WebSocket na VPS** (só controle) —
  ainda **sem** transportar mídia. Ver [`../docs/p2p/10-poc-plan.md`](../docs/p2p/10-poc-plan.md).

## Prova opcional de que nenhum servidor viu a mídia

Durante o Nível 2, rode na VPS `sudo tcpdump -ni any udp and not port 22` e
confirme que ela **não** vê o fluxo do DataChannel — só o STUN público é tocado, e
mesmo esse não é a VPS (é STUN do Google neste spike).
