# Kandidaten für Revision 2.2

Stand: M2 (02.11.2026). Abweichungen und Lücken, die nach der Freigabe von Dokumentversion 2.1 aufgefallen sind. Keiner der Punkte ändert das freigegebene Konzept 2.1; über die Übernahme entscheidet Rob.

| Nr. | Thema | Betroffen |
| --- | --- | --- |
| 1 | Beispielprofil in Design 7.4 auf Englisch | Design 7.4, CLAUDE.md (Oberflächentexte Englisch) |
| 2 | Hot Reload mit Polling-Fallback, Einstellung profiles.poll\_interval | CAT-003, Design 5.6, 7.1 |

## 1. Beispielprofil in Design 7.4 auf Englisch

- **Fundstelle:** 03-design.md, Kapitel 7.4 (Beschreibungen und Parameternamen deutsch, z. B. „Spieler online anzeigen“, spieler, grund); CLAUDE.md, Abschnitt Sprache (Oberflächentexte Englisch). Umsetzung: profiles/minecraft-java.yaml mit englischen Texten (Commit c379ecf); der Abnahmetest von CAT-001 importiert das deutsche Beispiel wörtlich (internal/profiles, TestDesignExample).
- **Vorschlag:** Beispiel in Design 7.4 mit englischen Beschreibungen und Parameternamen (player, reason, time), wie im ausgelieferten Profil; Spielportname game statt Spiel.
- **Begründung:** Katalogbeschreibungen und Parameternamen erscheinen in der Oberfläche; die Oberfläche ist in R1 englisch (NFR-007). Deutsche Texte kommen ab R2 über die Sprachdateien, nicht über das Profil.

## 2. Hot Reload mit Polling-Fallback, Einstellung profiles.poll\_interval

- **Fundstelle:** 01-gesamtkonzept.md, CAT-003 („bei jeder Dateiänderung … ohne Neustart“); 05-abnahme-r1.md, CAT-003 (sichtbar binnen 5 s); 03-design.md, Kapitel 5.6 und 7.1; 04-technologie.md, Kapitel 2 (fsnotify).
- **Umsetzung (M2):** Dateiereignisse über fsnotify, zusätzlich ein Abgleich alle profiles.poll\_interval (Standard 30 s) über Änderungszeit und Inhalts-Hash.
- **Vorschlag:** Design 7.1 um den Fallback ergänzen; profiles.poll\_interval (Standard 30 s, Bereich 1 s bis 1 h) in die Tabelle von Design 5.6 aufnehmen; im README darauf hinweisen, dass auf Netzlaufwerken die Abnahmegrenze von 5 s nur mit kürzerem Intervall gilt.
- **Begründung:** fsnotify liefert auf NFS- und SMB-Volumes und bei Docker Desktop mit gemounteten Host-Verzeichnissen keine Ereignisse; ohne Fallback würden Änderungen dort nie übernommen.
