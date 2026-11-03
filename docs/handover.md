# Übergabe

Stand: 03.11.2026, Meilenstein M4

## Stand des Repositorys

- Letzter Code-Commit auf main: bbf11d7 (Einstellungsseite mit Bezeichnung, Erklärtext und lesbaren Dauern). Danach folgt nur der Commit dieser Übergabe.
- WIP-Branch: keiner, alles ist auf main.

## Erledigt seit der letzten Übergabe (c4756ce)

| Schritt | Commits | IDs |
| --- | --- | --- |
| RCON offline zählt als Fehler bei unbekanntem Containerzustand | fef53fb | UI-003 |
| Panel-Binary ohne Testwerkzeuge (TestNoTestTools mit Gegenprobe) | 1d26898 | AGT-001 (Commit nennt fälschlich SEC-001, richtiggestellt in #50) |
| Schritt 4: Serverseite mit Kopf, Aktionen und Reitern je Rolle; Kandidat 16 | d6920be, 3750507, d0ebaa7 | UI-011, AGT-008, AGT-009 |
| Schritt 5: Admin-Konsole mit Blöcken, Zeitleiste, Spuren, Favoriten; Kandidat 17; Screenshots m4-5 | 711dd5a, c00de77, 104efec, 4ad3da9, 0473346 | CMD-001, CMD-012 bis CMD-015, UI-009 |
| #104: Minecraft-Vergleichsantworten vor dem Panel; Kandidat 18 | 9c9b337, 5711d6c | PROT-001, PROT-002, NFR-004 |
| Schritt 6: Befehlsseite für Moderator und Supporter, Katalogparameter, Spielermuster; Kandidat 19 | 8f5f2cf, 0b6f418, d86f337, 2e28064, 5048e93, 0ebaa7, c3fcb44 | CMD-003, CMD-004 (5048e93 nennt fälschlich CMD-005, richtiggestellt in #44) |
| Schritt 7: Logs-Reiter mit Live-Zeilen und logs.tail; Kandidat 20; Sprachdatei-Test | 82e794a, 384b29a, f7c3adc, 98650fb, 607dc10 | UI-009, AGT-003, AGT-008, NFR-007 |
| Schritt 8a: Benutzerverwaltung, Einladungen (Token im Fragment), Sessions beenden; Kandidaten 21, 23; Screenshots m4-8a | 3ba554a, d0a6246, cbd9d90, b3aa275, 7012004, cf826d3, d0951a7 | UI-007, UI-008, AUTH-006, AUTH-009, SEC-012 |
| Schritt 8b: Gruppen und Befehlslisten | 889a1d4 | UI-007, ROLE-004, ROLE-005, ROLE-007 |
| Optimistische Sperre: Version in der DB, atomar im UPDATE, If-Match Pflicht (428), 409 bei Konflikt; Kandidat 24 | 9937ecf, 6136364, 9e461f1, 1b57281, 0c74c7f, db99f95 | UI-007, UI-008, NFR-011 |
| Schritt 8c: Server, Servergruppen, Agents mit Kopplung; Container eines Agents; Kandidat 22 | 3f21c59, 452e2dc, 1dadc8c, 3a34ab1 | SRV-001, SRV-003, SRV-005, AGT-002, AGT-005, ROLE-002 |
| Schritt 8d: Einstellungen, Audit, eigenes Profil; Screenshots m4-8 | 2a04c94, 8c6105f | UI-008, AUTH-009, AUD-001 |
| Einstellungsseite: Bezeichnung und Erklärtext je Schlüssel aus der Sprachdatei, Dauern als „72 h“ / „15 min“ | bbf11d7 | UI-008, NFR-007 |
| Renovate #102 (Web-Abhängigkeiten) | 9309f90 | – |

Neue Ereigniscodes: `logs.tail`. Neue Ablehnungsgründe der API: `conflict` (409), `precondition_required` (428).

## Offen und Blocker

- Kandidaten 14 bis 24 stehen in docs/revision-2.4-kandidaten.md und warten auf die Revision 2.4.
- 889a1d4 hatte einen roten Integrationstest-Lauf; das Log ist nicht mehr abrufbar. Alle Läufe danach sind grün. Beim nächsten roten Lauf genauer hinsehen.
- Die volle E2E-Suite lief zuletzt vor bbf11d7 lokal grün (131); nach bbf11d7 nur settings-audit-profile und navigation (15 grün) plus make check.
- Issue #101 (/backups) folgt nach M5.

## Ausstehende CI-Läufe

- 8c6105f und alle Commits davor (außer 889a1d4, siehe oben): grün.
- bbf11d7: lief bei der Übergabe noch. Dazu der Lauf zum Commit dieser Übergabe.

## Nächster Handgriff

CI für bbf11d7 prüfen. Dann den Plan für Schritt 9 zeigen, nicht umsetzen.

Schritt 9, Handy-Ansicht (UI-012; Abnahme: bei 390 px Befehlsseite, Karten und Konsole ohne horizontales Scrollen bedienbar, Tippflächen ab 44 px, Verwaltung nur lesbar; Prüfart E2E; Design 6.5 und 6.6). Entwurf:

1. Grundgerüst: Seitenleiste wird unter `md` zum Menü (Sheet) mit gleicher Navigation; Kopf mit Menüknopf.
2. Karten: einspaltig, Statusrand bleibt; lange Namen umbrechen bzw. kürzen statt Überlauf.
3. Serverseite: Start, Stopp, Neustart ins Menü oben rechts; Reiter scrollbar ohne Seitenüberlauf.
4. Konsole: Zeitleiste ausgeblendet, Spur-Chips, Vorschläge als Chips über der Eingabe, Blöcke umbrechen.
5. Befehlsseite: Befehle als Karten untereinander, eine ausgeklappt; Spieler online als Bottom-Sheet.
6. Tippflächen: Buttons, Checkboxen, Chips und Menüeinträge mindestens 44 px auf schmalen Breiten.
7. Verwaltung mobil nur lesend: Formulare, Schnelleingabe und Aktionen unter `md` ausgeblendet, Hinweis „Am Desktop bearbeiten“. Nur Darstellung; das Backend prüft Rechte wie bisher.
8. Tests: Playwright-Projekt „mobile“ (390 × 844, Touch) mit Prüfung `scrollWidth <= clientWidth` je Seite und Rolle, Mindestgröße 44 px für sichtbare Bedienelemente, keine Schreib-Bedienelemente in der Verwaltung. Vitest für die Hilfslogik.
9. Screenshots m4-9 je Rolle bei 390 px, dann auf Freigabe warten.

Offene Fragen für den Plan: Mitteilungen (UI-012 nennt sie) gibt es in R1 noch nicht als eigene Seite, sie fallen daher heraus; ist das Abmelden im Menü oder im Kopf gewünscht; Verwaltung mobil ganz ohne Aktionen oder mit Lesen-Aktionen wie Kopieren.
