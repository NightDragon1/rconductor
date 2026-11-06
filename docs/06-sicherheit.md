# Bedrohungsmodell

Dokumentversion 2.7, freigegeben; Änderungen gegenüber 2.6 siehe Gesamtkonzept, Kapitel 16, gegenüber 2.5 Kapitel 15, gegenüber 2.4 Kapitel 14, gegenüber 2.3 Kapitel 13, gegenüber 2.2 Kapitel 12, gegenüber 2.1 Kapitel 11, gegenüber 2.0 Kapitel 10. STRIDE über die Bausteine und Verbindungen aus dem Tab Architektur. Größtes Einzelrisiko ist der Agent: Wer den Docker-Socket kontrolliert, hat praktisch Root auf dem Spiele-Host. Die meisten Bedrohungen sind durch bestehende Anforderungen abgedeckt; sechs neue Maßnahmen (SEC-006 bis SEC-011) schließen die Lücken; mit Version 2.1 kommt SEC-013 (Lieferkette) hinzu.

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
| T-001 | Spoofing | Diebstahl oder Fixierung einer Session | Serverseitige Sessions, Cookie HttpOnly und SameSite=Lax, über HTTPS Secure mit Präfix \_\_Host-, neue Session-ID nach Anmeldung (Design 2.1, D-043) | abgedeckt; ohne TLS-Proxy Restrisiko im LAN (SEC-001) |
| T-002 | Spoofing | Passwort-Raten | Argon2id, Login-Schutz, auch bei parallelen Versuchen und je IPv6-/64; Anmeldungen des Besitzers setzen die Zähler anderer Clients nicht zurück (AUTH-001, SEC-004, Design 3.7) | abgedeckt |
| T-003 | Spoofing | Fremder Agent gibt sich als gekoppelter Agent aus | Gegenseitig gepinnte Ed25519-Schlüssel, Widerruf (AGT-012) | abgedeckt |
| T-004 | Spoofing | Angreifer fängt den ersten Kontakt bei Weg B ab (Trust on First Use) | Weg B standardmäßig aus, Fingerprint-Abgleich (D-024) | Restrisiko bei aktivem Weg B |
| T-005 | Tampering | Befehlsinjektion über Zeilenumbrüche oder Steuerzeichen | Bereinigung vor dem Senden (CMD-006) | abgedeckt |
| T-006 | Tampering | Umgehung der Befehlsliste über Wrapper oder Präfix-Tricks | Token-Pfad-Abgleich, Wrapper immer gesperrt (CMD-004, D-026); für Moderator und Supporter zusätzlich die Prüfung der Katalogparameter mit Sperre von Selektoren wie @a und die Ablehnung von Befehlstrennern des Spielprofils (Design 3.3, Schritt 7b). Eigene Profile müssen syntax.separators selbst setzen | abgedeckt |
| T-007 | Tampering | Cross-Site Request Forgery | SameSite=Lax, Origin-Prüfung bei allen schreibenden Anfragen und beim WebSocket (Design 2.1) | abgedeckt |
| T-008 | Tampering | Schadcode über Spielernamen, Chat, RCON-Antworten oder Agent-Daten in der Oberfläche (gespeichertes XSS) | Alle Fremddaten escaped darstellen, Farbcodes in R1 entfernen, Bidi-Zeichen im Freitext als Platzhalter (SEC-006); Content-Security-Policy (SEC-009) | abgedeckt |
| T-009 | Tampering | Verändern des Audit-Logs durch jemanden mit Datenbankzugriff | Nur durch Zugriff auf Host oder Volume möglich; über die Oberfläche höchstens Verkürzung der Aufbewahrung auf 30 Tage; manipulationssichere Verkettung im Backlog (SEC-011) | Restrisiko |
| T-010 | Repudiation | Benutzer bestreitet einen Befehl | Audit mit Request-ID, Session, Rolle, auch für Ablehnungen (AUD-001, CMD-007) | abgedeckt |
| T-011 | Information Disclosure | Auslesen von RCON-Passwörtern aus Datenbank oder Backup | AES-256-GCM, Master-Key getrennt, nicht im Backup (SRV-002, SEC-005); neue Adresse nur mit erneuter Passworteingabe | abgedeckt |
| T-012 | Information Disclosure | Mitlesen des RCON-Passworts im Netz (RCON ist unverschlüsselt) | RCON nur im internen Netz (SEC-001) | Restrisiko |
| T-013 | Information Disclosure | Server außerhalb des Scopes werden sichtbar | 404 statt 403 (Design 3.1) | abgedeckt |
| T-014 | Information Disclosure | Geheimnisse in Container-Logs | Logs nur für RCON- und Server-Admin, nur bei offener Ansicht (UI-009, AGT-008) | abgedeckt |
| T-015 | Information Disclosure | Kopplungs-Token oder Initialpasswort in docker inspect bzw. Container-Log | Token einmalig und 24 h gültig, \_FILE-Variante (D-033); Initialpasswort nach Änderung ungültig (AUTH-007) | abgedeckt |
| T-016 | Denial of Service | Befehlsflut gegen einen Spieleserver | Ratenlimit (CMD-019), Warteschlange je Server (PROT-002); ein gesperrter Client füllt das Audit-Log nicht | abgedeckt |
| T-017 | Denial of Service | Übergroße Nachrichten oder Verbindungsflut am Agent-Port | Größenlimits und Handshake-Rate am Agent-Port, begrenzte wartende Kopplungen, Leerlauf ohne Session beendet die Verbindung (SEC-007) | abgedeckt |
| T-018 | Denial of Service | Viele WebSockets oder Abos je Session | Begrenzung der Sockets je Session und der Abos je Socket (SEC-008) | abgedeckt |
| T-019 | Denial of Service | Regex mit hoher Laufzeit im Panel | RE2, Zeilenlimit, Timeout (SEC-003) | abgedeckt |
| T-020 | Elevation of Privilege | Rolle oder Scope über die API erweitern | Rechteprüfung nur im Backend, Zuweisungen nur als Teilmenge (ROLE-008, Design 3.6) | abgedeckt |
| T-021 | Elevation of Privilege | Kompromittiertes Panel übernimmt den Host über den Agent | Agent erlaubt nur die Operationen aus Design 1.5 und nur gelabelte Container (AGT-002, AGT-004) | abgedeckt |
| T-022 | Elevation of Privilege | Kompromittierter Agent übernimmt den Host | Agent minimal, distroless, als Nicht-Root ohne offenen Port; zusätzlicher Socket-Proxy vor dem Docker-Socket als Empfehlung (SEC-010), der nur Auflisten, Inspizieren, Ereignisse, Logs, Statistiken, start, stop und restart durchlässt. Zugriff auf den Docker-Socket bleibt root-äquivalent. Beim empfohlenen Proxy erlaubt ALLOW\_RESTARTS auch kill; der Agent ruft es nie auf, ein kompromittierter Agent könnte es, gleichwertig zu stop | Restrisiko, gemindert |
| T-023 | Elevation of Privilege | Kompromittierter Agent greift das Panel an | Agent-Daten gelten als Fremddaten (SEC-006, SEC-007); Agent erhält nur Aufträge, keine Daten anderer Hosts | abgedeckt |
| T-025 | Denial of Service | Speicher wird ohne Anmeldung erschöpft: über Argon2id (64 MiB je Berechnung) oder große Request-Bodys | Begrenzung gleichzeitiger Berechnungen, Login-Schutz, Einladungs-Token vor der Passwortarbeit geprüft (Design 3.7), Bodys höchstens 256 KiB, Profilbild 2 MiB, gemerkte Pseudonyme begrenzt (Design 2.1), offene Verbindungen am Web-Port begrenzt (RCD\_MAX\_CONNECTIONS, Design 5.2); Speichergrenze im Compose | abgedeckt |
| T-026 | Tampering | Manipulierte Abhängigkeit oder Image in der Lieferkette | Signierte Images mit SBOM, govulncheck, gepinnte Actions und Basis-Images (SEC-013) | abgedeckt |
| T-024 | Information Disclosure | Geheimnisse in den Logs von Panel und Agent, die über Portainer, Log-Sammler oder MCP von Dritten gelesen werden | Schwärzung auf allen Log-Levels, Sessions nur als Hash, Abnahme über Logs nur gegen Testinstanzen (SEC-012, Design 8.1) | abgedeckt |

## 4. Neue Maßnahmen

| ID | Maßnahme | Prio | Target |
| --- | --- | --- | --- |
| SEC-006 | Alle Daten von Spieleservern und Agents (Antworten, Spielernamen, Logs, Versionen, Container-Namen) gelten als Fremddaten: escaped dargestellt, Größe begrenzt, Farbcodes in R1 entfernt (feste Liste erlaubter Stile mit CMD-017, R2), Bidi-Steuerzeichen im Freitext als Platzhalter gezeigt; Steuer- und Formatzeichen (Cc, Cf) aus Bezeichnern entfernt, damit Bidi-Zeichen einen Namen nicht als anderen erscheinen lassen | Muss | R1 |
| SEC-007 | Agent-Port: Höchstgröße je Nachricht, Begrenzung der Logzeilen je Nachricht, Rate für Verbindungsaufbau je Quelladresse, Obergrenze für wartende Kopplungen | Muss | R1 |
| SEC-008 | Höchstzahl an WebSocket-Verbindungen je Session und an Abos je Verbindung | Soll | R1 |
| SEC-009 | Sicherheitsheader auf jeder Antwort des Web-Ports: Content-Security-Policy ohne Inline-Skripte, X-Content-Type-Options, Referrer-Policy, frame-ancestors none; der Agent-Port liefert keine Inhalte für Browser | Muss | R1 |
| SEC-010 | Doku empfiehlt einen Socket-Proxy vor dem Docker-Socket, der nur die vom Agent benötigten Endpunkte durchlässt; der Agent funktioniert damit unverändert | Kann | R1 |
| SEC-011 | Manipulationssichere Verkettung der Audit-Einträge (Hash-Kette) mit Prüffunktion | Kann | Backlog |
| SEC-013 | Lieferkette: Images für amd64 und arm64 signiert (cosign keyless) mit SBOM (syft); govulncheck als Pflicht-Check; Actions auf Commit-SHA gepinnt; Abhängigkeits-Updates per Renovate ohne ungeprüftes Automerge, Merge durch Claude Code nach grüner CI und Sichtung des Changelogs, Major nur nach Freigabe; Release-Tags erst nach Veröffentlichung des Repositories; Signatur und SBOM gelten für ghcr.io und Docker Hub (PRJ-004) | Muss | R1 |

## 5. Restrisiken

- Wer Root auf dem Spiele-Host oder Zugriff auf den Agent-Container hat, kontrolliert die Container dieses Hosts; der Agent kann das nicht verhindern.
- RCON selbst ist unverschlüsselt; Schutz nur über Netztrennung.
- Wer Zugriff auf Datenbank und /secrets des Panels hat, kann alles lesen und das Audit-Log ändern.
- Bei aktivem Weg B hängt die Sicherheit am Fingerprint-Abgleich durch Menschen.
- Audit-Zeilen für erfundene Namen: Ohne Anmeldung erzeugt jeder Fehlversuch mit einem neuen Namen einen Audit-Eintrag, auch unbekannte Namen zählen wie bekannte (Design 3.7). Gebremst wird das nur durch die begrenzte Passwortberechnung, nicht durch eine Sperre; das Audit-Log kann so wachsen, bis die Aufbewahrung greift.
- Das Verbindungslimit schützt den Speicher, nicht die Erreichbarkeit: Wer ohne Anmeldung alle Verbindungen belegt, lässt neue Besucher warten, bis Zeitgrenzen sie freigeben (10 s ohne Kopf, 120 s im Leerlauf). Bestehende WebSockets laufen weiter. Eine Begrenzung je Adresse gehört an den Reverse Proxy, weil das Panel dahinter nur dessen Adresse sieht.
- Bei vielen gleichzeitigen richtigen Anmeldungen desselben Kontos vom selben Client kann 429 kommen: Laufende Versuche zählen vor ihrem Ergebnis (Design 3.7). Der nächste Versuch nach dem Ende der laufenden gelingt.
- Besitzer und Angreifer hinter derselben Adresse oder demselben IPv6-/64 gelten als ein Client; die Anmeldung des Besitzers löscht dann auch die Fehlversuche des Angreifers.
