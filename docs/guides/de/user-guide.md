# Tanpopo Benutzerhandbuch

**Tanpopo** ist ein selbst gehostetes Einzelbinär-KI-Gateway und Model-Context-Protocol-(MCP)-Hub für deterministisches Routing, Write-only-Zugangsdatensicherheit, exakte Mikro-USD-FinOps-Abrechnung und offlinefähigen Betrieb.

---

## 1. Schnellstart & Ersteinrichtung

1. Starten Sie den Tanpopo-Server:
   ```bash
   tanpopo serve --role all --listen 127.0.0.1:8080
   ```
2. Beim ersten Start gibt Tanpopo ein einmaliges **Bootstrap-Token** im Server-Protokoll aus.
3. Öffnen Sie `http://127.0.0.1:8080/setup` im Browser und schließen Sie den 4-stufigen Einrichtungsassistenten ab:
   - **Eigentümerkonto:** Geben Sie das Bootstrap-Token, die Administrator-E-Mail, das Passwort (mindestens 12 Zeichen), den Organisationsnamen und den ersten Projektnamen ein.
   - **Erste Verbindung (optional):** Binden Sie eine kommerzielle API (OpenAI, Anthropic, Google Gemini oder OpenAI-kompatibel) oder eine lokale Instanz (vLLM, Ollama) an. Lokale Endpunkte erfordern die ausdrückliche Freigabe privater Netzwerke sowie exakte CIDRs ohne Platzhalter (z. B. `127.0.0.1/32`).
   - **Schlüsselverwahrung:** Bestätigen Sie die Sicherung des Instanz-Master-Schlüssels (Master KEK). Bei Verlust sind verschlüsselte Tresor-Geheimnisse nicht wiederherstellbar.
   - **Prüfen & initialisieren:** Schließen Sie die Einrichtung ab, sichern Sie Ihren einmalig angezeigten virtuellen Schlüssel und verifizieren Sie die erste Streaming-Anfrage.

---

## 2. Sprache & Darstellung

- **Web-Konsole:** Verwenden Sie die Sprachauswahl in der oberen Leiste (sowie auf den Seiten Einrichtung und Anmeldung). **Auto** ist voreingestellt und wählt die erste unterstützte Sprache aus Ihren Browsereinstellungen (Fallback: English). Manuelle Auswahlen bleiben über Neuladen hinweg erhalten; eine Rückkehr zu **Auto** ist jederzeit möglich.
- **CLI:** Übergeben Sie `--lang <code>` (z. B. `tanpopo --lang de --help`) oder nutzen Sie `TANPOPO_LANG`, `LC_ALL`, `LC_MESSAGES`, `LANG`. Befehlsnamen, Flags und maschinenlesbare Ausgaben bleiben unverändert.
- **API-Fehler & Alarme:** Endpunkte unter `/admin/v1/*` werten den Header `Accept-Language` für menschenlesbare `error.message`-Texte und Alarmzusammenfassungen aus, während `error.code`, `error.type` und JSON-Schemas stabil bleiben.
- **Offline-Betrieb:** Alle 15 Sprachen, Schriftarten und Icons sind in der Binärdatei gebündelt – null externe CDN-Anfragen zur Laufzeit.

---

## 3. Zentrale Konsolen-Workflows

### Übersicht & Traces
- **Übersicht** zeigt Live-Anfragen, Fehlerrate, p95-Proxy-Overhead, abgerechnete Mikro-USD-Kosten, aktive Reservierungen, Bereitstellungen mit offenem Trennschalter und aktive Alarme mit direkten Behebungslinks.
- **Traces** erfasst reine Metadaten-Telemetrie (keine Prompts oder Antworten). Klicken Sie auf eine Anfrage, um den Phasen-Wasserfall und die Messgenauigkeit (`Exakt`, `Geschätzt` oder `Ungemessen`) zu prüfen.

### Anbieter, Modelle & Routing
- **Anbieter:** Versiegeln Sie API-Schlüssel, AWS-IAM-Zugangsdaten, Webhook-HMAC-Geheimnisse oder TLS-Zertifikate im Write-only-Tresor. Verknüpfen Sie Adapter mit Upstream-Endpunkten und führen Sie Live-Tests durch.
- **Modelle & Bereitstellungen:** Deklarieren Sie logische Modelle mit ihren Fähigkeiten und verknüpfen Sie sie mit exakten ganzzahligen **Nano-USD**-Preistabellen.
- **Routen & Richtlinien:** Konfigurieren Sie deterministisches Routing (`Prioritäts-Failover`, `Gewichtet`, `Gewichtetes Round-Robin`, `Niedrigste Kosten`, `Niedrigste Latenz (EWMA)`). Nutzen Sie die **Routen-Analyse** im Dry-Run-Modus ohne Upstream-Egress. Hierarchische Richtlinien auf Organisations-, Projekt- und Umgebungsebene können übergeordnete Vorgaben ausschließlich verschärfen.

### Erweiterungen, MCP-Hub & Playground
- **Erweiterungen:** Merken Sie Pakete aus dem Offline-Marktplatz oder über eine Hash-fixierte URI + 64-stelligen SHA-256 vor. Prüfen Sie Berechtigungsänderungen vor der Aktivierung und stellen Sie kompromittierte Pakete sofort unter Quarantäne.
- **MCP-Hub:** Registrieren Sie Streamable-HTTP-MCP-Server, synchronisieren Sie Tools, fixieren Sie RFC-8785-JCS-Schema-Hashes, vergeben Sie Risikostufen (`read_only`, `mutating`, `destructive`) und erteilen Sie Freigaben pro virtuellem Schlüssel.
- **Playground:** Testen Sie Prompts interaktiv mit einem kurzlebigen, budgetbegrenzten Sitzungsschlüssel und kopieren Sie fertige cURL-, Python- und TypeScript-Beispiele.

### Zugriff, FinOps, Sicherheit & Betrieb
- **Zugriff & Schlüssel:** Stellen Sie umgebungsgebundene virtuelle Schlüssel aus, rotieren Sie sie mit 24-Stunden-Übergangsfrist oder widerrufen Sie sie sanft bzw. per Notfall-Abbruch. Verwalten Sie die 6 RBAC-Rollen.
- **FinOps:** Überwachen Sie exakte und geschätzte Kosten, aktive Reservierungen und ungemessene Anfragen, legen Sie Mikro-USD-Budgetlimits fest und exportieren Sie CSV-/JSON-Berichte.
- **Sicherheit:** Entscheiden Sie HITL-Genehmigungen für destruktive MCP-Aufrufe, lösen Sie 4-stufige Not-Aus-Schalter aus oder heben Sie sie auf und verifizieren Sie die RFC-8785-JCS-Audit-Hash-Kette.
- **Betrieb (`OPERATOR`-Rolle):** Prüfen Sie `/readyz` und Cluster-Epochen, erstellen Sie verschlüsselte `.tpop.enc`-Backups, führen Sie Upgrade-Vorprüfungen durch und verwalten Sie HMAC-signierte Webhooks.

---

## 4. CLI-Kurzreferenz

| Befehl | Zweck |
| --- | --- |
| `tanpopo serve` | Tanpopo-Server starten (`--role all`, `control-plane` oder `data-plane`). |
| `tanpopo doctor` | Lokale Dateien, Datenbank und Dienstzustand prüfen (`--output json`). |
| `tanpopo support bundle` | Bereinigtes Diagnosearchiv erstellen (ohne Klartext-Geheimnisse oder Prompts). |
| `tanpopo admin reset-owner` | Passwort des lokalen Organisationseigentümers offline zurücksetzen. |
| `tanpopo backup create / verify / restore` | Verschlüsselte `.tpop.enc`-Backups erstellen, prüfen und wiederherstellen. |
| `tanpopo plugin init / validate / test / pack` | Erweiterungen entwickeln, offline testen und als `.tpkg` packen. |
| `tanpopo upgrade preflight` | ABI-Kompatibilität vor Core-Upgrades prüfen. |
| `tanpopo key / route / budget / audit / killswitch` | Remote-Steuerebenen-Ressourcen über `/admin/v1` abfragen. |
