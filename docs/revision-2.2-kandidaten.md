# Kandidaten für Revision 2.2

Stand: M2 (02.11.2026). Abweichungen und Lücken, die nach der Freigabe von Dokumentversion 2.1 aufgefallen sind. Keiner der Punkte ändert das freigegebene Konzept 2.1; über die Übernahme entscheidet Rob.

| Nr. | Thema | Betroffen |
| --- | --- | --- |
| 1 | Beispielprofil in Design 7.4 auf Englisch | Design 7.4, CLAUDE.md (Oberflächentexte Englisch) |
| 2 | Hot Reload mit Polling-Fallback, Einstellung profiles.poll\_interval | CAT-003, Design 5.6, 7.1 |
| 3 | Sichtbarkeit der Serverfelder je Rolle | Design 2.2, 6.3 |
| 4 | Änderung von Servergruppen beendet keine Sessions | AUTH-006, Design 2.1 (D-044) |
| 5 | Agent und Container am Server erst ab M3 setzbar | SRV-001, Plan R1 |

## 1. Beispielprofil in Design 7.4 auf Englisch

- **Fundstelle:** 03-design.md, Kapitel 7.4 (Beschreibungen und Parameternamen deutsch, z. B. „Spieler online anzeigen“, spieler, grund); CLAUDE.md, Abschnitt Sprache (Oberflächentexte Englisch). Umsetzung: profiles/minecraft-java.yaml mit englischen Texten (Commit c379ecf); der Abnahmetest von CAT-001 importiert das deutsche Beispiel wörtlich (internal/profiles, TestDesignExample).
- **Vorschlag:** Beispiel in Design 7.4 mit englischen Beschreibungen und Parameternamen (player, reason, time), wie im ausgelieferten Profil; Spielportname game statt Spiel.
- **Begründung:** Katalogbeschreibungen und Parameternamen erscheinen in der Oberfläche; die Oberfläche ist in R1 englisch (NFR-007). Deutsche Texte kommen ab R2 über die Sprachdateien, nicht über das Profil.

## 2. Hot Reload mit Polling-Fallback, Einstellung profiles.poll\_interval

- **Fundstelle:** 01-gesamtkonzept.md, CAT-003 („bei jeder Dateiänderung … ohne Neustart“); 05-abnahme-r1.md, CAT-003 (sichtbar binnen 5 s); 03-design.md, Kapitel 5.6 und 7.1; 04-technologie.md, Kapitel 2 (fsnotify).
- **Umsetzung (M2):** Dateiereignisse über fsnotify, zusätzlich ein Abgleich alle profiles.poll\_interval (Standard 30 s) über Änderungszeit und Inhalts-Hash.
- **Vorschlag:** Design 7.1 um den Fallback ergänzen; profiles.poll\_interval (Standard 30 s, Bereich 1 s bis 1 h) in die Tabelle von Design 5.6 aufnehmen; im README darauf hinweisen, dass auf Netzlaufwerken die Abnahmegrenze von 5 s nur mit kürzerem Intervall gilt.
- **Begründung:** fsnotify liefert auf NFS- und SMB-Volumes und bei Docker Desktop mit gemounteten Host-Verzeichnissen keine Ereignisse; ohne Fallback würden Änderungen dort nie übernommen.

## 3. Sichtbarkeit der Serverfelder je Rolle

- **Fundstelle:** 03-design.md, Kapitel 2.2 (/servers: „RCON-Admin; übrige lesend im Scope“) und 6.3 (Reiter Übersicht). Umsetzung: internal/servers (Commit von M2 Schritt 5).
- **Umsetzung (M2):** RCON-Admin und Server-Admin sehen Protokoll, Host, Port, Zustand der Zugangsdaten (ok, missing, reenter), Agent, Container und Spielports; Moderatoren und Supporter nur Name, Profil und die Servergruppen in ihrem Scope. Das Passwort gibt die API nie zurück.
- **Vorschlag:** In Design 2.2 festhalten, welche Felder welche Rolle sieht.
- **Begründung:** Host und Port des RCON-Zugangs sind interne Betriebsdaten; Moderatoren brauchen sie nicht (Least Privilege, SEC-002).

## 4. Änderung von Servergruppen beendet keine Sessions

- **Fundstelle:** 03-design.md, Kapitel 2.1 (Zeile Rechte, D-044).
- **Umsetzung (M2):** Ändert der RCON-Admin die Server einer Servergruppe oder löscht er sie, wirken Scopes und Grants auf diese Gruppe ab der nächsten Anfrage; Sessions bleiben, weil sich keine Zuweisung eines Benutzers geändert hat (wie bei Listenänderungen).
- **Vorschlag:** In Design 2.1 neben den Listen nennen.
- **Begründung:** Sonst würde jede Pflege einer Servergruppe alle betroffenen Admins und Moderatoren abmelden.

## 5. Agent und Container am Server erst ab M3 setzbar

- **Fundstelle:** 01-gesamtkonzept.md, SRV-001 („Agent und Container“); 07-plan-r1.md, Kapitel 3 (SRV-001: Abnahme M4).
- **Umsetzung (M2):** Die Felder agent\_id und container\_id sind vorhanden und werden gelesen, aber erst mit den Agents in M3 schreibbar.
- **Vorschlag:** Im Plan R1 bei SRV-001 vermerken.
- **Begründung:** Ohne Agents gibt es nichts Gültiges auszuwählen; freie Werte müssten in M3 bereinigt werden.
