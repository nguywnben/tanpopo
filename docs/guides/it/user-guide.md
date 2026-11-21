# Guida utente Tanpopo

**Tanpopo** è un AI Gateway e Hub MCP (Model Context Protocol) self-hosted in un singolo binario, progettato per routing deterministico, sicurezza delle credenziali in sola scrittura, contabilità FinOps esatta in micro-USD e funzionamento offline.

---

## 1. Avvio rapido e configurazione iniziale

1. Avvia il server Tanpopo:
   ```bash
   tanpopo serve --role all --listen 127.0.0.1:8080
   ```
2. Al primo avvio, Tanpopo stampa un **token bootstrap** monouso nel log del server.
3. Apri `http://127.0.0.1:8080/setup` nel browser e completa la procedura guidata in 4 fasi:
   - **Account proprietario:** Inserisci il token bootstrap, l'email amministratore, la password (minimo 12 caratteri), il nome dell'organizzazione e del primo progetto.
   - **Prima connessione (opzionale):** Collega un provider commerciale (OpenAI, Anthropic, Google Gemini o compatibile OpenAI) o un motore locale (vLLM, Ollama). Gli endpoint locali richiedono di abilitare esplicitamente la rete privata e indicare CIDR esatti senza jolly (es. `127.0.0.1/32`).
   - **Custodia chiave:** Conferma il backup della chiave master di cifratura (Master KEK). La perdita rende irrecuperabili i segreti cifrati nel vault.
   - **Riepilogo e inizializzazione:** Completa la configurazione, salva la chiave virtuale mostrata una sola volta e verifica la prima richiesta in streaming.

---

## 2. Lingua e aspetto

- **Console Web:** Usa il selettore lingua nella barra superiore (e nelle pagine Configurazione e Accesso). **Auto** è selezionato di default e sceglie la prima lingua supportata dal browser (ripiego: English). La scelta manuale persiste dopo il ricaricamento ed è sempre possibile tornare su **Auto**.
- **CLI:** Passa `--lang <codice>` (es. `tanpopo --lang it --help`) oppure imposta `TANPOPO_LANG`, `LC_ALL`, `LC_MESSAGES`, `LANG`. Nomi di comandi, flag e output machine-readable restano invariati.
- **Errori API e avvisi:** Gli endpoint `/admin/v1/*` rispettano `Accept-Language` per `error.message` e sommari degli avvisi mantenendo stabili `error.code`, `error.type` e schemi JSON.
- **Funzionamento offline:** Tutte le 15 lingue, i font e le icone sono inclusi nel binario, con zero richieste a CDN esterni.

---

## 3. Flussi principali della console

### Panoramica e Tracce
- **Panoramica** mostra richieste live, tasso di errore, overhead proxy p95, spesa in micro-USD, riserve attive, deployment con circuito aperto e avvisi attivi con link di rimedio diretto.
- **Tracce** raccoglie solo metadati di esecuzione (nessun prompt o risposta). Clicca su una richiesta per vedere la cascata per fasi e la precisione (`Esatto`, `Stimato` o `Non misurato`).

### Provider, Modelli e Routing
- **Provider:** Sigilla chiavi API, credenziali AWS IAM, segreti HMAC o certificati TLS nel vault in sola scrittura. Associa gli adattatori ed esegui verifiche live.
- **Modelli e deployment:** Dichiara modelli logici con capacità e listini interi esatti in **nano-USD**.
- **Rotte e policy:** Configura routing deterministico (`Failover per priorità`, `Pesato`, `Round robin pesato`, `Costo minimo`, `Latenza minima (EWMA)`). Usa **Simula rotta** in dry-run senza traffico in uscita. Le policy gerarchiche possono solo restringere i limiti ereditati.

### Estensioni, Hub MCP e Playground
- **Estensioni:** Prepara pacchetti dal Marketplace offline o tramite URI + SHA-256 di 64 caratteri. Esamina le differenze di permessi prima dell'attivazione e metti subito in quarantena pacchetti compromessi.
- **Hub MCP:** Registra server Streamable HTTP, sincronizza strumenti, fissa gli hash di schema RFC 8785 JCS, imposta i livelli di rischio (`read_only`, `mutating`, `destructive`) e concedi l'uso per chiave virtuale.
- **Playground:** Testa prompt con una chiave effimera con tetto di budget e copia snippet pronti in cURL, Python e TypeScript.

### Accesso, FinOps, Sicurezza e Operazioni
- **Accesso e chiavi:** Emetti chiavi virtuali per ambiente, ruotale con 24 ore di grazia o revocale con svuotamento graduale o interruzione d'emergenza. Gestisci i 6 ruoli RBAC.
- **FinOps:** Monitora spesa esatta, stimata, riserve attive e richieste non misurate; imposta tetti in micro-USD ed esporta CSV/JSON.
- **Sicurezza:** Decidi le approvazioni HITL per chiamate MCP distruttive, gestisci i blocchi d'emergenza a 4 livelli e verifica la catena hash crittografica RFC 8785 JCS.
- **Operazioni (ruolo `OPERATOR`):** Controlla `/readyz` e le epoche del cluster, crea backup cifrati `.tpop.enc`, esegui pre-verifiche di aggiornamento e registra webhook firmati HMAC.

---

## 4. Riepilogo comandi CLI

| Comando | Scopo |
| --- | --- |
| `tanpopo serve` | Avvia il server Tanpopo (`--role all`, `control-plane` o `data-plane`). |
| `tanpopo doctor` | Verifica file locali, database e stato del servizio (`--output json`). |
| `tanpopo support bundle` | Crea archivio diagnostico ripulito (senza segreti né prompt). |
| `tanpopo admin reset-owner` | Reimposta offline la password del proprietario locale. |
| `tanpopo backup create / verify / restore` | Crea, verifica e ripristina backup cifrati `.tpop.enc`. |
| `tanpopo plugin init / validate / test / pack` | Sviluppa, testa offline e impacchetta estensioni `.tpkg`. |
| `tanpopo upgrade preflight` | Valida compatibilità ABI prima di aggiornare Tanpopo Core. |
| `tanpopo key / route / budget / audit / killswitch` | Gestisce risorse remote del control plane via `/admin/v1`. |
