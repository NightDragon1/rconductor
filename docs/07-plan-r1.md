# Arbeitspakete R1

Dokumentversion 2.5, freigegeben; Änderungen gegenüber 2.4 siehe Gesamtkonzept, Kapitel 14, gegenüber 2.3 Kapitel 13, gegenüber 2.2 Kapitel 12, gegenüber 2.1 Kapitel 11, gegenüber 2.0 Kapitel 10. R1 entsteht in sechs Meilensteinen; nach Fundament und Rechten laufen RCON-Kern, Agent und Oberfläche parallel. Erstes Ziel ist ein Durchstich: ein Admin sendet list an einen Minecraft-Server und sieht die Antwort im Browser.

## 1. Ablauf

&#91;embedded content: Meilensteine R1 · Reihenfolge und Abhängigkeiten\]

M2 ist hervorgehoben, weil der Durchstich dort entsteht. M4 baut zuerst gegen Attrappen und schließt Live-Daten an, sobald M2 und M3 liefern.

## 2. Meilensteine

| Meilenstein | Inhalt | Anforderungen | Setzt voraus |
| --- | --- | --- | --- |
| M0 Fundament | Repository-Struktur, CI mit Lint, Tests, govulncheck; Multi-Arch-Build und signierte Images; Konfiguration per Env; JSON-Logging mit Ereigniscodes und Schwärzung von Geheimnissen; Datenbank mit goose und sqlc; Master-Key; Backups; OpenAPI-Gerüst | NFR-001, NFR-002, NFR-008, NFR-010, NFR-013, SEC-005, SEC-012, SEC-013, NFR-016, PRJ-001, PRJ-002 | – |
| M1 Identität und Rechte | Benutzer, Gruppen, Sessions, vertrauenswürdige Proxys (Teil von NFR-003), Login-Schutz, Notfall-Admin mit Initialpasswort, Rollen und Scope-Berechnung, Einladungen, Audit-Grundgerüst mit Aufbewahrung | AUTH-001, AUTH-002, AUTH-003, AUTH-006, AUTH-007, AUTH-009, ROLE-001 bis ROLE-008, ROLE-010, SEC-004, AUD-001, AUD-003, AUD-004 | M0 |
| M2 RCON-Kern | Erzwungener Passwortwechsel, Wiederherstellung des Notfall-Admins; WebSocket-Grenzen (aus M4 vorgezogen); Profil-Import mit Hot Reload; Server und Servergruppen; Verbindungsmanager mit Source-RCON-Adapter; Befehlspipeline mit Bereinigung, Token-Abgleich, Wrapper-Sperre, Ratenlimit; Ereignisbus und WebSocket; Durchstich | AUTH-010, AUTH-011, CAT-001, CAT-002, CAT-003, CAT-005, SRV-001 bis SRV-006, PROT-001, PROT-002, CMD-002, CMD-004 bis CMD-007, CMD-019, SEC-003, SEC-006, SEC-008 | M1 |
| M3 Agent | Agent-Binary; Protokoll aus Design 1; Kopplung über Token und Fingerprint; Pinning und Widerruf; Docker-Operationen, Messwerte, Logs; Versionsregel; Cache mit Altersangabe; Neustart ohne Agent | AGT-001 bis AGT-005, AGT-008, AGT-009, AGT-011 bis AGT-013, NFR-005, SEC-007, SEC-010 | M1 |
| M4 Oberfläche | Grundgerüst mit Navigation, hell und dunkel, Englisch; Landingpages und Karten; Serverseite mit Konsole; Befehlsseite; Verwaltung mit Schnelleingabe; Handy-Ansicht; Sicherheitsheader | UI-001 bis UI-004, UI-006 bis UI-009, UI-011, UI-012, CMD-001, CMD-003, CMD-012 bis CMD-015, NFR-007, SEC-009 | M1; Live-Teile mit M2 und M3 |
| M5 Auslieferung | Beide Compose-Varianten, README mit Pfaden und Betrieb, Dokumentation, E2E-Testlauf aller Abnahmekriterien, Review des Bedrohungsmodells, Versionsschema nach D-047 mit Anpassung der Versionsprüfung (AGT-013), GitHub-Release und Docker Hub, Release 1.0 | NFR-003, NFR-004, NFR-014, SEC-001, SEC-002, NFR-009, DOC-001 bis DOC-005, PRJ-003, PRJ-004 | M2, M3, M4 |

## 3. Teilabnahmen

Einige Anforderungen werden in einem Meilenstein umgesetzt, ihr Abnahmekriterium setzt aber einen späteren voraus. Ihr Issue hängt am Meilenstein der Abnahme.

| Anforderung | Umgesetzt in | Abnahme in | Grund |
| --- | --- | --- | --- |
| NFR-001 | M0 | M3 | Agent-Port 8443 |
| NFR-016, SEC-012 | M0 | M5 | Testlauf aller Ereigniscodes und Geheimnisse |
| PRJ-001, PRJ-002 | M0 | M5 | Veröffentlichung, Review vor dem Release |
| NFR-003 (Proxys) | M1 | M5 | Basispfad und WebSocket-Upgrade |
| ROLE-003, ROLE-005, ROLE-007 | M1 | M2 | Server-API, Befehlspipeline |
| AUD-001 | M1 | M3 | Einträge für Befehle und Agent-Aktionen |
| ROLE-002, AUTH-009 | M1 | M4 | E2E in der Oberfläche |
| SEC-013 | M0 | M5 | erstes Release mit Signatur und SBOM |
| SRV-001 | M2 | M4 | Auswahl von Agent und Container (setzbar ab M3), E2E |
| AGT-005 | M3 | M4 | Kopplungsdialog, E2E |
| SEC-006, SRV-003 | M2 | M4 | E2E in der Oberfläche (Darstellung, Farbcodes, Servergruppen) |
| AUTH-006 | M1, M2 | M4 | E2E: Abmelden beendet offene WebSockets in der Oberfläche |

## 4. Regeln

- Jeder Meilenstein ist ein GitHub-Milestone; jede Anforderung wird mindestens ein Issue mit ihrer ID im Titel.
- Ein Issue ist erledigt, wenn sein Abnahmekriterium (Tab Abnahme R1) in der genannten Prüfart besteht und die Doku nachgezogen ist.
- Die OpenAPI-Datei wird vor der Umsetzung eines Endpunkts erweitert; Backend und Oberfläche bauen gegen denselben Stand.
- M4 beginnt mit Attrappen der API (aus der OpenAPI-Datei) und schaltet auf echte Daten um, sobald M2 bzw. M3 sie liefern.
- Nach M5 wird das Bedrohungsmodell gegen den tatsächlichen Stand geprüft.
- /backups (API und Seite) folgt nach M5, nicht in 1.0 (Issue #101); NFR-008 ist davon nicht betroffen.
- Jedes neue prüfrelevante Verhalten bekommt im selben Issue seinen Ereigniscode (Design 8.3); ohne Code gilt das Abnahmekriterium aus Abschnitt 9 des Tabs Abnahme R1 als nicht erfüllt.
