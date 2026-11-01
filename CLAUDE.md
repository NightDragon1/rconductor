# RCONductor – Regeln für Claude Code

## Projekt
ShadowGhost.NET Game RCONductor (kurz RCONductor): selbst gehostetes Webpanel
für RCON-fähige Spieleserver. Lizenz GNU AGPL-3.0.
Das freigegebene Konzept (Version 2.0) liegt in docs/ und ist verbindlich.

## Verbindliche Dokumente
- docs/01-gesamtkonzept.md  Anforderungen mit IDs, Rollen, Entscheidungen D-xxx, Releaseplan
- docs/02-architektur.md    Bausteine, Verbindungen, Prinzipien
- docs/03-design.md         Agent-Protokoll, API, Rechteprüfung, Datenmodell,
                            Konfiguration, Oberfläche, Spielprofile, Ereigniscodes
- docs/04-technologie.md    Sprachen, Bibliotheken, Build
- docs/05-abnahme-r1.md     Abnahmekriterium je Anforderung
- docs/06-sicherheit.md     Bedrohungsmodell, Maßnahmen SEC-xxx
- docs/07-plan-r1.md        Meilensteine M0 bis M5
- docs/mockups/             Aussehen der Oberfläche

## Arbeitsweise
- Arbeite immer nur am Meilenstein, den ich nenne. Nichts aus späteren Meilensteinen
  oder Releases (R2, R3, Backlog) vorziehen.
- Vor jeder Umsetzung: Plan zeigen (Dateien, Schritte, betroffene Anforderungs-IDs)
  und auf mein OK warten.
- Jede Codeänderung nennt die Anforderungs-ID(s) in der Commit-Nachricht,
  z. B. "feat(cmd): Token-Pfad-Abgleich (CMD-004)".
- Eine Anforderung ist erst fertig, wenn ihr Abnahmekriterium aus
  docs/05-abnahme-r1.md in der genannten Prüfart besteht (Test vorhanden und grün).
- Widerspricht eine Anforderung einer anderen oder dem Design: nicht selbst
  entscheiden, sondern stoppen und mir den Widerspruch mit Fundstellen zeigen.
- docs/ nicht verändern. Vorschläge für Änderungen nur als Hinweis an mich.
- Kleine Schritte: lieber mehrere kleine Commits als ein großer.

## Technik (Details in docs/04-technologie.md)
- Panel und Agent: Go, aktuelle stabile Version, CGO aus, statische Binaries,
  amd64 und arm64.
- HTTP mit net/http der Standardbibliothek; WebSocket mit coder/websocket.
- API: api/openapi.yaml ist die einzige Quelle. Server-Stubs mit oapi-codegen,
  Client-Typen mit openapi-typescript. Erst die OpenAPI-Datei erweitern, dann umsetzen.
- Datenbank: database/sql mit sqlc; SQLite über modernc.org/sqlite;
  Migrationen mit goose, eingebettet, je Datenbank ein Satz.
- Oberfläche: React mit TypeScript und Vite, TanStack Router und Query,
  shadcn/ui mit Tailwind, i18next (Englisch in R1). Statisch ins Panel-Binary eingebettet.
- Logging: log/slog. Jedes prüfrelevante Ereignis als JSON-Zeile mit festem
  Ereigniscode laut docs/03-design.md Kapitel 8. Neue Codes nur ergänzen,
  nie umbenennen.

## Repository-Struktur
- cmd/panel, cmd/agent   Einstiegspunkte
- internal/              gesamter Go-Code, nach Modulen aus docs/02-architektur.md Kapitel 4
- web/                   Oberfläche
- api/openapi.yaml       API-Beschreibung
- migrations/sqlite, migrations/postgres
- profiles/              mitgelieferte Spielprofile
- deploy/compose/        Compose-Dateien (ab M5)
- docs/                  Konzept, nicht verändern

## Sicherheit (nicht verhandelbar)
- Rechte werden ausschließlich im Backend geprüft; Standard ist Ablehnung.
- Das Panel greift nie auf den Docker-Socket zu, nur der Agent.
- Keine Geheimnisse in Logs (SEC-012). Einzige Ausnahme: einmalige Ausgabe des
  Initialpassworts (AUTH-007).
- Alle Daten von Spieleservern und Agents gelten als Fremddaten (SEC-006).
- Keine Abhängigkeit mit AGPL-inkompatibler Lizenz. Neue Abhängigkeiten nur
  mit kurzer Begründung im Plan.

## Tests und Prüfung
- Go: go test ./..., Integrationstests mit testcontainers-go.
- Oberfläche: Vitest, Abläufe mit Playwright.
- Vor jedem Commit: golangci-lint, ESLint, Prettier, alle Tests grün.
- Nach jedem Meilenstein: kurze Zusammenfassung, welche Anforderungs-IDs
  erledigt sind und welche Ereigniscodes neu sind.

## Sprache
- Code, Kommentare, Commit-Nachrichten und Oberflächentexte: Englisch.
- Antworten an mich: Deutsch, kurz und sachlich.