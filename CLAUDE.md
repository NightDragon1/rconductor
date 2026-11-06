# RCONductor – Regeln für Claude Code

## Projekt
ShadowGhost.NET Game RCONductor (kurz RCONductor): selbst gehostetes Webpanel
für RCON-fähige Spieleserver. Lizenz GNU AGPL-3.0.
Das freigegebene Konzept liegt in docs/ und ist verbindlich; die gültige Revision steht
in docs/01-gesamtkonzept.md.

## Verbindliche Dokumente
- docs/01-gesamtkonzept.md  Anforderungen mit IDs, Rollen, Entscheidungen D-xxx, Releaseplan
- docs/02-architektur.md    Bausteine, Verbindungen, Prinzipien
- docs/03-design.md         Agent-Protokoll, API, Rechteprüfung, Datenmodell,
                            Konfiguration, Oberfläche, Spielprofile, Ereigniscodes
- docs/04-technologie.md    Sprachen, Bibliotheken, Build
- docs/05-abnahme-r1.md     Abnahmekriterium je Anforderung
- docs/06-sicherheit.md     Bedrohungsmodell, Maßnahmen SEC-xxx
- docs/07-plan-r1.md        Meilensteine M0 bis M5

## Arbeitsweise
- Arbeite immer nur am Meilenstein, den ich nenne. Nichts aus späteren Meilensteinen
  oder Releases (R2, R3, Backlog) vorziehen.
- Vor jeder Umsetzung: Plan zeigen (Dateien, Schritte, betroffene Anforderungs-IDs)
  und auf mein OK warten.
- Jede Codeänderung nennt die Anforderungs-ID(s) in der Commit-Nachricht,
  z. B. "feat(cmd): Token-Pfad-Abgleich (CMD-004)".
- Eine Anforderung ist erst fertig, wenn ihr Abnahmekriterium aus
  docs/05-abnahme-r1.md in der genannten Prüfart besteht (Test vorhanden und grün).
- Meilenstein-Abschluss: Bevor Abnahmen grün werden, blinde Prüfung durch einen
  Sub-Agent ohne Vorkontext (Stichprobe der Abnahmen und Sicherheit).
- Widerspricht eine Anforderung einer anderen oder dem Design: nicht selbst
  entscheiden, sondern stoppen und mir den Widerspruch mit Fundstellen zeigen.
- docs/ nur über Revisionen, auf meinen Auftrag. Vorschläge für Änderungen als Hinweis an mich.
- Kleine Schritte: lieber mehrere kleine Commits als ein großer.
- Renovate-PRs: Patch, Minor und Digest nach grüner CI selbst mergen, Major vorher melden.
- Bei Eingriffen in die Git-Historie Renovate pausieren (renovate.json "enabled": false),
  danach wieder aktivieren.
- Releases: Regeln in CONTRIBUTING.md, Abschnitt Release.

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
- scripts/               Hilfsskripte für Projektpflege (z. B. gh_plan.py für Milestones und Issues)
- docs/                  Konzept, nur über Revisionen

## Sicherheit (nicht verhandelbar)
- Rechte werden ausschließlich im Backend geprüft und durchgesetzt; Standard ist Ablehnung.
  Die Oberfläche blendet nur aus, was eine Rolle nicht darf; sie entscheidet nie über Rechte.
- Das Panel greift nie auf den Docker-Socket zu, nur der Agent.
- Keine Geheimnisse in Logs (SEC-012). Einzige Ausnahme: einmalige Ausgabe des
  Initialpassworts (AUTH-007).
- Alle Daten von Spieleservern und Agents gelten als Fremddaten (SEC-006).
- Keine Abhängigkeit mit AGPL-inkompatibler Lizenz. Neue Abhängigkeiten nur
  mit kurzer Begründung im Plan.

## Tests und Prüfung
- Go: go test ./..., Integrationstests mit testcontainers-go.
- Oberfläche: Vitest, Abläufe mit Playwright.
- Vor jedem Commit: make check (golangci-lint, ESLint, Prettier, generierter Code,
  alle Tests grün). Der Pre-Commit-Hook in .githooks ruft es auf; einmal je Klon
  aktivieren mit git config core.hooksPath .githooks.
- Nach jedem Meilenstein: kurze Zusammenfassung, welche Anforderungs-IDs
  erledigt sind und welche Ereigniscodes neu sind.
- Tests lokal gezielt:
  - Während eines Teilschritts nur betroffene Tests: go test der geänderten
    Pakete, vitest related, playwright --grep auf die betroffenen Specs.
  - Vor jedem Commit: make check (Lint, Unit, Vitest).
  - Volle E2E-Suite und Integrationstests lokal nur am Ende eines Schritts
    oder wenn Backend und Oberfläche gemeinsam geändert wurden.
  - Minecraft-Test lokal nie; das macht die CI.
- Die CI ist die vollständige Prüfinstanz. Nicht auf CI-Läufe warten, außer vor
  Merge, Abnahme oder Release. Ergebnisse beim nächsten Bericht nachreichen.
- Fehler zuerst reproduzieren, dann gezielt beheben; keine Komplettläufe als
  Fehlersuche.
- Definition of Done für Tests: neben dem Normalfall auch Reihenfolge- und
  Nebenläufigkeitsfälle und Missbrauch (große Eingaben, parallele Anfragen, fehlende
  Pflichtfelder), wo die Anforderung sie berührt.

## Kontext und Berichte
- Kontext sparsam: docs/handover.md und das betroffene Kapitel lesen, nicht alle
  docs/ bei jedem Schritt.
- docs/handover.md ist eine lokale Datei (in .gitignore): überschreiben, nicht
  committen. Screenshots unter docs/screenshots/ bleiben ebenfalls lokal.
- Berichte wie bisher in Umfang und Struktur; Testzahlen dürfen als Summe je
  Testart stehen.

## START/STOPP
STOPP: Laufende Arbeit sofort beenden, nichts Neues anfangen.
1. git status prüfen. Fertig und make check grün: auf main committen und pushen.
   Sonst: Branch wip/<datum>, --no-verify, Nachricht "wip: <was lief>", pushen.
2. Nicht auf CI warten, keine Tests mehr starten.
3. docs/handover.md lokal überschreiben (nicht committen): Branch/Commit, erledigt,
   unterbrochene Aufgabe, offene CI-Läufe, offene Fragen, offene Aufträge im Wortlaut.
4. Kurze Meldung. Danach nichts.

START: docs/handover.md und Git-Stand lesen. Offene CI-Läufe prüfen. wip/-Branch
vorhanden: fertigstellen, make check, Squash-Merge, Branch löschen. Dann nächster
Handgriff aus der Übergabe. Offene Fragen oder ausstehendes Screenshot-OK: erst
melden und warten.

Das bloße Wort "STOPP" bzw. "START" von mir löst den Ablauf aus.

## Sprache
- Code, Kommentare, Commit-Nachrichten und Oberflächentexte: Englisch.
- Antworten an mich: Deutsch, kurz und sachlich.