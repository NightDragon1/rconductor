# Abnahmekriterien R1

Dokumentversion 2.3, zur Freigabe vorgelegt; Änderungen gegenüber 2.2 siehe Gesamtkonzept, Kapitel 12, gegenüber 2.1 Kapitel 11, gegenüber 2.0 Kapitel 10. Jede Anforderung mit Target R1 gilt als erfüllt, wenn ihr Kriterium in der genannten Prüfart besteht. Kriterien beziehen sich auf die aktuelle Revision der Anforderung.

| Prüfart | Bedeutung |
| --- | --- |
| Unit | go test bzw. Vitest, ohne externe Dienste |
| Integration | go test mit testcontainers-go gegen echtes Docker, SQLite oder RCON-Testserver |
| E2E | Playwright gegen eine laufende Installation |
| Review | Prüfung von Code, Konfiguration oder Doku vor dem Release |

## 1. Allgemein, Sicherheit, Projekt (2.1)

| ID | Abnahmekriterium | Prüfart |
| --- | --- | --- |
| NFR-001 | Das Panel-Image startet mit leerem Volume ohne Umgebungsvariablen, lauscht auf 8080 (Web) und 8443 (Agent) und ist nach dem Start erreichbar | Integration |
| NFR-002 | Start ohne jede Konfiguration legt /data/rconductor.db an und führt alle Migrationen aus | Integration |
| NFR-003 | Hinter einem Proxy mit RCD\_BASE\_PATH=/rcon funktionieren Oberfläche, API und WebSocket; das Neuladen eines tiefen Pfads lädt alle Assets; X-Forwarded-For wird nur von RCD\_TRUSTED\_PROXIES übernommen | E2E |
| NFR-004 | Eine Person ohne Vorwissen bringt laut README Panel und Agent in der All-in-One-Variante in unter 15 Minuten zum Laufen | Review |
| NFR-005 | Cache-Werte tragen ein Alter; nach Ablauf der Frist aus Gesamtkonzept 4.6 zeigt die Oberfläche sie als veraltet; bei Agent-Trennung werden Messwerte sofort als veraltet markiert | Integration |
| NFR-007 | Alle Texte der Oberfläche kommen aus Sprachdateien; eine Testsprache mit veränderten Texten ändert jede sichtbare Beschriftung | Unit, E2E |
| NFR-008 | Ein Backup erzeugt eine konsistente Kopie der Datenbank in /backups, die sich in eine leere Installation einspielen lässt; der Master-Key ist nicht enthalten | Integration |
| NFR-009 | Die Erweiterungspunkte aus Architektur 7 sind als Go-Interfaces umgesetzt, jedes mit mindestens einer Test-Implementierung | Review |
| NFR-010 | Ein Neuanlegen des Containers mit denselben Volumes verliert keine Daten; das README listet jeden Pfad in /data, /secrets und /backups | Integration, Review |
| NFR-013 | Ein Panel der Version N migriert eine Datenbank der Version N-2 in einem Lauf; ein zweiter gleichzeitiger Start wartet auf die Sperre; vor der Migration liegt ein Backup in /backups | Integration |
| NFR-014 | Beide Compose-Varianten aus Design 5.4 und 5.5 starten unverändert; in der All-in-One-Variante ist 8443 nicht veröffentlicht | Integration |
| SEC-001 | Die Doku beschreibt den Betrieb nur im LAN oder per VPN; kein Beispiel veröffentlicht einen RCON-Port | Review |
| SEC-002 | Panel-Image läuft als Nicht-Root mit read-only Root-Dateisystem; das Panel hat keinen Zugriff auf den Docker-Socket; jede Rolle hat nur die Rechte aus Haupttab 3 | Review, E2E, Integration |
| SEC-003 | Ein Muster mit katastrophalem Backtracking bei anderen Engines endet in unter 1 s; Zeilenlimit und Timeout greifen | Unit |
| SEC-004 | Nach der eingestellten Zahl von Fehlanmeldungen in Folge (Standard 5) wird jede weitere Anmeldung dieses Kontos verzögert bzw. zeitweise gesperrt und im Audit-Log vermerkt | Integration |
| SEC-005 | Der Master-Key liegt nur in /secrets; weder Datenbank noch Backup enthalten ihn; RCD\_MASTER\_KEY\_FILE wird gelesen | Integration |
| PRJ-001 | Repository ist öffentlich unter AGPL-3.0, LICENSE liegt bei, Spielprofile liegen unter profiles/ | Review |
| PRJ-002 | Kein übernommener Fremdcode außer deklarierten Abhängigkeiten mit kompatibler Lizenz | Review |
| SEC-006 | Ein Spielername mit \<script> und eine RCON-Antwort mit HTML erscheinen als Text, nichts wird ausgeführt; ein unbekannter Farbcode erzeugt keinen Stil; ein Spielername mit U+202E erscheint ohne Umkehrung | E2E, Unit |
| SEC-007 | Eine Nachricht über der Höchstgröße trennt die Agent-Verbindung; schnelle Verbindungsversuche einer Adresse werden gedrosselt; wartende Kopplungen bleiben unter der Obergrenze | Integration |
| SEC-008 | Ein WebSocket über der Abo-Grenze erhält error limit statt eines weiteren Abos; ein Socket über der Grenze je Session schließt den ältesten Socket mit error replaced; beide Grenzen ändert nur der RCON-Admin | Integration |
| SEC-009 | Jede Antwort trägt die Header laut SEC-009; die Oberfläche läuft ohne CSP-Verletzung in der Browser-Konsole | E2E |
| SEC-010 | Die Doku enthält ein lauffähiges Beispiel mit Socket-Proxy; alle Agent-Funktionen aus AGT-003 funktionieren damit | Integration, Review |
| NFR-016 | Jeder Ereigniscode aus Design 8.3 tritt im Testlauf mindestens einmal auf, als gültige JSON-Zeile mit allen Pflichtfeldern aus 8.2 | Integration |
| SEC-013 | Release-Images für amd64 und arm64 tragen eine cosign-Signatur und eine SBOM-Attestierung, die cosign verify bestätigt; govulncheck ist Pflicht-Check; alle Actions sind auf Commit-SHA gepinnt | Review |
| SEC-012 | Ein Testlauf auf debug mit bekannten Test-Geheimnissen (Passwort, RCON-Passwort, Token) findet keines davon im Log von Panel und Agent, außer der einmaligen Ausgabe des Initialpassworts | Integration |

## 2. Benutzer (2.2)

| ID | Abnahmekriterium | Prüfart |
| --- | --- | --- |
| AUTH-001 | Benutzer lassen sich nur im Panel anlegen; die Datenbank enthält ausschließlich Argon2id-Hashes | Integration |
| AUTH-002 | Eine Zuweisung an eine Moderatorgruppe wirkt für alle Mitglieder; Entfernen aus der Gruppe nimmt die Rechte sofort | Integration |
| AUTH-003 | Der Notfall-Admin meldet sich ohne externe Identitätsquelle an; mit RCD\_EMERGENCY\_ADMIN=false ist keine Anmeldung möglich | Integration |
| AUTH-006 | Jede Anfrage ist einer Session zugeordnet; Abmelden, Sperren oder Rechteänderung beenden betroffene Sessions sofort, auch offene WebSockets | Integration, E2E |
| AUTH-007 | Beim ersten Start steht ein zufälliges Passwort genau einmal im Container-Log; es lässt sich in der Oberfläche ändern, danach ist es ungültig | Integration |
| AUTH-011 | Mit RCD\_EMERGENCY\_ADMIN\_RESET=true steht nach dem Start ein neues Initialpasswort genau einmal im Container-Log; das alte Passwort und alle Sessions des Notfall-Admins sind ungültig; must\_change\_password ist gesetzt; auth.emergency\_admin.reset und ein Audit-Eintrag ohne Passwort liegen vor | Integration |
| AUTH-010 | Nach dem ersten Start und nach einem Passwort-Reset liefert jede Anfrage außer /me/password, /auth/logout und /auth/session 403 mit password\_change\_required; nach dem Wechsel ist alles erlaubt, was die Rolle erlaubt; ein Reset beendet alle Sessions des Betroffenen, und das temporäre Passwort steht nur in der Antwort an den Verwaltenden, weder in Log noch Audit | Integration |
| AUTH-009 | Pseudonym ist Pflicht und eindeutig; Echtname, Bild und Kontakte sind optional und für andere Rollen nur laut Rechtemodell sichtbar | E2E |

## 3. Rechte (2.3)

| ID | Abnahmekriterium | Prüfart |
| --- | --- | --- |
| ROLE-001 | Es gibt genau vier Rollen; ein Benutzer ohne Rolle kann sich nicht anmelden | Unit |
| ROLE-002 | Der RCON-Admin erreicht jeden Server und jede Verwaltungsfunktion | E2E |
| ROLE-003 | Ein Server-Admin sieht genau die Server seiner Servergruppen und Einzelserver; Server außerhalb liefern 404 | Integration |
| ROLE-004 | Ein Server-Admin kann Moderatoren nur Server seines Scopes zuweisen; Versuche außerhalb werden abgelehnt und protokolliert | Integration |
| ROLE-005 | Ein Moderator kann nur Befehle der Liste seiner Moderatorgruppe senden | Integration |
| ROLE-006 | Ist die Supporter-Freigabe global oder für den Moderator aus, kann dieser keine Supporter anlegen oder zuweisen | Integration |
| ROLE-007 | Ein Supporter kann nur weitergebbare Befehle senden; eine Liste, in der alle Befehle weitergebbar wären, lässt sich nicht speichern | Integration |
| ROLE-008 | Keine Rolle kann eine gleich- oder höhergestellte Rolle anlegen, ändern oder herabstufen; jeder Versuch über die API wird abgelehnt | Integration |
| ROLE-010 | Ein Moderator in zwei Moderatorgruppen nutzt auf jedem Server nur die Liste der Gruppe(n), über die er den Server hat | Unit |

## 4. Server und Protokolle (2.4, 2.5)

| ID | Abnahmekriterium | Prüfart |
| --- | --- | --- |
| SRV-001 | Ein Server mit Name, Profil, Host, Port und Passwort lässt sich anlegen; Agent und Container sind optional und wählbar | E2E |
| SRV-002 | Das RCON-Passwort steht nur verschlüsselt in der Datenbank; mit gelöschtem Master-Key startet das Panel und fordert die Zugangsdaten neu an | Integration |
| SRV-003 | Der RCON-Admin legt Servergruppen an und ordnet Server zu; ein Server kann in mehreren Gruppen sein | E2E |
| SRV-004 | Bei falschem Passwort zeigt der Server Auth-Fehler, bei gestopptem Spiel offline, sonst verbunden; der Wechsel ist binnen 10 s sichtbar | Integration |
| SRV-005 | Spielports kommen aus dem Profil und lassen sich je Server überschreiben | Integration |
| SRV-006 | Spielerliste und Laufzeitdaten stehen in keiner Tabelle; nach einem Panel-Neustart werden sie neu abgefragt | Review, Integration |
| PROT-001 | Befehle an einen Minecraft-Testserver liefern dieselbe Antwort wie ein direkter RCON-Client, auch bei Antworten über 4 KB | Integration |
| PROT-002 | Bei 3 Benutzern, die gleichzeitig je 20 Befehle senden, besteht genau eine RCON-Verbindung und jede Antwort landet beim richtigen Absender; nach einem Spielneustart verbindet sich das Panel selbst | Integration |

## 5. Spielprofile (2.6)

| ID | Abnahmekriterium | Prüfart |
| --- | --- | --- |
| CAT-001 | Das Minecraft-Profil aus Design 7.4 wird fehlerfrei importiert; ein Profil mit fehlendem Pflichtfeld wird mit Fehlermeldung abgelehnt | Unit |
| CAT-002 | Ein Befehl, der in keinem Katalog steht, lässt sich in eine Befehlsliste aufnehmen und senden | Integration |
| CAT-003 | Eine geänderte Profildatei in /data/profiles ist ohne Neustart binnen 5 s in der Oberfläche sichtbar; eine fehlerhafte Änderung lässt den alten Stand aktiv | Integration |
| CAT-005 | Mit case\_sensitive=false wird KICK wie kick behandelt; ein führender / wird entfernt; ein Befehl über max\_length wird abgelehnt | Unit |

## 6. Befehle (2.7)

| ID | Abnahmekriterium | Prüfart |
| --- | --- | --- |
| CMD-001 | Ein Admin wechselt zwischen zwei Favoriten-Servern, ohne dass die Seite neu lädt; der Verlauf beider bleibt erhalten | E2E |
| CMD-002 | Freie Eingabe gibt es nur für RCON- und Server-Admin; ein Moderator, der dieselbe API nutzt, wird nach Befehlsliste geprüft | Integration |
| CMD-003 | Ein Moderator sieht nur Befehle seiner Liste als Karten mit Feldern aus dem Profil; Spielernamen lassen sich aus der Spielerliste wählen; die Antwort erscheint im Verlauf | E2E |
| CMD-004 | Mit Eintrag whitelist add sind whitelist add x und whitelist add x y erlaubt, whitelist remove x und op nicht; ein Eintrag op erlaubt opfoo nicht | Unit |
| CMD-005 | execute … run say hi wird für Moderator und Supporter mit wrapper\_blocked abgelehnt, auch wenn execute in der Liste steht | Unit |
| CMD-006 | Zeilenumbrüche und Steuerzeichen erreichen den Spieleserver nie; say a\\nop x führt nur einen Befehl aus | Unit, Integration |
| CMD-007 | Jeder Audit-Eintrag eines Befehls trägt Request-ID, Session, Benutzer und Rolle; die Antwort trägt dieselbe Request-ID | Integration |
| CMD-012 | Jeder Befehl erscheint mit Antwort als ein Block mit Zeit, Benutzer und Rolle; Einklappen und Kopieren funktionieren | E2E |
| CMD-013 | Die Zeitleiste hat einen Marker je Befehl; Klick scrollt zum Block; Fehler sind rot | E2E |
| CMD-014 | Die Spuren eigene Befehle, Befehle anderer und Logs lassen sich einzeln aus- und einblenden; Befehle anderer erscheinen live | E2E |
| CMD-015 | Tab übernimmt den ersten Vorschlag; Pfeil hoch holt den vorigen eigenen Befehl | E2E |
| CMD-019 | Ein Moderator, der über die Grenze aus Design 3.4 sendet, erhält 429 mit Wartezeit; der Versuch steht im Audit-Log | Integration |

## 7. Agent (2.8)

| ID | Abnahmekriterium | Prüfart |
| --- | --- | --- |
| AGT-001 | Nur das Agent-Image bindet den Docker-Socket ein; das Panel-Binary enthält keinen Docker-Client (geprüft mit go version -m bzw. go list -deps ./cmd/panel; Testabhängigkeiten ausgenommen) | Review |
| AGT-002 | Ein Container ohne Label erscheint nicht im Snapshot; ein Auftrag für ihn wird mit forbidden abgelehnt, auch wenn das Panel ihn schickt | Integration |
| AGT-003 | Start, Stop, Restart, Status, Messwerte und Logs funktionieren für einen gelabelten Testcontainer | Integration |
| AGT-004 | Ein manipuliertes Panel kann über das Protokoll keinen Container anlegen, löschen, exec ausführen oder Images und Volumes verändern; unbekannte Typen liefern unsupported | Integration |
| AGT-005 | Der Kopplungsdialog erzeugt ein Token und einen fertigen Compose-Ausschnitt; Kopplung über Fingerprint ist standardmäßig aus; nur der RCON-Admin sieht den Dialog | E2E |
| AGT-008 | Ein Server-Admin kann Agent-Aktionen nur für Server seines Scopes auslösen; Moderator und Supporter sehen keine Aktionen | Integration |
| AGT-009 | Ohne Agent löst Neustart den stop-Befehl aus dem Profil aus; der Container kommt mit unless-stopped zurück | Integration |
| AGT-011 | Der Agent-Container veröffentlicht keinen Port und verbindet sich selbst zum Panel | Review, Integration |
| AGT-012 | Ein Agent mit fremdem Schlüssel oder ein Panel mit fremdem Schlüssel wird beim Handshake abgewiesen; nach Widerruf verbindet sich der Agent nicht mehr | Integration |
| AGT-013 | Ein Agent mit anderer Hauptversion wird mit Grund version abgelehnt; der Hinweis steht im Panel und im Agent-Log | Integration |

## 8. Oberfläche und Audit (2.9 bis 2.11)

| ID | Abnahmekriterium | Prüfart |
| --- | --- | --- |
| UI-001 | Alle Seiten sind im dunklen und hellen Design lesbar (Kontrast mindestens 4,5:1 für Text); keine verschiebbaren Widgets | E2E, Review |
| UI-002 | Jede Rolle landet nach der Anmeldung auf ihrer Landingpage laut Haupttab 5 | E2E |
| UI-003 | Die Gesundheitsübersicht zeigt Restart-Schleifen, getrennte Agents, RCON-Fehler sowie aktive und gesperrte Benutzer, jeweils mit Sprung zum Objekt | E2E |
| UI-004 | Karten zeigen nur Felder mit Quelle; bei sha256-Image steht das Spielprofil statt des Images | E2E |
| UI-006 | Moderator-Karten zeigen keine Agent-Daten; der Status kommt aus der RCON-Erreichbarkeit | E2E |
| UI-007 | Listen sind durchsuchbar und filterbar; eine Massenaktion auf 2 Benutzer wirkt auf beide | E2E |
| UI-008 | Ein Benutzer lässt sich aus einer Vorlage anlegen; ein Einladungslink läuft nach der eingestellten Frist ab; Rückgängig stellt eine Änderung wieder her; Tastaturkürzel sind standardmäßig aus | E2E |
| UI-009 | Logs werden nur bei offener Ansicht übertragen; nach Verlassen der Seite endet logs.follow | Integration |
| UI-011 | Navigation und Reiter zeigen je Rolle nur erlaubte Einträge laut Design 6.2 und 6.3 | E2E |
| UI-012 | Bei 390 px Breite sind Befehlsseite, Karten und Konsole ohne horizontales Scrollen bedienbar; Tippflächen sind mindestens 44 px; Verwaltung ist nur lesbar | E2E |
| AUD-001 | Befehle, Ablehnungen, Agent-Aktionen, Verwaltungsänderungen und Anmeldungen erzeugen je einen Eintrag mit Ergebnis | Integration |
| AUD-003 | Einträge älter als die Frist (Standard 90 Tage) sind nach dem nächsten Aufräumlauf entfernt; nur der RCON-Admin kann die Frist ändern | Integration |
| AUD-004 | Ein Server-Admin sieht nur Einträge seines Scopes; der Versuch, die Frist zu ändern, wird abgelehnt | Integration |

Grenzwerte in diesen Kriterien (10 s, 5 s, 4 KB, 390 px) sind Prüfwerte, keine neuen Anforderungen; sie werden mit der Umsetzung bestätigt.

## 9. Prüfbar über Logs und MCP

Diese Kriterien lassen sich ganz oder zum Teil über die Ereigniscodes aus Design 8.3 nachweisen, ohne Zugriff auf Code oder Datenbank. Oberflächen-Kriterien (E2E) bleiben bei Playwright bzw. Sichtprüfung.

| ID | Nachweis im Log | Umfang |
| --- | --- | --- |
| NFR-002 | db.migrate.done beim ersten Start mit leerem Volume | voll |
| NFR-008 | backup.done mit Dateiname; kein key.\* in der Sicherung | teilweise |
| NFR-013 | db.migrate.start und done über zwei Versionen in einem Lauf; db.migrate.lock\_wait beim zweiten Start; backup.done davor | voll |
| SEC-004 | auth.login.failed bis zur Grenze, danach auth.lockout | voll |
| SEC-005 | key.created bzw. key.loaded mit Pfad in /secrets | teilweise |
| SEC-007 | agent.limit mit reason bei übergroßer Nachricht oder Verbindungsflut | voll |
| SEC-008 | ws.subscribe.denied über der Abo-Grenze | voll |
| AUTH-003 | auth.login.ok für den Notfall-Admin; mit deaktiviertem Flag auth.login.failed | voll |
| AUTH-006 | auth.session.revoked nach Sperre oder Rechteänderung | voll |
| AUTH-007 | auth.initial\_password.issued genau einmal über zwei Starts | voll |
| ROLE-003, ROLE-004, ROLE-008 | perm.denied mit reason bei Zugriffen außerhalb des Scopes | voll |
| SRV-004 | rcon.auth\_failed bzw. rcon.connected und rcon.disconnected | voll |
| PROT-002 | genau ein rcon.connected je Server; je cmd.sent ein cmd.response mit gleicher request\_id; rcon.reconnect nach Spielneustart | voll |
| CAT-001, CAT-003 | profile.imported bzw. profile.rejected nach Dateiänderung, ohne app.start dazwischen | voll |
| CMD-004, CMD-005 | cmd.accepted bzw. cmd.denied mit not\_in\_list oder wrapper\_blocked | voll |
| CMD-007 | request\_id, session, user und role in cmd.accepted, cmd.sent und cmd.response gleich | voll |
| CMD-019 | cmd.denied mit rate\_limited | voll |
| AGT-002, AGT-004 | agent.forbidden bzw. agent.unsupported im Agent-Log | voll |
| AGT-003 | docker.action mit Ergebnis für start, stop, restart | teilweise |
| AGT-012 | agent.rejected mit pin\_mismatch; nach Widerruf agent.revoked und kein weiteres agent.connected | voll |
| AGT-013 | agent.rejected mit version in Panel- und Agent-Log | voll |
| UI-009 | logs.follow.start beim Öffnen, logs.follow.stop beim Verlassen | voll |
| AUD-003 | audit.purge mit Anzahl entfernter Einträge | teilweise |
| NFR-005 | cache.stale und agent.disconnected nach Agent-Trennung | teilweise |

Voll heißt: Der Log-Nachweis genügt für die Abnahme. Teilweise heißt: Das Log belegt den Ablauf, das Ergebnis (z. B. Inhalt einer Sicherung) wird zusätzlich geprüft.
