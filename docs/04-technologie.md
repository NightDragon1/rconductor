# Technologie

Dokumentversion 2.3, freigegeben; Änderungen gegenüber 2.2 siehe Gesamtkonzept, Kapitel 12, gegenüber 2.1 Kapitel 11, gegenüber 2.0 Kapitel 10. Abgeleitet aus den Tabs Architektur und Design. Panel und Agent werden in Go geschrieben, die Oberfläche in React mit TypeScript; damit ist D-001 bestätigt und D-002 ersetzt.

## 1. Grundsätze

- Wenige, gepflegte Abhängigkeiten; Standardbibliothek vor Framework.
- Ein statisches Binary je Image, ohne CGO, für amd64 und arm64. Tests dürfen CGO nutzen (Race-Detector).
- Eine Schnittstellenbeschreibung (OpenAPI 3.1) als einzige Quelle für Server- und Client-Typen.
- Jede Bibliothek mit kompatibler Lizenz zur AGPL-3.0 (D-012).

## 2. Panel und Agent

| Bereich | Wahl | Begründung | Release |
| --- | --- | --- | --- |
| Sprache | Go, jeweils aktuelle stabile Version | Docker-SDK, RE2, TLS 1.3 und Ed25519 nativ; statische Binaries, einfacher Cross-Compile | R1 |
| HTTP und Routing | net/http mit Pattern-Routing der Standardbibliothek | keine Framework-Abhängigkeit, ausreichend für REST nach Design 2 | R1 |
| API-Schicht | oapi-codegen, Server-Stubs aus der OpenAPI-Datei | API und Implementierung können nicht auseinanderlaufen; Grundlage für die externe API ab R2 | R1 |
| WebSocket | coder/websocket (Nachfolger von nhooyr/websocket) | Browser und Agent über dieselbe Bibliothek, Kontext-basiert, gepflegt | R1 |
| Agent-Port | crypto/tls mit TLS 1.3, selbstsignierte Ed25519-Zertifikate, Pinning über eigene Zertifikatsprüfung | erfüllt D-005 und D-019 ohne CA | R1 |
| Docker | github.com/moby/moby/client (offizielles SDK) | einziger offizieller Weg zur Docker-API; github.com/docker/docker ist seit Docker v29 veraltet | R1 |
| Socket-Zugriff des Agents | Nicht-Root (65532) mit group\_add auf die GID des Sockets; empfohlen ein Socket-Proxy (linuxserver/socket-proxy, eigenes Image) | Defense-in-Depth; Zugriff auf den Socket bleibt root-äquivalent (Sicherheit T-022) | R1 |
| Source RCON | eigener Adapter mit Endmarker-Verfahren hinter der Adapter-Schnittstelle (Design 3.8) | gorcon/rcon liest je Befehl nur ein Paket und vervollständigt Antworten über 4 KB nicht (PROT-001); kein Fremdcode nötig | R1 |
| Weitere Protokolle | eigene Adapter oder Bibliotheken je Protokoll | HTTP-API ab R2, BattlEye, WebRCON und Telnet ab R3 | R2, R3 |
| Datenbankzugriff | database/sql mit sqlc, je Datenbank ein eigener Satz Abfragen | typsicher, SQL bleibt sichtbar, kein ORM-Overhead | R1 |
| SQLite | modernc.org/sqlite | reines Go ohne CGO, dadurch statisches Binary | R1 |
| Postgres | pgx im database/sql-Modus | Standardtreiber für Postgres in Go | R2 |
| Migrationen | goose, eingebettet, je Datenbank ein Migrationssatz | D-018 | R1 |
| Passwörter | Argon2id aus golang.org/x/crypto | Design 4.1 | R1 |
| Verschlüsselung | AES-256-GCM und HKDF aus der Standardbibliothek | D-030 | R1 |
| IDs | UUIDv7 mit google/uuid | D-027 | R1 |
| Regex | regexp der Standardbibliothek (RE2) | lineare Laufzeit, SEC-003 | R1 |
| Spielprofile | YAML mit goccy/go-yaml, Dateiänderungen über fsnotify, zusätzlich Abgleich in festem Intervall (Design 7.1) | Hot Reload nach CAT-003, auch auf Netzlaufwerken | R1 |
| Logging | log/slog | Text oder JSON nach RCD\_LOG\_FORMAT | R1 |
| Zwei-Faktor | pquerna/otp (TOTP) | verbreitet, RFC 6238 | R2 |
| Keycloak | coreos/go-oidc mit golang.org/x/oauth2 | Standard-OIDC, nur Authentifizierung (D-007) | R2 |
| Metriken | prometheus/client\_golang | NFR-006 | R2 |

## 3. Oberfläche

| Bereich | Wahl | Begründung | Release |
| --- | --- | --- | --- |
| Framework | React mit TypeScript, als Single Page App mit Vite gebaut und ins Panel-Binary eingebettet | größte Basis an Mitwirkenden für ein Open-Source-Projekt; kein Server-Rendering nötig | R1 |
| Routing | TanStack Router | typsichere Routen und Suchparameter (Filter, Spuren) | R1 |
| Serverdaten | TanStack Query; WebSocket-Ereignisse aktualisieren den Query-Cache | ein Datenweg für REST und Live-Daten | R1 |
| API-Typen | openapi-typescript aus derselben OpenAPI-Datei | gleiche Typen wie im Backend | R1 |
| Komponenten | shadcn/ui (Radix-Primitives, Quellcode im Repository) mit Tailwind CSS | barrierearme Grundbausteine, eigenes Aussehen, helles und dunkles Design über Tokens | R1 |
| Konsole | eigene Komponente mit TanStack Virtual | lange Verläufe ohne Ruckeln | R1 |
| Formulare | react-hook-form mit zod | Prüfung im Browser, endgültige Prüfung bleibt im Backend | R1 |
| Sprachen | i18next mit react-i18next | Englisch ab R1, Deutsch ab R2 (NFR-007) | R1 |
| API-Laufzeit | github.com/oapi-codegen/runtime | vom generierten Server-Code benötigt (D-039) | R1 |
| Zeitreihen | uPlot | schnell bei vielen Messpunkten, wie in Grafana | R2 |
| Topologie | React Flow (@xyflow/react) | ausgereifte Graph-Bibliothek für den Live-Graph (UI-010) | R2 |

SvelteKit (bisher D-002) bringt kleinere Bundles und weniger Code. Bei einer ins Binary eingebetteten Oberfläche im LAN wiegt das wenig; React bietet mehr mögliche Mitwirkende, und React Flow ist das Original von Svelte Flow.

## 4. Build, Auslieferung und Qualität

| Bereich | Wahl |
| --- | --- |
| Repository | Ein Repository: cmd/panel, cmd/agent, internal, web, profiles, migrations, api/openapi.yaml, deploy/compose, scripts (Hilfsskripte, nicht Teil der Images) |
| Images | Mehrstufiger Build; Panel auf distroless static als Nicht-Root-Benutzer mit read-only Root-Dateisystem; Agent ebenfalls distroless static |
| Plattformen | amd64 und arm64 über Go-Cross-Compile und docker buildx |
| Registry | ghcr.io; Tags 1, 1.2 und 1.2.3, damit die Hauptversion allein genügt (D-023) |
| Lieferkette | SEC-013: SBOM mit syft, Signatur der Images mit cosign keyless, govulncheck als Pflicht-Check, Actions auf Commit-SHA gepinnt, Renovate gruppiert und ohne Automerge |
| Tests Backend | go test; Integrationstests mit testcontainers-go gegen Postgres und Docker |
| Tests Oberfläche | Vitest für Komponenten, Playwright für Abläufe im Browser |
| Statische Prüfung | golangci-lint, ESLint, Prettier, SPDX-Header; gebündelt in make check, auch als Pre-Commit-Hook |
| CI | GitHub Actions |

## 5. Offene Punkte

1. Vorgehen bei Lizenzprüfung neuer Abhängigkeiten (z. B. automatisiert in der CI).

## Quellen

- [moby/moby](https://github.com/moby/moby): Go-Client als eigenes Modul, github.com/docker/docker seit v29 veraltet
- [gorcon/rcon](https://github.com/gorcon/rcon/blob/master/README.md): Source RCON in Go, MIT; geprüft und nicht verwendet (Design 3.8)
- [coder/websocket](https://github.com/nhooyr/ws): Nachfolger von nhooyr/websocket
