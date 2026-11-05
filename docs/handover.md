# Übergabe

Stand: 05.11.2026, Meilenstein M4, Schritt 10 (Abbruch auf Wunsch, Stand gesichert)

## Repository

- Arbeitsbranch: `m4/step10`, PR #117 offen. Letzter Arbeitsstand 4f671c4, darauf nur dieser Übergabe-Commit.
- main: 28cc8ea, sauber, CI grün. Schritt 10 ist noch nicht auf main.
- Der Branch ist fertig, `make check` war bei 4f671c4 grün. Nach main geht er nur über #117: Das Ruleset verlangt die Pflicht-Checks, und deren Lauf ist nicht fertig.
- Dieser Übergabe-Commit lief mit `--no-verify`, weil keine Tests mehr starten sollten. Er ändert nur diese Datei.

## Erledigt seit der letzten Übergabe

- Schritt 9: Kartenlisten und E2E-Prüfung auf main, #103 gemergt, Screenshots m4-9 freigegeben.
- Öffentliches Repo: Prüfung ohne Befund, Secret Scanning, Push Protection, Private Vulnerability Reporting, Ruleset, SECURITY.md, CONTRIBUTING.md, Aufbewahrung 7 Tage.
- CI in drei Stufen (#107), E2E-Filter auf `internal/` und `api/` (#111). Nur Läufe mit Stufe 2 melden die Pflicht-Checks, ein Branch-Push erfüllte sie vorher ohne Test.
- Alle 12 Dependabot-Alerts behoben (#112, #115), keiner dismissed. #108 bis #110 geschlossen.
- moby/go-archive und moby/sys/user liegen weder im Agent- noch im Panel-Binary, nur in Tests über testcontainers.
- Sporadische Tests behoben: `TestAgentThroughTheStack`, `TestHalfOpenConnection` (#116). `pull-test-images.sh` scheitert nicht mehr an `go.sum`-Änderungen.

## Unterbrochene Aufgabe: Schritt 10 (SEC-006, SEC-009)

Fertig in #117, lokal grün (161 E2E bestanden, 0 fehlgeschlagen):

- `ForeignText`: Bidi-Zeichen als `⟨U+202E⟩`, je Zeile `<bdi>`, Kopieren liefert das Original, ohne Farbcodes (so bestätigt). Kandidaten 28 und 29.
- Feindlicher Server, Container und Moderator `hmod` im Stack; `foreign.spec.ts` inklusive Befehlsseite.
- Unit-Test für die feindliche Agent-Version.
- CSP-Prüfung in jedem E2E-Test, Header-Test für alle Antwortarten des Web-Ports.
- Werkzeugleisten brechen auf dem Handy um; `mobile`-Prüfung auf Controls außerhalb des Fensters, auch bei 320 px.
- E2E-Filter um `cmd/`, `migrations/`, `profiles/` erweitert.
- Screenshots m4-10 für das Handy neu aufgenommen (17 Bilder gesamt).

Fehlt:

- Grüne CI für #117, danach Merge.
- Kurzmeldung zu den neuen Handy-Screenshots (hier nachgeholt).
- Plan für Schritt 11 zeigen, noch nicht vorgelegt.

Nächster Handgriff:

1. `gh pr checks 117`: Bei Grün `gh pr merge 117 --rebase --delete-branch`, die Pflicht-Checks müssen aus dem PR-Lauf stammen. Bei Rot zuerst reproduzieren.
2. Danach Plan für Schritt 11 zeigen, nicht umsetzen.

Vorarbeit zu Schritt 11 (E2E-Suite, ursprünglich mit Kontrastprüfung durch axe): Der CI-Job und der Test-Stack stehen.

- UI-001: Kontrastprüfung fehlt. `@axe-core/playwright` ist installiert, aber unbenutzt.
- NFR-007: Der Test mit einer Testsprache fehlt.
- Alle anderen E2E-Kriterien von M4 werden in den Specs mit ID genannt. Das ist nur per Suche geprüft, nicht je Kriterium gegen `docs/05-abnahme-r1.md`.
- In M4 sind noch 25 Issues offen; das Schließen gehört zum Abschluss.

## Offene CI-Läufe

- #117, Commit 4f671c4: PR-Lauf 41937362993 lief noch (16 grün, 1 offen: E2E shard 2), Push-Lauf grün.
- Der Push dieses Übergabe-Commits startet die CI für #117 neu; ein älterer Lauf kann dadurch abgebrochen sein. Es zählt der neueste Stand.
- Nächtlicher Lauf (Stufe 3): bisher nicht beobachtet, nur als manueller Lauf geprüft.

## Offene Fragen

- `GO-2026-5932` in `golang.org/x/crypto` 0.58.0: Unser Code ruft die Stelle nicht auf, der Fix existiert nur als Pseudo-Version. Warten auf ein Release oder Pseudo-Version nehmen? Vorschlag: warten.
- #113 und #114 (Renovate, andere Pakete) sind offen. Nach grüner CI mergen, wie üblich?
- Kandidaten 14 bis 29 warten auf die Revision 2.4; Issue #101 (/backups) folgt nach M5.
