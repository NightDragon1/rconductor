# Kandidaten für Revision 2.4

Stand: Beginn M4 (03.11.2026). Abweichungen, Lücken und Festlegungen, die nach der Freigabe von Dokumentversion 2.3 aufgefallen sind. Keiner der Punkte ändert das freigegebene Konzept 2.3; über die Übernahme entscheidet Rob.

| Nr. | Thema | Betroffen |
| --- | --- | --- |
| 1 | /backups erst nach M5 | Design 2.2, 6.2, Plan R1 |

## 1. /backups erst nach M5

- **Fundstelle:** 03-design.md, Kapitel 2.2 (/backups mit GET und POST, RCON-Admin, R1) und 6.2 (Verwaltung: Einstellungen, Backups, R1); 01-gesamtkonzept.md, NFR-008 (Backups, Abnahme ohne API); 07-plan-r1.md (kein Meilenstein nennt die API).
- **Befund (Planung M4):** Die automatischen Backups laufen seit M0 (NFR-008 erfüllt); die API zum Anzeigen und Auslösen und die Seite dazu deckt kein Issue ab.
- **Entscheidung (Rob, 03.11.2026):** /backups folgt nach M5, nicht in R1 1.0.0.
- **Vorschlag:** In Design 2.2 und 6.2 die Release-Angabe für /backups und die Backup-Seite auf nach M5 setzen; im Plan R1 vermerken.
- **Begründung:** Backups entstehen automatisch und lassen sich über das Volume /backups prüfen; die Ansicht ist Komfort, kein Muss für 1.0.0.
