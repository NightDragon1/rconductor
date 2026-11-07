# Bedrohungsmodell

Dokumentversion 2.8, freigegeben; Änderungen gegenüber 2.7 siehe Gesamtkonzept, Kapitel 17, gegenüber 2.6 Kapitel 16, gegenüber 2.5 Kapitel 15, gegenüber 2.4 Kapitel 14, gegenüber 2.3 Kapitel 13, gegenüber 2.2 Kapitel 12, gegenüber 2.1 Kapitel 11, gegenüber 2.0 Kapitel 10. STRIDE über die Bausteine und Verbindungen aus dem Tab Architektur. Größtes Einzelrisiko ist der Agent: Wer den Docker-Socket kontrolliert, hat praktisch Root auf dem Spiele-Host. Die meisten Bedrohungen sind durch bestehende Anforderungen abgedeckt; sechs neue Maßnahmen (SEC-006 bis SEC-011) schließen die Lücken; mit Version 2.1 kommt SEC-013 (Lieferkette) hinzu.

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
| T-002 | Spoofing | Passwort-Raten | Argon2id, Login-Schutz, auch bei parallelen Versuchen und je IPv6-/64; Anmeldungen des Besitzers setzen die Zähler anderer Clients nicht zurück, ein Client ist über alle Namen begrenzt, laufende Zähler werden nie verdrängt, eine volle Tabelle zählt in einem Überlauf-Zähler fester Größe und lässt die Anmeldung langsam offen (AUTH-001, SEC-004, Design 3.7) | abgedeckt für eine Adresse; R-11, R-12 |
| T-003 | Spoofing | Fremder Agent gibt sich als gekoppelter Agent aus | Gegenseitig gepinnte Ed25519-Schlüssel, Widerruf (AGT-012) | abgedeckt |
| T-004 | Spoofing | Angreifer fängt den ersten Kontakt bei Weg B ab (Trust on First Use) | Weg B standardmäßig aus, Fingerprint-Abgleich (D-024) | Restrisiko bei aktivem Weg B |
| T-005 | Tampering | Befehlsinjektion über Zeilenumbrüche oder Steuerzeichen | Bereinigung vor dem Senden (CMD-006) | abgedeckt |
| T-006 | Tampering | Umgehung der Befehlsliste über Wrapper oder Präfix-Tricks | Token-Pfad-Abgleich, Wrapper immer gesperrt (CMD-004, D-026); für Moderator und Supporter zusätzlich die Prüfung der Katalogparameter mit Sperre von Selektoren wie @a und die Ablehnung von Befehlstrennern des Spielprofils (Design 3.3, Schritt 7b). Eigene Profile müssen syntax.separators selbst setzen | abgedeckt |
| T-007 | Tampering | Cross-Site Request Forgery | SameSite=Lax, Origin-Prüfung bei allen schreibenden Anfragen und beim WebSocket (Design 2.1) | abgedeckt |
| T-008 | Tampering | Schadcode über Spielernamen, Chat, RCON-Antworten oder Agent-Daten in der Oberfläche (gespeichertes XSS) | Alle Fremddaten escaped darstellen, Farbcodes in R1 entfernen, Bidi-Zeichen im Freitext als Platzhalter (SEC-006); Content-Security-Policy (SEC-009) | abgedeckt |
| T-009 | Tampering | Verändern des Audit-Logs durch jemanden mit Datenbankzugriff | Nur durch Zugriff auf Host oder Volume möglich; über die Oberfläche höchstens Verkürzung der Aufbewahrung auf 30 Tage; manipulationssichere Verkettung im Backlog (SEC-011) | Restrisiko |
| T-010 | Repudiation | Benutzer bestreitet einen Befehl | Audit mit Request-ID, Session, Rolle, auch für Ablehnungen (AUD-001, CMD-007) | abgedeckt |
| T-011 | Information Disclosure | Auslesen von RCON-Passwörtern aus Datenbank oder Backup | AES-256-GCM, Master-Key getrennt, nicht im Backup (SRV-002, SEC-005); neue Adresse nur mit erneuter Passworteingabe, auch beim Verbindungstest (D-049) | abgedeckt |
| T-012 | Information Disclosure | Mitlesen des RCON-Passworts im Netz (RCON ist unverschlüsselt) | RCON nur im internen Netz (SEC-001) | Restrisiko |
| T-013 | Information Disclosure | Server außerhalb des Scopes werden sichtbar | 404 statt 403 (Design 3.1) | abgedeckt |
| T-014 | Information Disclosure | Geheimnisse in Container-Logs | Logs nur für RCON- und Server-Admin, nur bei offener Ansicht (UI-009, AGT-008) | abgedeckt |
| T-015 | Information Disclosure | Kopplungs-Token oder Initialpasswort in docker inspect bzw. Container-Log | Token einmalig und 24 h gültig, \_FILE-Variante (D-033); Initialpasswort nach Änderung ungültig (AUTH-007) | abgedeckt |
| T-016 | Denial of Service | Befehlsflut gegen einen Spieleserver | Ratenlimit (CMD-019), Warteschlange je Server (PROT-002); ein gesperrter Client füllt das Audit-Log nicht, je Client und Sperrfenster sind die Audit-Einträge begrenzt; Einträge gehen unter Last nicht verloren | abgedeckt; R-05 |
| T-017 | Denial of Service | Übergroße Nachrichten oder Verbindungsflut am Agent-Port | Größenlimits und Handshake-Rate am Agent-Port, je IPv6-/64 gezählt, Verbindungen insgesamt und je Client begrenzt, begrenzte wartende Kopplungen, ohne Agent-Session endet die Verbindung mit der ersten Antwort (SEC-007) | abgedeckt |
| T-018 | Denial of Service | Viele WebSockets oder Abos je Session | Begrenzung der Sockets je Session und der Abos je Socket (SEC-008) | abgedeckt |
| T-019 | Denial of Service | Regex mit hoher Laufzeit im Panel | RE2, Zeilenlimit, Timeout (SEC-003) | abgedeckt |
| T-020 | Elevation of Privilege | Rolle oder Scope über die API erweitern | Rechteprüfung nur im Backend, Zuweisungen nur als Teilmenge (ROLE-008, Design 3.6) | abgedeckt |
| T-021 | Elevation of Privilege | Kompromittiertes Panel übernimmt den Host über den Agent | Agent erlaubt nur die Operationen aus Design 1.5 und nur gelabelte Container (AGT-002, AGT-004) | abgedeckt |
| T-022 | Elevation of Privilege | Kompromittierter Agent übernimmt den Host | Agent minimal, distroless, als Nicht-Root ohne offenen Port; zusätzlicher Socket-Proxy vor dem Docker-Socket als Empfehlung (SEC-010), der nur Auflisten, Inspizieren, Ereignisse, Logs, Statistiken, start, stop und restart durchlässt. Zugriff auf den Docker-Socket bleibt root-äquivalent. Beim empfohlenen Proxy erlaubt ALLOW\_RESTARTS auch kill; der Agent ruft es nie auf, ein kompromittierter Agent könnte es, gleichwertig zu stop | Restrisiko, gemindert |
| T-023 | Elevation of Privilege | Kompromittierter Agent greift das Panel an | Agent-Daten gelten als Fremddaten (SEC-006, SEC-007); Agent erhält nur Aufträge, keine Daten anderer Hosts | abgedeckt |
| T-025 | Denial of Service | Speicher wird erschöpft oder Plätze werden gehalten, ohne Anmeldung oder mit einer Session jeder Rolle: über Argon2id (64 MiB je Berechnung), große Request-Bodys oder viele Verbindungen | Begrenzung gleichzeitiger Berechnungen, Login-Schutz, Einladungs-Token vor der Passwortarbeit geprüft (Design 3.7), Bodys höchstens 256 KiB, ohne Session 16 KiB, Profilbild 2 MiB und beim Dekodieren nach Pixeln begrenzt, schreibende Anfragen ohne Session werden vor dem Lesen des Bodys abgelehnt, gemerkte Pseudonyme begrenzt (Design 2.1), offene Verbindungen am Web-Port insgesamt und je Client begrenzt, Schreibfrist 60 s, Verbindungen ohne Session enden nach 30 s (RCD\_MAX\_CONNECTIONS, RCD\_MAX\_CLIENT\_CONNECTIONS, RCD\_MAX\_ANONYMOUS\_CONNECTION\_SECONDS, Design 5.2), Bodys in Arbeit unabhängig von der Rolle begrenzt, Verbindungen zur Datenbank begrenzt (Design 2.1); GOMEMLIMIT 80 % der Speichergrenze, Speichergrenze im Compose | abgedeckt, gemessen; R-06, R-07 |
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

Ein Restrisiko gilt erst mit Messung oder der Begründung, warum es nicht messbar ist, und nach ausdrücklicher Freigabe durch den Projektinhaber (Kapitel 6). Die Ausnahme bei einer Datenbanksperre über 2 min ist kein Restrisiko, sie gehört zu AUD-001.

| ID | Risiko | Messung | Widerspruch zu einer Anforderung | Freigabe |
| --- | --- | --- | --- | --- |
| R-01 | Wer Root auf dem Spiele-Host oder Zugriff auf den Agent-Container hat, kontrolliert die Container dieses Hosts; der Agent kann das nicht verhindern. | Nicht messbar, folgt aus dem Aufbau | Nein | freigegeben 06.11.2026 |
| R-02 | RCON selbst ist unverschlüsselt; Schutz nur über Netztrennung (T-012). | Nicht messbar, folgt aus dem Protokoll | Nein | freigegeben 06.11.2026 |
| R-03 | Wer Zugriff auf Datenbank und /secrets des Panels hat, kann alles lesen und das Audit-Log ändern (T-009). | Nicht messbar, folgt aus dem Aufbau | Nein | freigegeben 06.11.2026 |
| R-04 | Bei aktivem Weg B hängt die Sicherheit am Fingerprint-Abgleich durch Menschen (T-004). | Nicht messbar | Nein | freigegeben 06.11.2026 |
| R-05 | Audit-Zeilen für erfundene Namen: Ohne Anmeldung erzeugt jeder Fehlversuch einen Eintrag. Je Client und Sperrfenster ist das begrenzt, viele Adressen zusammen erzeugen entsprechend mehr. | Höchstens 72 Einträge je Client und Fenster bei Standardwerten: Test TestR1B3AuditRowsPerClientAreBounded, TestR1B1MadeUpNamesGiveNoAttempts | Nein | freigegeben 06.11.2026 |
| R-06 | Das Verbindungslimit schützt den Speicher, die Erreichbarkeit nur begrenzt: Viele Adressen zusammen können alle Plätze belegen, wenn sie sofort neu verbinden; neue Besucher warten, bestehende WebSockets laufen weiter. Hinter einem Reverse Proxy gehört die Begrenzung je Adresse an den Proxy, dessen Adresse altert nicht. | 4 Adressen, je 128 Verbindungen, sofortiges Neuverbinden, 180 s: 90 von 90 Besuchern bedient, Median 10 s, Maximum 30 s. Wiederholbar: make measure-panel MODE=hold SECONDS=180; Test TestV2ConnectionWithoutSessionAges | Nein | freigegeben 06.11.2026 |
| R-07 | Wer eine Session hat, kann mit vielen großen Anfragen die Budgets der Bodys und Bilder belegen: Schreibende Anfragen anderer warten dann bis zu 5 s und erhalten 503. Das Panel bleibt in der Speichergrenze, aber nur mit GOMEMLIMIT. | Echtes Binary, 1 GiB, Session der Rolle Supporter. Alles zusammen (240 Worker Bodys, 64 teuerstes Bild, 240 Bild-Abrufe, 64 Anmeldungen), 180 s: Spitze 884 MiB, kein OOM, 90 von 90 Besuchern bedient, Median 56 ms, Maximum 7,2 s. Ohne GOMEMLIMIT (GOMEMLIMIT=off) dieselbe Last: Panel nach wenigen Sekunden vom Kernel beendet. Einzeln: Bilder 597 MiB, Bild-Abrufe 233 MiB, Bodys 467 MiB. Wiederholbar: make measure-panel MODE=all SECONDS=180, MODE=picture, read, body. Tests: TestV1BodiesStayWithinMemory, TestR4B1PictureBombStaysWithinMemory, TestR5B11ProgressiveJPEGStaysWithinItsWeight, TestR5B12PictureReadsStayWithinThePool. RCD\_PASSWORD\_HASH\_CONCURRENCY über 2 ist nicht gemessen und deshalb nicht zulässig: TestHashConcurrencyStaysMeasured | Nein | freigegeben 06.11.2026 mit Auflage: RCD\_PASSWORD\_HASH\_CONCURRENCY höchstens 2, bis eine Messung mit höheren Werten vorliegt |
| R-08 | Bei vielen gleichzeitigen richtigen Anmeldungen desselben Kontos vom selben Client kann 429 kommen: Laufende Versuche zählen vor ihrem Ergebnis (Design 3.7). Der nächste Versuch nach dem Ende der laufenden gelingt. | Test TestParallelBurstStaysUnderTheLimit | Nein | freigegeben 06.11.2026 |
| R-09 | Besitzer und Angreifer hinter derselben Adresse oder demselben IPv6-/64 gelten als ein Client; die Anmeldung des Besitzers löscht dann auch die Fehlversuche des Angreifers für dieses Konto. Der Zähler über alle Namen bleibt. | Test TestLoginKeepsTheClientCount für den Zähler über alle Namen; der Grundfall ist nicht messbar, er folgt aus dem Aufbau | Nein | freigegeben 06.11.2026 |
| R-10 | Die Sperre je Client trifft alle hinter derselben Adresse: Hinter NAT, im gemeinsamen IPv6-/64 oder hinter einem Proxy ohne Eintrag in den vertrauenswürdigen Proxys teilen sich alle die login.max\_client\_failures Fehlversuche. Erreicht einer die Grenze, kann sich dort für die Sperrdauer niemand neu anmelden; bestehende Sessions bleiben. Für große gemeinsame Adressen ist der Wert anzuheben. | Test TestClientLimit | Nein | freigegeben 06.11.2026 |
| R-11 | Viele Adressen zusammen umgehen die Grenze je Client: Jede Adresse hat ihre eigenen Versuche, je Konto bleiben es login.max\_failures je Adresse. | Nicht als Messung: Rechnung, 100 Adressen ergeben 500 Versuche je Konto und Fenster; die Grenze je Adresse belegt TestClientLimit | Nein | freigegeben 06.11.2026 für 0.9; seit #162 wächst die Verzögerung je Konto über alle Adressen bis 30 s, das verlangsamt die Versuche, senkt aber nicht ihre Zahl je Fenster; Freigabe für 1.0 offen |
| R-12 | Bei voller Zähltabelle warten noch nicht gezählte Clients in einer gemeinsamen Schlange (Design 3.7), statt sofort angemeldet zu werden. Die Tabelle fasst 100 000 Einträge, je Adresse bis zu 197 am Rand eines Fensters: Sie füllt sich ab rund 510 Adressen und nur, wenn mindestens 111 Einträge je Sekunde über ein ganzes Sperrfenster entstehen. | Speicher: 19 MiB bei 100 000 Einträgen, 26 MiB mit Namen von 256 Byte, dazu 4 MiB für den Überlauf-Zähler. Wartezeit des Besitzers, Angreifer ohne Zähler versuchen es je Adresse einmal je Sekunde, Echtzeit, erster Versuch: 100 Adressen 7 s, 300 Adressen 63 s, 1000 Adressen 4 min 10 s, rund 0,25 s je Adresse und Fenster; danach 0 bis 3 s. Angreifer mit Client-Zähler (Simulation, bis 1000 Adressen mit 5 Anfragen je Sekunde): Median unter 0,3 s, Maximum 0,5 s. Angreifer, die vor ihrem Zug auflegen, bleiben vorn: 40 Adressen, Median 12,5 s, Maximum 30,6 s (#161). Angreifer je Adresse höchstens 5 Versuche je Sperrfenster, ein Eintrag wirkt ein bis zwei Fenster. Bei RCD\_PASSWORD\_HASH\_CONCURRENCY 2 wurden 23 Fehlversuche je Sekunde gemessen, das füllt die Tabelle nach Rechnung nicht. Wiederholbar: make measure-login; kurz: TestR4B2OwnerGetsThroughCountedAttackers, TestR5B21SketchWindowHolds, TestR5B23EntriesPerAddress | Nein | freigegeben 06.11.2026 |
| R-13 | Ohne TLS-Proxy laufen Sessions im LAN ungeschützt (T-001). | Nicht messbar, folgt aus dem Aufbau | Nein | freigegeben 06.11.2026 |

## 6. Schwere und Restrisiken

Verfügbarkeit, nach dem Aufwand des Angreifers:

| Stufe | Maßstab |
| --- | --- |
| hoch | Ausfall durch einen einzelnen Client mit wenigen Anfragen, ohne Anmeldung oder mit der niedrigsten Rolle |
| mittel | Ausfall durch eine angemeldete Rolle (Insider) |
| niedrig | Ausfall oder Verzögerung nur mit vielen Adressen (verteilter Angriff) |

Vertraulichkeit, Integrität, Rechteausweitung, Umgehung von Sperre oder Audit:

| Stufe | Maßstab |
| --- | --- |
| hoch | Ohne Anmeldung oder mit niedrigster Rolle erreichbar und Datenabfluss zwischen Benutzern, Rechteausweitung oder Umgehung von Sperre oder Audit |
| mittel | Sicherheitsschutz geschwächt, aber begrenzt; oder nur mit erhöhter Rolle erreichbar |
| niedrig | Randfall, Härtung |

Funktionsfehler ohne Sicherheitsbezug sind nie hoch oder mittel im Sinne einer Release-Schwelle; sie blockieren 1.0, nicht 0.x.

Ein Restrisiko braucht eine Messung oder die Begründung, warum es nicht messbar ist; in beiden Fällen gilt es nur nach ausdrücklicher Freigabe des Projektinhabers. Widerspricht es einer Anforderung, braucht es eine Revision.
