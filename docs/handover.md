# Übergabe

Stand: 05.11.2026, Meilenstein M4 abgeschlossen

## Repository

- Branch: main, Arbeitsstand be01ae0, darauf nur dieser Übergabe-Commit.
- Offen: #130 (Revision 2.4, Branch docs/revision-2.4, nicht mergen vor deiner Freigabe) und #123 (Renovate, wartet auf playwright-core 1.65.0).
- Milestone M4 geschlossen, alle 27 Issues zu; M5 ist offen und noch nicht begonnen.

## Erledigt seit der letzten Übergabe

- AUTH-006 Rechteänderung: Integrationstest gegen das Panel-Image (`internal/integration/session_test.go`) und E2E mit zweitem Browser-Kontext.
- Kartenrahmen unter 3:1 belegt: Titel, Status, Fokusring im Browser gemessen.
- UI-007 Variante A: Suche für Server, Servergruppen und Agents (#129); Filter und Mehrfachauswahl nur Benutzerliste, Kandidat 31.
- Abgleichstabelle fertig gelesen; Lücken in SRV-001 und UI-007 mit Tests geschlossen.
- Screenshots m4-11 freigegeben, Review UI-001 erledigt.
- Renovate: #122, #124, #125, #126 gemergt, #127 (jsdom 31, Major) nach OK gemergt.
- 25 M4-Issues mit Beleg je Prüfart geschlossen.
- Revision 2.4 mit allen 31 Kandidaten als PR #130.

## Zusammenfassung M4

- Erledigte Anforderungen: UI-001 bis UI-004, UI-006 bis UI-009, UI-011, UI-012, CMD-001, CMD-003, CMD-012 bis CMD-015, NFR-007, SEC-009.
- Teilabnahmen aus früheren Meilensteinen: ROLE-002, AUTH-006, AUTH-009, SEC-006, SRV-001, SRV-003, AGT-005.
- Neue Ereigniscodes: logs.tail.
- Lokal zuletzt voll geprüft vor #129: 212 E2E bestanden, 0 fehlgeschlagen, 74 übersprungen (Screenshots). CI auf be01ae0 grün.

## Unterbrochene Aufgabe

Keine. Fertig ist alles aus Schritt 12. Für die Revision fehlt nur deine Freigabe.

Nächster Handgriff:

1. Nach deiner Freigabe: In #130 Status und Historienzeile auf „freigegeben“ setzen, mergen.
2. Erst auf deine Nennung: Plan für M5 zeigen, nicht umsetzen.

## Offene CI-Läufe

- #130: Lauf gestartet, Ergebnis offen.
- Dieser Übergabe-Commit startet einen Lauf auf main; es zählt der neueste Stand.
- Nächtlicher Lauf (Stufe 3): am 05.11. grün auf 4e778ee, Start von GitHub um rund 7 Stunden verzögert.

## Offene Fragen

- Revision 2.4 (#130): Freigabe?

## Weiter offen

- `GO-2026-5932` in `golang.org/x/crypto`: Wir warten auf das reguläre Release.
- #123 liegt, bis playwright-core 1.65.0 veröffentlicht ist.
- Issue #101 (/backups) folgt nach M5.
