# Kandidaten für Revision 2.4

Stand: Beginn M4 (03.11.2026). Abweichungen, Lücken und Festlegungen, die nach der Freigabe von Dokumentversion 2.3 aufgefallen sind. Keiner der Punkte ändert das freigegebene Konzept 2.3; über die Übernahme entscheidet Rob. Nr. 2 bis 13 sind von Rob für M4 entschieden (Freigabe des M4-Plans, 03.11.2026).

| Nr. | Thema | Betroffen |
| --- | --- | --- |
| 1 | /backups erst nach M5 | Design 2.2, 6.2, Plan R1 |
| 2 | Startzeit und Restart-Zahl im Agent-Protokoll, Restart-Schleifen | UI-003, UI-004, Design 1.5 |
| 3 | servers.overview mit Laufzeitdaten aller Server im Scope | Design 2.3, UI-002 bis UI-006 |
| 4 | Laufzeitdaten im Server-Objekt der REST-API | Design 2.2, UI-004 |
| 5 | Erlaubte Befehle je Server: GET /servers/{id}/commands | Design 2.2, CMD-003, CMD-015 |
| 6 | Favoriten je Benutzer im Backend | CMD-001, Design 2.2, 4.2 |
| 7 | Einstellung ui.shortcuts\_enabled, für alle lesbar über /auth/session | UI-008, Design 5.6, 2.2 |
| 8 | Rückgängig im Browser mit 5 s Verzögerung | UI-008, Design 6.1 |
| 9 | Farbcodes in R1 entfernen | SEC-006, CMD-017, Design 7.2 |
| 10 | Mitteilungen in UI-012 erst mit R2 | UI-012, MSG-\* |
| 11 | Spielversion auf Karten erst mit R2 | UI-004, UI-006, AGT-006 |
| 12 | CMD-001 verweist nur auf CMD-012 bis CMD-015 in R1 | CMD-001, CMD-016, CMD-017 |
| 13 | shadcn/ui als gekennzeichneter Fremdcode | PRJ-002, Technologie 3 |

## 1. /backups erst nach M5

- **Fundstelle:** 03-design.md, Kapitel 2.2 (/backups mit GET und POST, RCON-Admin, R1) und 6.2 (Verwaltung: Einstellungen, Backups, R1); 01-gesamtkonzept.md, NFR-008 (Backups, Abnahme ohne API); 07-plan-r1.md (kein Meilenstein nennt die API).
- **Befund (Planung M4):** Die automatischen Backups laufen seit M0 (NFR-008 erfüllt); die API zum Anzeigen und Auslösen und die Seite dazu deckt kein Issue ab.
- **Entscheidung (Rob, 03.11.2026):** /backups folgt nach M5, nicht in R1 1.0.0.
- **Vorschlag:** In Design 2.2 und 6.2 die Release-Angabe für /backups und die Backup-Seite auf nach M5 setzen; im Plan R1 vermerken.
- **Begründung:** Backups entstehen automatisch und lassen sich über das Volume /backups prüfen; die Ansicht ist Komfort, kein Muss für 1.0.0.

## 2. Startzeit und Restart-Zahl im Agent-Protokoll

- **Fundstelle:** 03-design.md, Kapitel 1.5 (snapshot: Status, Ports, Volumes); 01-gesamtkonzept.md, UI-003 (Restart-Schleifen), UI-004 (letzter Restart).
- **Entscheidung (M4, F1):** Container im snapshot und in container.event tragen started\_at und restart\_count. Die Erweiterung ist additiv (Minor-Version, neue Fähigkeit); ein Agent ohne die Felder bleibt gültig, die Karten blenden die Werte dann aus. Als Restart-Schleife gelten mindestens 3 Starts binnen 10 min.
- **Vorschlag:** Design 1.5 um die Felder ergänzen, Design 6 um die Regel für Restart-Schleifen.

## 3. servers.overview mit Laufzeitdaten

- **Fundstelle:** 03-design.md, Kapitel 2.3 (servers.overview); Grenze von 16 Abos je Socket (Design 5.6).
- **Entscheidung (M4, F2):** servers.overview liefert für alle Server im Scope Status, Containerzustand, CPU und RAM mit Kennzeichen veraltet sowie die Spielerzahl; Agent-Daten nur für RCON-Admin und Server-Admin (UI-006). Solange die Übersicht abonniert ist, gelten ihre Server als angesehen (Spielerliste alle 60 s, Gesamtkonzept 4.6).
- **Vorschlag:** Design 2.3 entsprechend ergänzen.

## 4. Laufzeitdaten im Server-Objekt

- **Entscheidung (M4, F3):** GET /servers und /servers/{id} liefern runtime (Containerzustand, Startzeit, Restarts, letzte Messwerte mit Zeit und veraltet, Spielerzahl) als Startwert für Karten und Kopf; Agent-Teile nur für Admins.
- **Vorschlag:** Design 2.2, Zeile /servers.

## 5. Erlaubte Befehle je Server

- **Entscheidung (M4, F4):** GET /servers/{id}/commands liefert die Pfade, die der Aufrufer auf dem Server senden darf, mit den Katalogparametern des Profils; für Admins den Katalog des Profils (Autovervollständigung, CMD-015).
- **Vorschlag:** Design 2.2, Zeile /servers/{id}/commands um GET ergänzen.

## 6. Favoriten je Benutzer im Backend

- **Entscheidung (M4, F5):** Favoriten (Server) je Benutzer im Backend, PUT /me/favorites, damit sie auf allen Geräten gelten.
- **Vorschlag:** Design 2.2 und 4.2 (Tabelle oder Feld für Favoriten).

## 7. Einstellung ui.shortcuts\_enabled

- **Fundstelle:** 03-design.md, Kapitel 6.1 (Tastaturkürzel nur global durch den RCON-Admin aktivierbar) und 5.6 (kein Schlüssel); /settings lesen nur RCON-Admins.
- **Entscheidung (M4, F6):** Einstellung ui.shortcuts\_enabled, Standard aus; für alle Benutzer lesbar über GET /auth/session (Feld ui).
- **Vorschlag:** Zeile in Design 5.6, Hinweis in 2.2 bei /auth/session.

## 8. Rückgängig im Browser

- **Fundstelle:** 03-design.md, Kapitel 6.1 (Bestätigung nur bei destruktiven Aktionen, sonst Rückgängig); UI-008.
- **Entscheidung (M4, F8):** Nicht destruktive Änderungen sendet die Oberfläche erst nach 5 s; bis dahin lassen sie sich rückgängig machen. Beim Verlassen der Seite (pagehide) werden offene Änderungen sofort gesendet. Löschen und Widerruf bestätigt ein Dialog.
- **Vorschlag:** Design 6.1, Zeile Bestätigung.

## 9. Farbcodes in R1 entfernen

- **Fundstelle:** 01-gesamtkonzept.md, SEC-006 (Farbcodes nur in eine feste Liste erlaubter Stile) gegenüber CMD-017 (Farbcodes, R2) und Design 7.2 (formatting, R2).
- **Entscheidung (M4, W1):** In R1 entfernt die Oberfläche Farbcodes (z. B. Minecraft-§-Codes) vor der Anzeige; es entsteht nie ein Stil. Die Übersetzung in erlaubte Stile kommt mit CMD-017 in R2.
- **Vorschlag:** SEC-006 und Abnahme SEC-006 auf R1 „Farbcodes werden entfernt“ fassen, die Stilliste an CMD-017 binden.

## 10. Mitteilungen in UI-012 erst mit R2

- **Fundstelle:** UI-012 (R1: Karten, Befehlsseite, Konsole und Mitteilungen mobil); Design 6.2 (Mitteilungen R2).
- **Entscheidung (M4, W2):** Mitteilungen entfallen in R1; UI-012 gilt in R1 für Karten, Befehlsseite und Konsole.

## 11. Spielversion auf Karten erst mit R2

- **Fundstelle:** UI-004 und UI-006 (Spielversion); AGT-006 (R2); Gesamtkonzept 4.6 (Spielversion ab R2, Revision 2.3).
- **Entscheidung (M4, W3):** Die Version entfällt in R1 nach der Regel „Felder ohne Quelle ausgeblendet“.

## 12. CMD-001 in R1 mit CMD-012 bis CMD-015

- **Fundstelle:** CMD-001 („gemäß CMD-012 bis CMD-017“); CMD-016 und CMD-017 sind R2.
- **Entscheidung (M4, W4):** In R1 gelten für die Konsole CMD-012 bis CMD-015.

## 13. shadcn/ui als gekennzeichneter Fremdcode

- **Fundstelle:** PRJ-002 (kein übernommener Fremdcode außer deklarierten Abhängigkeiten); 04-technologie.md, Kapitel 3 (shadcn/ui, Quellcode im Repository).
- **Entscheidung (M4):** shadcn/ui-Komponenten werden ins Repository kopiert. Jede kopierte Datei trägt im Kopf den MIT-Hinweis und SPDX-License-Identifier: MIT mit Herkunft, THIRD\_PARTY\_NOTICES nennt sie, und der SPDX-Check erlaubt MIT nur für diese Dateien.
- **Vorschlag:** PRJ-002 um „kopierte Komponenten mit kompatibler Lizenz, gekennzeichnet“ ergänzen; Technologie 3 verweist auf THIRD\_PARTY\_NOTICES.
- **Begründung:** shadcn/ui ist eine Vorlage zum Kopieren, keine Abhängigkeit; ohne Kennzeichnung wäre es Fremdcode im Sinne von PRJ-002.


## 14. Ablehnung nach Rolle vor dem Objekt

- **Fundstelle:** Design 2.2 (Rollen je Ressource), 3.1 (Standard ist Ablehnung; Server außerhalb des Scopes 404), 3.5 (403 forbidden); offen war der Status für Rollen, die eine Ressource gar nicht nutzen.
- **Entscheidung (M4, Rechtematrix):**
  - Darf eine Rolle die Ressourcenart nicht nutzen, antwortet das Panel mit 403 forbidden.
  - Darf sie sie nutzen und liegt das Objekt außerhalb ihres Scopes, antwortet es mit 404 not\_found.
  - Die Rollenprüfung kommt immer vor dem Objekt-Lookup; für eine abgelehnte Rolle sind die Antworten bei existierender und nicht existierender ID gleich.
- **Vorschlag:** Design 3.1 und 3.5 um diese Regel ergänzen.

## 15. Supporter-Freigabe in der Session

- **Fundstelle:** Design 2.2 (/auth/session), 6.2 (Navigation je Rolle); UI-011, ROLE-006.
- **Entscheidung (M4, Schritt 2):** Session.ui enthält zusätzlich supporters\_enabled (globaler Schalter) und supporters\_allowed (Freigabe des Moderators), damit Navigation und Router-Guards allein aus der Session entscheiden. Über Rechte entscheidet weiter nur das Backend.
- **Vorschlag:** Design 2.2 bei /auth/session um die Felder ergänzen.

## 16. Neustart ohne Agent in der Oberfläche

- **Fundstelle:** Design 6.3 (Aktionen im Kopf nur, wenn der Server einem Agent zugeordnet ist) gegenüber AGT-009 (R1, Kann: ohne Agent Neustart per stop-Befehl und Restart-Policy unless-stopped) und Design 2.2 (/servers/{id}/actions).
- **Entscheidung (M4, Schritt 4, W6 B):** Ohne Agent zeigt der Kopf für RCON-Admin und Server-Admin im Scope nur „Neustart über RCON (stop-Befehl)“. Der Bestätigungsdialog sagt: Der Server wird über den stop-Befehl beendet. Er startet nur neu, wenn ein Neustart-Mechanismus greift (z. B. Docker restart: always oder unless-stopped; on-failure greift bei sauberem Beenden nicht). Start und Stopp gibt es nur mit Agent.
- **Vorschlag:** Design 6.3, Absatz zu den Aktionen, um diesen Fall ergänzen.

## 17. Kopieren ohne sicheren Kontext

- **Fundstelle:** D-011 (kein HTTPS für die Weboberfläche, TLS am Reverse Proxy) gegenüber CMD-012 (Blöcke kopierbar) und dem Verlauf (Design 6.3). Browser bieten navigator.clipboard nur in einem sicheren Kontext (https oder localhost), nicht über http://lan-ip.
- **Entscheidung (M4, Schritt 5):** Ohne Clipboard-API markiert der Kopieren-Knopf den Text und zeigt den Hinweis „Press Ctrl+C“. E2E-Test über http mit einem Namen statt localhost.
- **Vorschlag:** Design 6.1 um eine Zeile „Kopieren“ ergänzen; Hinweis in der Betriebsdoku, dass HTTPS über den Reverse Proxy das direkte Kopieren ermöglicht.

## 18. Betriebshinweis: parallele RCON-Clients bei Minecraft

- **Fundstelle:** README (Betriebsdoku zu NFR-004; eine Anforderung DOC-001 gibt es im Gesamtkonzept nicht); PROT-002 (eine Verbindung je Spieleserver). Beleg: Issue #104.
- **Befund:** Minecraft sammelt die RCON-Ausgabe aller Verbindungen an einer gemeinsamen Stelle. Senden zwei Verbindungen gleichzeitig, bekommt eine die Ausgabe beider oder eine leere Antwort. Das Panel verursacht die Vermischung nicht (eine Verbindung, Befehle nacheinander), ist aber von ihr betroffen, sobald ein zweiter RCON-Client parallel sendet, z. B. alte itzg/rcon-Container, Backup-Container wie itzg/mc-backup (save-off, save-all), Bots oder rcon-cli-Skripte.
- **Vorschlag:** Abschnitt im README:
  - Alte RCON-Container nach dem Umzug auf RCONductor abschalten.
  - Backup-Läufe als bekannte Ursache für vereinzelt falsche oder leere Antworten nennen.
  - Verweis auf #104.

## 19. Prüfung der Katalogparameter im Backend (R1)

- **Fundstelle:** Design 7.3 (Spalte „Prüfung im Backend“ je Parametertyp) gegenüber Design 3.3 Schritt 7 („Argument-Einschränkung folgt in R2“) und CMD-009 (R2).
- **Entscheidung (M4, Schritt 6, W7 B):** Für Moderator und Supporter prüft das Backend in R1 Befehle mit Katalogeintrag gegen die Katalogparameter des Spielprofils:
  - player ein Spielername: passend zum neuen Profilfeld syntax.player_pattern (RE2, ganzer Name; Minecraft Java: ^[A-Za-z0-9_]{1,16}$). Ohne Muster werden mindestens ein führendes @ (Selektoren wie @a, @e[type=player]), Anführungszeichen und Steuerzeichen abgelehnt;
  - choice nur die genannten Werte (Groß-/Kleinschreibung laut Profil);
  - number ganzzahlig im Bereich min/max;
  - text nur als letzter Parameter (Rest der Zeile);
  - Pflichtparameter vorhanden.

  Bei Abweichung antwortet das Backend mit 422 invalid und dem Parameternamen im Detail; der Audit-Eintrag ist cmd.denied. Maßgeblich ist der längste Katalogpfad, mit dem der Befehl beginnt. Weitere Tokens nach den Katalogparametern sind nur erlaubt, wenn der letzte Parameter vom Typ text ist; sonst 422 invalid mit „unexpected argument“ (z. B. time set day extra, whitelist add x y). Befehle ohne Katalogeintrag und die freie Eingabe von RCON-Admin und Server-Admin bleiben unverändert. Die Befehlsseite prüft nach denselben Regeln; GET /servers/{id}/commands liefert dafür player_pattern mit.
- **Abgrenzung zu CMD-004:** CMD-004 regelt den Pfad (ganze Tokens am Anfang), nicht die Form der Argumente. Das Abnahmekriterium „Mit Eintrag whitelist add sind whitelist add x und whitelist add x y erlaubt“ gilt für den Pfadabgleich; mit Katalogeintrag lehnt die Parameterprüfung whitelist add x y für Moderator und Supporter ab.
- **Abgrenzung zu CMD-009 (R2):** Dort geht es um Einschränkungen je Listeneintrag, die ein Server-Admin festlegt, z. B. nur bestimmte Spielernamen oder Werte für einen Eintrag einer Befehlsliste. Die R1-Prüfung kennt nur die festen Parameter aus dem Spielprofil und gilt für alle Listen gleich.
- **Vorschlag:** Design 3.3 um den Schritt „7b: Katalogparameter (Moderator, Supporter)“ ergänzen; Design 7.2 um das Feld syntax.player_pattern; Design 7.3 Spalte „Prüfung im Backend“ an diese Regeln angleichen und für Einschränkungen je Listeneintrag auf CMD-009 verweisen; Abnahme CMD-004 auf „Pfadabgleich“ präzisieren.

## 20. Rückwirkende Logs über logs.tail

- **Fundstelle:** Gesamtkonzept 5 (Logs: „Live- und rückwirkende Container-Logs“); Design 1.5 (logs.tail im Agent-Protokoll); Design 2.2 ohne REST-Endpunkt dafür; Design 8.3 (Ereigniscodes).
- **Entscheidung (M4, Schritt 7, W8 A):**
  - GET /servers/{id}/logs?lines=N (Standard 200, höchstens 500) liest die letzten Zeilen über logs.tail.
  - Nur für RCON-Admin und Server-Admin im Scope mit verbundenem Agent. Ohne Agent antwortet das Panel mit 503 agent\_unavailable, Moderator und Supporter erhalten 403 (Rolle vor Objekt, Kandidat 14).
  - Die Zeilen sind Fremddaten und werden bereinigt und begrenzt (SEC-006). Sie gehen nur an den Aufrufer, nie in den Live-Ringpuffer.
  - Das Ratenlimit des Agents je Container greift und kommt als 429 rate\_limited an.
  - Neuer Ereigniscode logs.tail (Agent-Funktion im Panel).
  - Der Logs-Reiter hat einen Knopf „Load earlier lines“.
- **Vorschlag:** Design 2.2 um /servers/{id}/logs (GET, RCON-Admin; Server-Admin im Scope) ergänzen, Design 8.3 in der Zeile „Agent selbst“ um logs.tail.
