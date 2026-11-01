# Bedrohungsmodell

Freigegeben mit Dokumentversion 2.0. STRIDE über die Bausteine und Verbindungen aus dem Tab Architektur. Größtes Einzelrisiko ist der Agent: Wer den Docker-Socket kontrolliert, hat praktisch Root auf dem Spiele-Host. Die meisten Bedrohungen sind durch bestehende Anforderungen abgedeckt; sechs neue Maßnahmen (SEC-006 bis SEC-011) schließen die Lücken.

## 1. Schutzgüter

| Schutzgut | Ort | Schaden bei Verlust |
| --- | --- | --- |
| Docker-Socket des Spiele-Hosts | Agent | Root auf dem Host, alle Container und Daten |
| RCON-Zugangsdaten | Datenbank, verschlüsselt | Volle Kontrolle über Spieleserver, Umgehung aller Befehlslisten |
| Master-Key | /secrets | Entschlüsselung aller RCON-Zugangsdaten |
| Agent- und Panel-Schlüssel | /secrets beider Seiten | Vortäuschen von Panel oder Agent |
| Sessions und Passwörter | Datenbank (gehasht), Browser-Cookie | Handeln im Namen eines Benutzers |
| Audit-Log | Datenbank | Taten nicht mehr nachweisbar |

## 2. Vertrauensgrenzen

| Grenze | Wer misstraut wem | Warum |
| --- | --- | --- |
| Browser → Panel | Panel misstraut jeder Anfrage | Benutzer mit niedriger Rolle können die API direkt aufrufen |
| Agent ↔ Panel | gegenseitig | Host-Betreiber und RCON-Admin können verschiedene Personen sein (D-020); jede Seite kann kompromittiert sein |
| Panel → Spieleserver | Panel misstraut Antworten | Spielernamen, Chat und Antworten stammen von Spielern |
| Agent → Docker | Agent begrenzt sich selbst | Der Socket kennt keine feineren Rechte |

## 3. Bedrohungen und Maßnahmen

| ID | STRIDE | Bedrohung | Maßnahme | Status |
| --- | --- | --- | --- | --- |
| T-001 | Spoofing | Diebstahl oder Fixierung einer Session | Serverseitige Sessions, Cookie HttpOnly, Secure, SameSite=Strict, neue Session-ID nach Anmeldung (Design 2.1) | abgedeckt |
| T-002 | Spoofing | Passwort-Raten | Argon2id, Login-Schutz (AUTH-001, SEC-004) | abgedeckt |
| T-003 | Spoofing | Fremder Agent gibt sich als gekoppelter Agent aus | Gegenseitig gepinnte Ed25519-Schlüssel, Widerruf (AGT-012) | abgedeckt |
| T-004 | Spoofing | Angreifer fängt den ersten Kontakt bei Weg B ab (Trust on First Use) | Weg B standardmäßig aus, Fingerprint-Abgleich (D-024) | Restrisiko bei aktivem Weg B |
| T-005 | Tampering | Befehlsinjektion über Zeilenumbrüche oder Steuerzeichen | Bereinigung vor dem Senden (CMD-006) | abgedeckt |
| T-006 | Tampering | Umgehung der Befehlsliste über Wrapper oder Präfix-Tricks | Token-Pfad-Abgleich, Wrapper immer gesperrt (CMD-004, D-026) | abgedeckt |
| T-007 | Tampering | Cross-Site Request Forgery | SameSite=Strict, Origin-Prüfung bei schreibenden Anfragen und WebSocket (Design 2.1) | abgedeckt |
| T-008 | Tampering | Schadcode über Spielernamen, Chat, RCON-Antworten oder Agent-Daten in der Oberfläche (gespeichertes XSS) | Alle Fremddaten escaped darstellen, Farbcodes nur in erlaubte Stile übersetzen (SEC-006); Content-Security-Policy (SEC-009) | neu |
| T-009 | Tampering | Verändern des Audit-Logs durch jemanden mit Datenbankzugriff | Nur durch Zugriff auf Host oder Volume möglich; manipulationssichere Verkettung im Backlog (SEC-011) | Restrisiko |
| T-010 | Repudiation | Benutzer bestreitet einen Befehl | Audit mit Request-ID, Session, Rolle, auch für Ablehnungen (AUD-001, CMD-007) | abgedeckt |
| T-011 | Information Disclosure | Auslesen von RCON-Passwörtern aus Datenbank oder Backup | AES-256-GCM, Master-Key getrennt, nicht im Backup (SRV-002, SEC-005) | abgedeckt |
| T-012 | Information Disclosure | Mitlesen des RCON-Passworts im Netz (RCON ist unverschlüsselt) | RCON nur im internen Netz (SEC-001) | Restrisiko |
| T-013 | Information Disclosure | Server außerhalb des Scopes werden sichtbar | 404 statt 403 (Design 3.1) | abgedeckt |
| T-014 | Information Disclosure | Geheimnisse in Container-Logs | Logs nur für RCON- und Server-Admin, nur bei offener Ansicht (UI-009, AGT-008) | abgedeckt |
| T-015 | Information Disclosure | Kopplungs-Token oder Initialpasswort in docker inspect bzw. Container-Log | Token einmalig und 24 h gültig, \_FILE-Variante (D-033); Initialpasswort nach Änderung ungültig (AUTH-007) | abgedeckt |
| T-016 | Denial of Service | Befehlsflut gegen einen Spieleserver | Ratenlimit (CMD-019), Warteschlange je Server (PROT-002) | abgedeckt |
| T-017 | Denial of Service | Übergroße Nachrichten oder Verbindungsflut am Agent-Port | Größenlimits und Handshake-Rate am Agent-Port, begrenzte wartende Kopplungen (SEC-007) | neu |
| T-018 | Denial of Service | Viele WebSocket-Abos je Session | Begrenzung der Abos je Session (SEC-008) | neu |
| T-019 | Denial of Service | Regex mit hoher Laufzeit im Agent | RE2, Zeilenlimit, Timeout (SEC-003) | abgedeckt |
| T-020 | Elevation of Privilege | Rolle oder Scope über die API erweitern | Rechteprüfung nur im Backend, Zuweisungen nur als Teilmenge (ROLE-008, Design 3.6) | abgedeckt |
| T-021 | Elevation of Privilege | Kompromittiertes Panel übernimmt den Host über den Agent | Agent erlaubt nur die Operationen aus Design 1.5 und nur gelabelte Container (AGT-002, AGT-004) | abgedeckt |
| T-022 | Elevation of Privilege | Kompromittierter Agent übernimmt den Host | Agent minimal, distroless, ohne offenen Port; zusätzlicher Socket-Proxy vor dem Docker-Socket als Empfehlung (SEC-010) | Restrisiko, gemindert |
| T-023 | Elevation of Privilege | Kompromittierter Agent greift das Panel an | Agent-Daten gelten als Fremddaten (SEC-006, SEC-007); Agent erhält nur Aufträge, keine Daten anderer Hosts | neu |
| T-024 | Information Disclosure | Geheimnisse in den Logs von Panel und Agent, die über Portainer, Log-Sammler oder MCP von Dritten gelesen werden | Schwärzung auf allen Log-Levels, Sessions nur als Hash, Abnahme über Logs nur gegen Testinstanzen (SEC-012, Design 8.1) | neu |

## 4. Neue Maßnahmen

| ID | Maßnahme | Prio | Target |
| --- | --- | --- | --- |
| SEC-006 | Alle Daten von Spieleservern und Agents (Antworten, Spielernamen, Logs, Versionen, Container-Namen) gelten als Fremddaten: escaped dargestellt, Größe begrenzt, Farbcodes nur in eine feste Liste erlaubter Stile übersetzt | Muss | R1 |
| SEC-007 | Agent-Port: Höchstgröße je Nachricht, Begrenzung der Logzeilen je Nachricht, Rate für Verbindungsaufbau je Quelladresse, Obergrenze für wartende Kopplungen | Muss | R1 |
| SEC-008 | Höchstzahl an WebSocket-Verbindungen und Abos je Session | Soll | R1 |
| SEC-009 | Sicherheitsheader: Content-Security-Policy ohne Inline-Skripte, X-Content-Type-Options, Referrer-Policy, frame-ancestors none | Muss | R1 |
| SEC-010 | Doku empfiehlt einen Socket-Proxy vor dem Docker-Socket, der nur die vom Agent benötigten Endpunkte durchlässt; der Agent funktioniert damit unverändert | Kann | R1 |
| SEC-011 | Manipulationssichere Verkettung der Audit-Einträge (Hash-Kette) mit Prüffunktion | Kann | Backlog |

## 5. Restrisiken

- Wer Root auf dem Spiele-Host oder Zugriff auf den Agent-Container hat, kontrolliert die Container dieses Hosts; der Agent kann das nicht verhindern.
- RCON selbst ist unverschlüsselt; Schutz nur über Netztrennung.
- Wer Zugriff auf Datenbank und /secrets des Panels hat, kann alles lesen und das Audit-Log ändern.
- Bei aktivem Weg B hängt die Sicherheit am Fingerprint-Abgleich durch Menschen.
