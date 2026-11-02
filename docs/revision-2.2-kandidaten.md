# Kandidaten für Revision 2.2

Stand: M2 (02.11.2026). Abweichungen und Lücken, die nach der Freigabe von Dokumentversion 2.1 aufgefallen sind. Keiner der Punkte ändert das freigegebene Konzept 2.1; über die Übernahme entscheidet Rob.

| Nr. | Thema | Betroffen |
| --- | --- | --- |
| 1 | Beispielprofil in Design 7.4 auf Englisch | Design 7.4, CLAUDE.md (Oberflächentexte Englisch) |
| 2 | Hot Reload mit Polling-Fallback, Einstellung profiles.poll\_interval | CAT-003, Design 5.6, 7.1 |
| 3 | Sichtbarkeit der Serverfelder je Rolle | Design 2.2, 6.3 |
| 4 | Änderung von Servergruppen beendet keine Sessions | AUTH-006, Design 2.1 (D-044) |
| 5 | Agent und Container am Server erst ab M3 setzbar | SRV-001, Plan R1 |
| 6 | Eigener Source-RCON-Adapter statt gorcon/rcon | Technologie 2, PROT-001 |
| 7 | RCON-Verbindung: Prüfung im Leerlauf und Backoff höchstens 5 s | SRV-004, PROT-002, Design 1.1 |
| 8 | Zeitüberschreitung eines Befehls und Audit vor dem Senden | Design 3.3, 3.5, 4.2 |

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

## 6. Eigener Source-RCON-Adapter statt gorcon/rcon

- **Fundstelle:** 04-technologie.md, Kapitel 2 (Source RCON: gorcon/rcon hinter eigener Adapter-Schnittstelle; „Austausch ohne Umbau möglich“); 05-abnahme-r1.md, PROT-001 (Antworten über 4 KB). Umsetzung: internal/rcon/source.
- **Befund (M2):** gorcon/rcon v1.4.0 liest je Befehl genau ein Paket mit fester Paket-ID, kennt kein Endmarker-Verfahren und begrenzt Befehle künstlich auf 1000 Zeichen. Antworten über 4 KB, die Minecraft auf mehrere Pakete ohne Endmarke verteilt, kämen unvollständig an.
- **Umsetzung (M2, nach Vorgabe):** eigener Adapter hinter der vorgesehenen Schnittstelle: nach jedem Befehl ein leeres Paket als Endmarker, Sammeln bis zu dessen Antwort, Pakete mit fremder ID werden verworfen; Grenzen 64 KiB je Paket und 1 MiB je Antwort (SEC-006). gorcon/rcon wird nicht eingebunden.
- **Vorschlag:** Technologie 2, Zeile Source RCON: „eigener Adapter (Endmarker-Verfahren)“ statt gorcon/rcon.
- **Begründung:** Erfüllt PROT-001 ohne Fremdcode; der Austausch war über die Adapter-Schnittstelle ohnehin vorgesehen.

## 7. RCON-Verbindung: Prüfung im Leerlauf und Backoff höchstens 5 s

- **Fundstelle:** 05-abnahme-r1.md, SRV-004 (Wechsel binnen 10 s sichtbar) und PROT-002 (Panel verbindet sich nach Spielneustart selbst); 03-design.md, Kapitel 1.1 (Backoff 1 s, Faktor 1,6, ±20 %, höchstens 120 s – für den Agent) und 4.6 (RCON-Erreichbarkeit „bei Verbindungsereignis“). Umsetzung: internal/connmgr, internal/rcon/source.
- **Umsetzung (M2):** Eine ruhende Verbindung wird alle 2 s geprüft, ohne einen Befehl an das Spiel zu senden (kurzer Lesevorgang: Zeitüberschreitung heißt verbunden, Verbindungsende heißt offline). Nach einem Abbruch wird sofort neu verbunden, danach mit Backoff wie Design 1.1, aber höchstens 5 s.
- **Vorschlag:** In Design 4.6 bzw. einem neuen Abschnitt zum Verbindungsmanager festhalten; Design 1.1 ausdrücklich nur auf den Agent beziehen.
- **Begründung:** Ohne Prüfung im Leerlauf fiele ein gestoppter Spielserver erst beim nächsten Befehl auf; mit 120 s Backoff wäre der Wechsel nach einem Neustart nicht binnen 10 s sichtbar.

## 8. Zeitüberschreitung eines Befehls und Audit vor dem Senden

- **Fundstelle:** 03-design.md, Kapitel 3.3 (Schritt 10: Audit-Eintrag vor dem Senden; Schritt 12: Ergebnis im Audit), 3.5 (keine Antwort für eine Zeitüberschreitung) und 4.2 (audit\_log.result: ok, denied, error, timeout). Umsetzung: internal/commands.
- **Umsetzung (M2):** Der Eintrag vor dem Senden trägt result error mit reason\_code not\_answered und wird mit der Antwort auf ok, timeout oder error gesetzt; so bleibt ein Absturz zwischen Senden und Antwort als nicht beantwortet sichtbar. Eine Zeitüberschreitung (10 s) beantwortet die API mit 504 ohne Grund-Code; Log cmd.timeout, Audit timeout.
- **Vorschlag:** In Design 3.5 eine Zeile 504 timeout ergänzen (Grund-Code timeout) und in 3.3 festhalten, mit welchem Ergebnis der Eintrag vor dem Senden entsteht.
- **Begründung:** Beides ist für Oberfläche und Auswertung des Audits nötig und bisher nicht festgelegt.
