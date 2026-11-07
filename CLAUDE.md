# RCONductor – Regeln für Claude Code

## Projekt
ShadowGhost.NET Game RCONductor (kurz RCONductor): selbst gehostetes Webpanel
für RCON-fähige Spieleserver. Lizenz GNU AGPL-3.0.

## Technik
- Panel und Agent: Go, aktuelle stabile Version, CGO aus, statische Binaries,
  amd64 und arm64.
- HTTP mit net/http der Standardbibliothek; WebSocket mit coder/websocket.
- API: api/openapi.yaml ist die einzige Quelle. Server-Stubs mit oapi-codegen,
  Client-Typen mit openapi-typescript. Erst die OpenAPI-Datei erweitern, dann umsetzen.
- Datenbank: database/sql mit sqlc; SQLite über modernc.org/sqlite;
  Migrationen mit goose, eingebettet, je Datenbank ein Satz.
- Oberfläche: React mit TypeScript und Vite, TanStack Router und Query,
  shadcn/ui mit Tailwind, i18next (Englisch). Statisch ins Panel-Binary eingebettet.
- Logging: log/slog. Jedes prüfrelevante Ereignis als JSON-Zeile mit festem
  Ereigniscode (internal/logging). Neue Codes nur ergänzen, nie umbenennen.
- Jede Codeänderung nennt die Anforderungs-ID(s) in der Commit-Nachricht,
  z. B. "feat(cmd): Token-Pfad-Abgleich (CMD-004)".

## Repository-Struktur
- cmd/panel, cmd/agent   Einstiegspunkte
- internal/              gesamter Go-Code, nach Modulen
- web/                   Oberfläche
- api/openapi.yaml       API-Beschreibung
- migrations/sqlite, migrations/postgres
- profiles/              mitgelieferte Spielprofile
- deploy/compose/        Compose-Dateien
- deploy/dockerhub/      Texte für Docker Hub
- scripts/               Hilfsskripte für Prüfung und Release
- docs/user/             Benutzerdokumentation

## Sicherheit (nicht verhandelbar)
- Rechte werden ausschließlich im Backend geprüft und durchgesetzt; Standard ist Ablehnung.
  Die Oberfläche blendet nur aus, was eine Rolle nicht darf; sie entscheidet nie über Rechte.
- Das Panel greift nie auf den Docker-Socket zu, nur der Agent.
- Keine Geheimnisse in Logs (SEC-012). Einzige Ausnahme: einmalige Ausgabe des
  Initialpassworts (AUTH-007).
- Alle Daten von Spieleservern und Agents gelten als Fremddaten (SEC-006).
- Keine Abhängigkeit mit AGPL-inkompatibler Lizenz. Neue Abhängigkeiten nur
  mit kurzer Begründung.

## Tests und Prüfung
- Go: go test ./..., Integrationstests mit testcontainers-go.
- Oberfläche: Vitest, Abläufe mit Playwright.
- Vor jedem Commit: Lint und Tests der geänderten Pakete (Go mit -race), über den
  Pre-Commit-Hook. Vor jedem Push: make check vollständig (golangci-lint, ESLint,
  Prettier, generierter Code, alle Tests grün), über den Pre-Push-Hook; einmal je Klon
  aktivieren mit git config core.hooksPath .githooks.
- Tests lokal gezielt:
  - Während eines Teilschritts nur betroffene Tests: go test der geänderten
    Pakete, vitest related, playwright --grep auf die betroffenen Specs.
  - Volle E2E-Suite und Integrationstests lokal nur am Ende eines Schritts
    oder wenn Backend und Oberfläche gemeinsam geändert wurden.
  - Minecraft-Test lokal nie; das macht die CI.
- Fehler zuerst reproduzieren, dann gezielt beheben; keine Komplettläufe als
  Fehlersuche.
- Neue Grenzen und Zeiten sind Einstellungen mit Standard und validiertem Mindestwert;
  kleinere Werte nur über Test-Konstruktor oder Test-Konfiguration, nie per Env in
  Produktion. Zeit in Tests über injizierte Uhr oder testing/synctest, kein echtes Sleep.
  Feste Werte bleiben nur Protokollgrößen, Krypto-Parameter und Feldlängen.
- Definition of Done für Tests: neben dem Normalfall auch Reihenfolge- und
  Nebenläufigkeitsfälle und Missbrauch (große Eingaben, parallele Anfragen, fehlende
  Pflichtfelder), wo die Anforderung sie berührt.

## Sprache
- Code, Kommentare, Commit-Nachrichten und Oberflächentexte: Englisch.
