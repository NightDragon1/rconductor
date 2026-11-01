# Kandidaten für Revision 2.1

Stand: Abschluss M0 (01.11.2026). Abweichungen und Lücken, die bei der Umsetzung von M0 aufgefallen sind. Keiner der Punkte ändert das freigegebene Konzept 2.0; über die Übernahme entscheidet Rob. Verweise auf Commits beziehen sich auf den Stand nach dem Umschreiben der Historie.

| Nr. | Thema | Betroffen |
| --- | --- | --- |
| 1 | Reservierte Pfade für Health und Metriken | NFR-006, Design 2 |
| 2 | Auslieferung der eingebetteten Oberfläche | Design 2 und 6, Architektur 4 |
| 3 | Read-only Root-Dateisystem | SEC-002, Technologie 4 |
| 4 | Deep-Links mit Basispfad | NFR-003, Design 6 |
| 5 | Wortlaut der Abnahme von AGT-001 | Abnahme R1, Abschnitt 7 |
| 6 | CGO bei Tests | Technologie 1 und 4, CLAUDE.md |
| 7 | Anforderung für Lieferkette und CI | D-040, Technologie 4 |
| 8 | Socket-Zugriff des Agent-Images | Technologie 5.1, AGT-001 |
| 9 | Verzeichnis scripts/ | Technologie 4 |

## 1. Reservierte Pfade für Health und Metriken

- **Fundstelle:** 01-gesamtkonzept.md, NFR-006 (R2: „Prometheus-Metriken und Healthcheck-Endpoint“); 03-design.md, Kapitel 2.2 nennt keine Pfade dafür. Umsetzung: internal/web/web.go (`reservedPaths`), Commit 6730098.
- **Vorschlag:** In Design 2.2 festlegen: `/healthz` (Healthcheck) und `/metrics` (Prometheus) liegen außerhalb von `/api/v1`, sind ab R1 reserviert und werden nie von der Oberfläche beantwortet. Ab R2 mit NFR-006 befüllt; Zugriffsschutz für `/metrics` dort festlegen.
- **Begründung:** Ohne Reservierung würde der SPA-Fallback diese Pfade mit `index.html` beantworten; ein Healthcheck meldete dann fälschlich „gesund“. Die Namen sind Konvention (Kubernetes, Prometheus), aber bisher nicht im Konzept.

## 2. Auslieferung der eingebetteten Oberfläche

- **Fundstelle:** 02-architektur.md, Kapitel 4 (Web-Schicht: „Auslieferung der Oberfläche“); 03-design.md, Kapitel 2 und 6 ohne Regeln dazu. Umsetzung: internal/webui/webui.go, Commit 6730098.
- **Vorschlag:** In Design 6.1 (oder 2.1) aufnehmen:
  - Pfade ohne Dateiendung liefern `index.html` (Client-Routen nach Neuladen), mit `Cache-Control: no-cache`.
  - Pfade mit Dateiendung sind Assets; fehlt die Datei, antwortet das Panel mit 404 als Problem Details, nie mit `index.html`. Gehashte Assets werden als `immutable` gecacht.
  - Versteckte Dateien (Pfadteil beginnt mit Punkt) werden nicht ausgeliefert.
  - Nur GET und HEAD; sonst 405.
  - Ohne gebaute Oberfläche antwortet jede Seite mit 503 „web UI is not built“; die API bleibt nutzbar.
- **Begründung:** Das Verhalten ist für Abnahme und Fehlersuche relevant (z. B. 404 statt leerer Seite bei fehlendem Asset), stand aber nirgends. Ohne Festlegung könnten spätere Änderungen es unbemerkt ändern.

## 3. Read-only Root-Dateisystem

- **Fundstelle:** 01-gesamtkonzept.md, SEC-002 (Least Privilege); 04-technologie.md, Kapitel 4 (distroless static, Nicht-Root). Read-only ist nicht erwähnt. Manuell geprüft mit `docker run --read-only` für Panel und Agent (M0, Schritt 10): beide starten und arbeiten.
- **Vorschlag:** SEC-002 bzw. Technologie 4 ergänzen: Panel- und Agent-Image laufen mit read-only Root-Dateisystem; geschrieben wird nur in die Volumes. Compose-Dateien (M5) setzen `read_only: true`. Abnahme: Integrationstest startet das Panel-Image mit read-only Root-Dateisystem.
- **Begründung:** Kostet nichts, weil das Panel schon heute nur in /data, /secrets und /backups schreibt, und erschwert das Festsetzen nach einer Kompromittierung. Ohne Aufnahme ins Konzept ist es nicht abgesichert und könnte durch eine spätere Änderung (z. B. temporäre Dateien außerhalb der Volumes) brechen.

## 4. Deep-Links mit Basispfad

- **Fundstelle:** 01-gesamtkonzept.md, NFR-003 (Basispfad); 05-abnahme-r1.md, NFR-003; 03-design.md, Kapitel 6. Umsetzung: web/vite.config.ts (`base: './'`), internal/webui. Nachverfolgt in Issue #88 (M4).
- **Vorschlag:** In Design 6.1 festlegen: Das Panel setzt beim Ausliefern von `index.html` ein `<base href>` aus `RCD_BASE_PATH` (ohne Inline-Skript, wegen SEC-009); der Router der Oberfläche nutzt denselben Basispfad. Abnahme von NFR-003 um „Neuladen eines tiefen Pfads lädt alle Assets“ ergänzen.
- **Begründung:** Mit relativen Asset-Pfaden löst der Browser bei `/servers/1` die Assets nach `/servers/assets/…` auf; das Panel antwortet dort korrekt mit 404, die Seite bleibt leer. Das betrifft jeden Betrieb, mit und ohne Basispfad, sobald es Client-Routen gibt (M4).

## 5. Wortlaut der Abnahme von AGT-001

- **Fundstelle:** 05-abnahme-r1.md, Abschnitt 7, AGT-001: „das Panel-Image enthält keinen Docker-Client“. go.mod enthält seit Commit d6498ce `github.com/moby/moby/client` als indirekte Abhängigkeit von testcontainers-go (nur Integrationstests).
- **Vorschlag:** Kriterium präzisieren: „Das Panel-Binary enthält keinen Docker-Client (geprüft mit `go version -m` bzw. `go list -deps ./cmd/panel`); Testabhängigkeiten sind ausgenommen.“
- **Begründung:** Bei einem Review von go.mod könnte das Kriterium fälschlich als verletzt gelten. Entscheidend ist, dass das ausgelieferte Panel nicht mit Docker sprechen kann; das ist über das Binary prüfbar.

## 6. CGO bei Tests

- **Fundstelle:** 04-technologie.md, Kapitel 1 („ohne CGO“) und Kapitel 4; CLAUDE.md, Abschnitt Technik. .github/workflows/ci.yml führt `go test -race` aus; der Race-Detector braucht CGO (Commit 96d2069).
- **Vorschlag:** Klarstellen: „Ausgelieferte Binaries und Images ohne CGO. Tests dürfen CGO nutzen (Race-Detector).“
- **Begründung:** Der Race-Detector findet Datenrennen in Verbindungsmanager, Ereignisbus und Agent-Hub (M2, M3), die sonst kaum auffallen. Die Binaries bleiben unberührt; die Image-Builds in der CI laufen weiter mit `CGO_ENABLED=0`.

## 7. Anforderung für Lieferkette und CI

- **Fundstelle:** 01-gesamtkonzept.md, D-040 (signierte Images mit SBOM); 04-technologie.md, Kapitel 4 (Lieferkette: syft, cosign, govulncheck, Renovate; CI: GitHub Actions); 07-plan-r1.md, M0 („CI mit Lint, Tests, govulncheck; Multi-Arch-Build und signierte Images“). Keine Anforderungs-ID; Commits tragen behelfsweise PRJ-001 (96d2069, b2403c1, 3dc0007).
- **Vorschlag:** Neue Anforderung, z. B. SEC-013 „Lieferkette“: Images für amd64 und arm64, signiert (cosign keyless) mit SBOM (syft); govulncheck als Pflicht-Check; Actions auf Commit-SHA gepinnt; Abhängigkeits-Updates per Renovate ohne Automerge. Abnahme: Review plus `cosign verify` eines Release-Images. Release-Tags erst nach Veröffentlichung des Repositories (Rekor-Log öffentlich).
- **Begründung:** Die Arbeit ist im Plan vorgesehen, aber keiner Anforderung zugeordnet; damit fehlen Issue, Abnahmekriterium und Nachweis. PRJ-001 (Open Source) passt nur lose.

## 8. Socket-Zugriff des Agent-Images

- **Fundstelle:** 04-technologie.md, Kapitel 5, offener Punkt 1 („Root oder docker-Gruppe des Hosts; wird bei der Umsetzung festgelegt“). Agent-Image in M0 als Stub mit Benutzer 65532 (Commit d6498ce).
- **Vorschlag:** In M3 entscheiden und in Technologie 4 sowie Design 5.3 festhalten. Empfehlung zur Prüfung: Nicht-Root mit `group_add` auf die GID des Docker-Sockets (in Compose per Variable); Root nur als dokumentierte Ausnahme; mit Socket-Proxy (SEC-010) entfällt der direkte Socket-Zugriff.
- **Begründung:** Die Entscheidung bestimmt Compose-Dateien, README und das Restrisiko T-022 im Bedrohungsmodell. Bis M3 hat der Stub keinen Socket-Zugriff.

## 9. Verzeichnis scripts/

- **Fundstelle:** 04-technologie.md, Kapitel 4 (Repository: cmd/panel, cmd/agent, internal, web, profiles, migrations, api/openapi.yaml). In CLAUDE.md mit Commit 3dc0007 ergänzt; enthält scripts/gh_plan.py (Commit b2403c1).
- **Vorschlag:** Technologie 4, Zeile Repository, um `scripts` (Hilfsskripte zur Projektpflege, nicht Teil der Images) und `deploy/compose` ergänzen.
- **Begründung:** Repository-Struktur in Konzept und CLAUDE.md sollen übereinstimmen; deploy/compose steht bisher nur in CLAUDE.md.
