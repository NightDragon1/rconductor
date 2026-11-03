# Übergabe

Stand: 03.11.2026, Meilenstein M4

## Stand des Repositorys

- Letzter Code-Commit auf main: 274f6b1 (Screenshots M4 Schritt 3). Danach folgt nur der Commit dieser Übergabe.
- WIP-Branch: keiner, alles ist auf main.

## Erledigt seit der letzten Übergabe

| Schritt | Commit | IDs |
| --- | --- | --- |
| Navigation nach Design 6.2, Supporter-Schalter für Moderatoren | 7d11934 | UI-011, ROLE-006 |
| Screenshots m4-2 je Rolle (freigegeben) | d54b7c6 | UI-011 |
| Router-Guards nur mit Session, Grundsatz in CLAUDE.md | 4605684 | UI-011, ROLE-006 |
| 403 für Rollen ohne Ressourcenart (Benutzer, Einladungen, Gruppen, Befehlslisten, Servergruppen) | fbdf3a6 | ROLE-006, ROLE-008 |
| Rechtematrix: alle OpenAPI-Operationen × 8 Rollen, WebSocket-Themen | 73276f2 | ROLE-003, ROLE-006, ROLE-008, AUTH-010 |
| Revision 2.4: Kandidaten 14 (403/404, Rolle vor Lookup) und 15 (Session.ui) | f9bf0ad | ROLE-006, ROLE-008, UI-011 |
| Serveraktionen: Rolle vor dem Server prüfen | 907b59c | AGT-009, ROLE-008 |
| 403 verrät keine Existenz (gleiche Antwort bei existierender und fehlender ID) | f7688b3 | ROLE-008 |
| Zeiten der Agent-Daten in UTC | c92ee4d | NFR-005 |
| Spielerliste sofort beim ersten Betrachter | a7d3dd9 | UI-004, UI-006 |
| E2E-Stack mit Agent, vier Servern, zwei Servergruppen, gesperrtem Benutzer | c53b68c | UI-003, UI-004, UI-006 |
| Schritt 3: Landingpages, Gesundheitsübersicht, Server-Karten, live über servers.overview | cfd92ff | UI-002, UI-003, UI-004, UI-006 |
| Screenshots m4-3 | 274f6b1 | UI-002, UI-003, UI-004, UI-006 |

Neue Ereigniscodes: keine.

## Offen und Blocker

- Screenshots m4-3 warten auf Freigabe. Erst danach geht es mit Schritt 4 weiter.
- Zur Prüfung bei der Freigabe:
  - „Up“ heißt: Der Server läuft und antwortet über RCON.
  - Als RCON-Fehler zählt nur ein Server, der antworten sollte; ein gestoppter Container zählt nicht.
  - Die Hinweis-Links zeigen auf `/admin/agents#<id>` und `/admin/users#<id>`; diese Seiten kommen erst in Schritt 8.
  - Die Serverseite ist bis Schritt 4 ein Platzhalter.
- Der In-Process-Agent im E2E-Stack ist aus Schritt 11 vorgezogen, weil die E2E-Tests der Karten ihn brauchen.
- Kandidaten 14 und 15 stehen in docs/revision-2.4-kandidaten.md und warten auf die Revision 2.4.
- Issue #101 (/backups) folgt nach M5.

## Ausstehende CI-Läufe

- 274f6b1 und alle Commits davor: grün.
- Ausstehend ist nur der Lauf zum Commit dieser Übergabe.

## Nächster Handgriff

Freigabe der Screenshots m4-3 abwarten. Danach Schritt 4: Serverseite mit Kopf und Status, Aktionen nur für Admins und nur mit Agent, Reiter je Rolle nach Design 6.3 (UI-011, AGT-008). Vorher kurz den Plan für Schritt 4 zeigen.
