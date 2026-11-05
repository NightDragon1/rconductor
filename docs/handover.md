# Übergabe

Stand: 05.11.2026, Meilenstein M4, Schritt 11 (Abbruch auf Wunsch, Stand gesichert)

## Repository

- Branch: main. Letzter Arbeitsstand 6192463 (#121 gemergt), darauf nur dieser Übergabe-Commit.
- Keine offenen PRs, keine Arbeitsbranches.
- Dieser Übergabe-Commit lief mit `--no-verify`, weil keine Tests mehr starten sollten, und direkt auf main über den Owner-Bypass. Er ändert nur diese Datei.

## Erledigt seit der letzten Übergabe

- Schritt 10 auf main (#117), Screenshots m4-10 freigegeben.
- Renovate-Regel in CLAUDE.md: Patch, Minor und Digest nach grüner CI selbst mergen, Major vorher melden. #113, #114, #118, #119 gemergt.
- `TestPairAndReconnect` behoben (#120).
- Schritt 11 auf main (#121), PR-Lauf 41986893780 grün:
  - `internal/logtest` mit `Wait`, `WaitLine`, `WaitCount` (Timeout 10 s) ersetzt einmalige Lesestellen und eigene Warteschleifen in `internal/`. Abwesenheitsprüfungen lesen direkt, nach dem Warten. `internal/logging` bleibt ausgenommen, dort wird der Schreiber selbst getestet.
  - UI-001: `web/e2e/contrast.spec.ts`, axe auf allen Seiten je Rolle sowie Dialog und Menüs, hell und dunkel.
  - Nicht-Text-Kontrast 3:1: `web/src/lib/contrast.test.ts` prüft die Farbwerte aus `index.css` je Theme; ein zu schwacher Wert lässt den Test nachweislich scheitern.
  - Behoben: gestoppte Karten (`opacity-70`, 3,5 bis 4,0:1, jetzt 85), roter Button im dunklen Design (2,76:1, neuer Token `--danger-text`), Rahmen von Eingabefeldern und Outline-Buttons (1,2 bis 1,4:1, neuer Token `--control-border`).
  - NFR-007: Build `--mode language`, Stack `language`, `web/e2e/language.spec.ts` über alle Seiten, Dialoge, Menüs und Fehlermeldung, mit Gegenbeweis. Kein fester Oberflächentext gefunden.
  - Screenshots `docs/screenshots/m4-11/` (6 Bilder). Neue Abhängigkeiten und Ereigniscodes: keine.

## Kleinste Kontrastwerte (Nicht-Text)

| Teil | Dunkel | Hell |
| --- | --- | --- |
| Symbole | 6,03 | 4,40 |
| Rahmen von Controls | 3,45 | 3,30 |
| Fokusring | 9,04 | 6,23 |
| Statuspunkte | 6,03 | 4,40 |
| Gruppenrahmen | 6,13 | 4,38 |

Kartenrahmen (`--border`, 1,2 bis 1,4:1) sind ausgenommen, siehe Offene Fragen.

## Abgleich der E2E-Kriterien (docs/05-abnahme-r1.md)

Testkörper gegen den Wortlaut gelesen.

| Kriterium | Ergebnis | Spec |
| --- | --- | --- |
| NFR-003 | Abnahme in M5 (Plan Kapitel 3); Projekt `basepath` läuft | delivery |
| NFR-007 | erfüllt, neu | language |
| SEC-002 | Abnahme in M5 | – |
| SEC-006 | erfüllt | foreign, server, commands |
| SEC-009 | erfüllt | delivery, fixtures |
| AUTH-006 | Abmelden neu, Sperren vorhanden; Rechteänderung siehe Offene Fragen | auth, users |
| AUTH-009 | Pflichtfeld und Eindeutigkeit neu | invite, users, settings-audit-profile |
| ROLE-002 | Einstellungen ergänzt | servers-admin |
| SRV-001, SRV-003 | erfüllt | servers-admin |
| CMD-001, CMD-012 bis CMD-015 | erfüllt | console |
| CMD-003 | erfüllt | commands |
| AGT-005 | erfüllt, „nur RCON-Admin“ über guards | servers-admin, guards |
| UI-001 | erfüllt, Review-Anteil steht aus | contrast, navigation |
| UI-002, UI-004, UI-006 | erfüllt | overview |
| UI-003 | aktive Benutzer ergänzt | overview |
| UI-007, UI-008 | erfüllt | users, invite, groups, settings-audit-profile |
| UI-011 | erfüllt | navigation, server |
| UI-012 | erfüllt | mobile |

Nicht einzeln gelesen, nur Titel und ID: SRV-001 ab Anlegen, CMD-003, UI-007, UI-008, UI-011, UI-012.

## Unterbrochene Aufgabe: Schritt 11 (UI-001, NFR-007)

Fertig: alles aus dem Plan, auf main, CI grün.

Fehlt:

- Volle E2E-Suite und Integrationstests lokal: Lauf abgebrochen, kein Ergebnis. Die CI hat beides für denselben Stand grün geprüft.
- Die oben genannten Kriterien einzeln gegen ihre Testkörper lesen.
- Review-Anteil UI-001: „keine verschiebbaren Widgets“ belegt nur die Codesuche (kein dnd, kein draggable in `web/src`).
- Screenshot-OK zu m4-11.

Nächster Handgriff:

1. Die Antworten auf die offenen Fragen einarbeiten.
2. Restliche Kriterien lesen, Tabelle ergänzen.
3. Plan für Schritt 12 (Abschluss, 25 offene M4-Issues) zeigen, nicht umsetzen.

## Offene CI-Läufe

- main 6192463: Der Push-Lauf läuft nach dem Merge von #121, Ergebnis offen. Der PR-Lauf desselben Stands war grün.
- Dieser Übergabe-Commit startet einen weiteren Lauf; es zählt der neueste Stand.
- Nächtlicher Lauf (Stufe 3): bisher nicht beobachtet, nur als manueller Lauf geprüft.

## Offene Fragen

- Screenshots m4-11: OK?
- Kartenrahmen: WCAG 1.4.11 verlangt 3:1 nur für Rahmen, die ein Control erkennbar machen. Vorschlag: Karten und Tabellen bleiben bei `--border`.
- AUTH-006: Rechteänderung ist nur im Go-Test belegt (`internal/users`, gefälschter Session-Widerruf), nicht als Integrationstest oder E2E. Der Plan nennt für E2E nur das Abmelden. Reicht das, oder ergänzen?
- WCAG 1.4.11 steht nicht in `docs/`. Als Kandidat für die Revision 2.4 aufnehmen?

## Weiter offen

- `GO-2026-5932` in `golang.org/x/crypto`: Wir warten auf das reguläre Release.
- Kandidaten 14 bis 29 warten auf die Revision 2.4; Issue #101 (/backups) folgt nach M5.
