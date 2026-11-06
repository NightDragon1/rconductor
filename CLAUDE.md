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
- docs/ nicht verändern, außer auf meinen ausdrücklichen Auftrag. Vorschläge für
  Änderungen nur als Hinweis an mich.
- Kleine Schritte: lieber mehrere kleine Commits als ein großer.
- PRs nach grüner CI selbst mergen, außer Änderungen an CLAUDE.md, CONTRIBUTING.md,
  Release-Workflow (auch die von ihm aufgerufenen Skripte) und Rulesets: die merge ich.
- Renovate-PRs: Patch, Minor und Digest nach grüner CI selbst mergen, Major vorher melden.
- Bei Eingriffen in die Git-Historie Renovate pausieren (renovate.json "enabled": false),
  danach wieder aktivieren.
- Releases: Regeln in CONTRIBUTING.md, Abschnitt Release. Ein echtes Release nur nach
  meinem OK; die Freigabe im Environment release mache ich.
- Keine Sub-Agents für Release-Schritte und für Eingriffe in die Git-Historie.
- Kein Force-Push. Falsche Nachricht in einem gepushten Commit: Kommentar am
  betroffenen Issue, die Historie bleibt.
- Nichts systemweit installieren. Die Minecraft-EULA nie stellvertretend akzeptieren.
- Status prüfen, nicht annehmen: CI-Läufe, Merges und Issues vor jeder Meldung
  abfragen. Nicht Abgefragtes als ungeprüft melden, nie als "offen".

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
- docs/                  Konzept, nicht verändern

## Sicherheit (nicht verhandelbar)
- Rechte werden ausschließlich im Backend geprüft und durchgesetzt; Standard ist Ablehnung.
  Die Oberfläche blendet nur aus, was eine Rolle nicht darf; sie entscheidet nie über Rechte.
- Das Panel greift nie auf den Docker-Socket zu, nur der Agent.
- Keine Geheimnisse in Logs (SEC-012). Einzige Ausnahme: einmalige Ausgabe des
  Initialpassworts (AUTH-007).
- Alle Daten von Spieleservern und Agents gelten als Fremddaten (SEC-006).
- Keine Abhängigkeit mit AGPL-inkompatibler Lizenz. Neue Abhängigkeiten nur
  mit kurzer Begründung im Plan.

## Schwere von Befunden
- hoch: ohne Anmeldung oder mit niedrigster Rolle erreichbar UND Ausfall des Panels,
  Datenabfluss zwischen Benutzern, Rechteausweitung oder Umgehung von Sperre/Audit.
- mittel: Sicherheitsschutz geschwächt, aber begrenzt; oder nur mit erhöhter Rolle
  erreichbar.
- niedrig: Randfall, Härtung, Funktionsfehler ohne Sicherheitsbezug.
- Funktionsfehler ohne Sicherheitsbezug sind nie hoch/mittel im Sinne einer
  Release-Schwelle; sie blockieren 1.0, nicht 0.x.
- Restrisiko: nur mit Messung und nur nach meiner ausdrücklichen Freigabe.
  Widerspricht es einer Anforderung, braucht es eine Revision.
- Blinde Prüfungen haben je Runde einen festen Umfang. Funde außerhalb: nur hoch
  blockiert, der Rest wird Issue.

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
   unterbrochene Aufgabe, offene CI-Läufe, offene Fragen, "Offene Aufträge" im Wortlaut.
4. Kurze Meldung. Danach nichts.

START: docs/handover.md und Git-Stand lesen. Offene CI-Läufe prüfen. wip/-Branch
vorhanden: fertigstellen, make check, Squash-Merge, Branch löschen. Dann nächster
Handgriff aus der Übergabe. Offene Fragen oder ausstehendes Screenshot-OK: erst
melden und warten.

Das bloße Wort "STOPP" bzw. "START" von mir löst den Ablauf aus.

## Sprache
- Code, Kommentare, Commit-Nachrichten und Oberflächentexte: Englisch.
- Antworten an mich: Deutsch, kurz und sachlich.