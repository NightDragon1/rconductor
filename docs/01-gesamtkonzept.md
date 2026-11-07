# ShadowGhost.NET Game RCONductor – Gesamtkonzept

Projektname: ShadowGhost.NET Game RCONductor (kurz RCONductor). Dokumentversion 2.8, Stand 06.11.2026, Status freigegeben; Änderungen gegenüber 2.7 in Kapitel 17, gegenüber 2.6 in Kapitel 16, gegenüber 2.5 in Kapitel 15, gegenüber 2.4 in Kapitel 14, gegenüber 2.3 in Kapitel 13, gegenüber 2.2 in Kapitel 12, gegenüber 2.1 in Kapitel 11, gegenüber 2.0 in Kapitel 10.

## 1. Einleitung

### 1.1 Zweck

Eine zentrale, selbst gehostete Weboberfläche steuert alle RCON-fähigen Spieleserver mit fein abgestuften Rechten. Sie ersetzt das Muster „eine Web-App pro Spieleserver“ (z. B. einzelne itzg/rcon-Container) und erscheint als Open Source auf GitHub.

### 1.2 Ziele

- Eine Instanz für beliebig viele Spieleserver und Spiele.
- Delegation an Server-Admins, Moderatoren und Supporter ohne Vollzugriff.
- Hoster behalten die Kontrolle: Das Panel erreicht nur ausdrücklich freigegebene Container.
- Einrichtung ohne Expertenwissen.

### 1.3 Abgrenzung

| Im Umfang | Nicht im Umfang |
| --- | --- |
| Befehle per RCON und verwandten Protokollen | Spieleserver anlegen, installieren, aktualisieren |
| Start, Stop, Restart, Status, Logs freigegebener Container | Beliebiger Zugriff auf Host oder Docker |
| Benutzer, Rollen, Rechte | Zugang für normale Spieler |
| Betrieb hinter Reverse Proxy | TLS, Zertifikate, Reverse Proxy selbst |

### 1.4 Begriffe

| Begriff | Bedeutung |
| --- | --- |
| Panel | Die Webanwendung (Backend und Oberfläche) |
| Agent | Kleiner Dienst pro Host, einzige Komponente mit Zugriff auf den Docker-Socket |
| Spielprofil | Datei im Repository mit Protokoll, Befehlskatalog und Erkennungsmustern eines Spiels |
| Befehlskatalog | Bekannte Befehle eines Spiels; nur Orientierung, keine Erlaubnisliste |
| Befehlsliste | Vom Server-Admin festgelegte Befehle einer Moderatorgruppe |
| Servergruppe | Benannte Menge von Spieleservern, Einheit der Rechtevergabe |
| Scope | Server und Servergruppen, auf die ein Benutzer Zugriff hat |

### 1.5 Konventionen

- Gliederung der Anforderungen nach Kapiteln; ID: Kürzel plus mindestens dreistellige Nummer (z. B. ROLE-003), wird nie neu vergeben.
- Priorität: Muss, Soll, Kann.
- Target Release: R1 (erste Version), R2, R3, Backlog.
- Affected Component: Backend, UI, Agent, Profile, DB, Doku.
- Rev: Revision der einzelnen Anforderung, startet bei 1 und steigt bei jeder inhaltlichen Änderung.

## 2. Anforderungen

Die Anforderungen sind nach Kapiteln gegliedert; jedes Kapitel beschreibt einen Funktionsbereich. IDs bleiben über alle Versionen stabil.

### 2.1 Allgemeine Anforderungen

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| NFR-001 | Ein Container | Panel als ein Container mit einem Web-Port und einem Agent-Port (D-019), Konfiguration per Env | Backend | Muss | R1 | 2 |
| NFR-002 | Datenbanken | Standardinstallation mit SQLite ohne jede Konfiguration; Postgres optional für größere Installationen ab R2 | Backend, DB | Muss | R1 | 1 |
| NFR-003 | Reverse Proxy | Weboberfläche spricht nur HTTP; proxy-tauglich mit X-Forwarded-Headern, Trusted Proxies, Basispfad und WebSocket-Upgrade; der Agent-Port terminiert TLS selbst und läuft nie über den Proxy (D-019) | Backend | Muss | R1 | 2 |
| NFR-004 | Einfache Einrichtung | Panel und Agent ohne Expertenwissen einrichtbar: starten, koppeln, fertig | Backend, Agent | Muss | R1 | 1 |
| NFR-005 | Cache | Agent sammelt, Panel puffert; Invalidierung bei Verbindungsabbruch, Containerwechsel und Ablauf | Backend | Muss | R1 | 1 |
| NFR-006 | Metriken | Prometheus-Metriken und Healthcheck-Endpoint | Backend, Agent | Soll | R2 | 1 |
| NFR-007 | Sprachen | Oberfläche in Englisch ab R1, Übersetzungsstruktur von Anfang an; Deutsch als zweite Sprache ab R2; weitere Sprachen als Dateien ergänzbar | UI | Muss | R1 | 1 |
| NFR-008 | Backups | Regelmäßige automatische Backups der veränderlichen Daten (Datenbank, Befehlslisten, Einstellungen) nach /backups; der Master-Key wird getrennt davon gesichert (SEC-005) | Backend | Soll | R1 | 1 |
| NFR-009 | Erweiterbarkeit | Niedrig priorisierte Anforderungen werden in der Architektur von Anfang an berücksichtigt, damit spätere Releases nichts umbauen müssen; Schnittstellen für Auth-Anbieter und Datenbanktreiber folgen in R2 | alle | Muss | R1 | 2 |
| NFR-010 | Volumes | Alle Daten liegen in eigenen Volumes: /data (SQLite mit Profilbildern, eigene Profile), /secrets (Master-Key, Agent-Schlüssel), /backups; README beschreibt genau, was wo liegt | Backend, Doku | Muss | R1 | 2 |
| NFR-011 | Datenbankwahl per Env | RCD\_DB\_DRIVER wählt sqlite (Standard) oder postgres. postgres ohne RCD\_DB\_URL: interner Postgres-Dienst aus der mitgelieferten Compose-Datei, Zugangsdaten automatisch über Init-Skript bzw. .env vorbereitet. postgres mit RCD\_DB\_URL: externer Server, alle Zugangsdaten (Host, Port, Datenbank, Benutzer, Passwort) kommen vom Betreiber | Backend, Doku | Soll | R2 | 2 |
| NFR-012 | Datenbankwechsel | Wechsel von SQLite nach Postgres per Befehl, ohne Datenverlust | Backend | Soll | R2 | 1 |
| NFR-013 | Schema-Migrationen | Versionierte, ins Binary eingebettete Migrationen; beim Start automatisch ausgeführt, auch gegen externe Datenbanken; mehrere Versionen werden in einem Lauf nacheinander nachgezogen; Sperre gegen parallele Läufe; Backup vor jeder Migration | Backend, DB | Muss | R1 | 1 |
| NFR-014 | Installationsvarianten | Getrennte Compose-Dateien für Panel und Agent als empfohlener Weg, dazu eine All-in-One-Datei; Panel und Agent sind getrennte Images; in der All-in-One-Variante ist der Agent-Port nur im Docker-Netz erreichbar | Backend, Agent, Doku | Muss | R1 | 1 |
| NFR-015 | Externe API | Dokumentierte REST-API mit OpenAPI 3.1 für Automatisierung; API-Tokens je Benutzer (Bearer) mit gleichem Rechteumfang wie der Benutzer, Ablaufzeit und Widerruf | Backend, Doku | Soll | R2 | 1 |
| NFR-016 | Ereignis-Logs | Panel und Agent schreiben prüfrelevante Ereignisse als JSON-Zeilen mit festem Ereigniscode, Request-ID und Kontextfeldern auf stdout; Codes sind dokumentiert und innerhalb einer Hauptversion stabil; Abnahme darüber z. B. per Portainer-MCP (Design 8) | Backend, Agent, Doku | Muss | R1 | 1 |
| SEC-001 | Netzannahme | Zugriff auf das Panel nur aus LAN oder per VPN (z. B. WireGuard); RCON nie extern erreichbar | Betrieb | Muss | R1 | 1 |
| SEC-002 | Least Privilege | Jede Komponente und Rolle erhält nur die minimal nötigen Rechte | alle | Muss | R1 | 1 |
| SEC-003 | Regex-Sicherheit | Muster mit RE2-Semantik (lineare Laufzeit), Zeilenlimit und Timeout | Agent | Muss | R1 | 1 |
| SEC-004 | Login-Schutz | Verzögerung und zeitweise Sperre nach wiederholten Fehlanmeldungen | Backend | Muss | R1 | 1 |
| SEC-005 | Key-Trennung | Master-Key liegt nur in /secrets, nie in der Datenbank oder im Datenbank-Backup; optional per Docker-Secret übergebbar; ab R2 kann der RCON-Admin einen mit Passphrase geschützten Recovery-Export herunterladen | Backend, UI | Muss | R1 | 1 |
| SEC-006 | Fremddaten | Daten von Spieleservern und Agents (Antworten, Spielernamen, Logs, Versionen, Container-Namen) werden escaped dargestellt und in der Größe begrenzt; Farbcodes werden in R1 vor der Anzeige entfernt, die feste Liste erlaubter Stile kommt mit CMD-017 (R2). Bidi-Steuerzeichen im Freitext wirken nicht, die Oberfläche zeigt sie als Platzhalter. Steuer- und Formatzeichen (Unicode Cc und Cf, u. a. Bidi-Steuerzeichen) werden aus Bezeichnern wie Spielernamen und Container-Namen entfernt, Steuerzeichen außer Zeilenumbruch und Tabulator auch aus Freitext | Backend, UI | Muss | R1 | 3 |
| SEC-007 | Grenzen Agent-Port | Höchstgröße je Nachricht, begrenzte Logzeilen je Nachricht, Rate für Verbindungsaufbau je Quelladresse, Obergrenze für wartende Kopplungen | Backend, Agent | Muss | R1 | 1 |
| SEC-008 | WebSocket-Grenzen | Höchstzahl an WebSocket-Verbindungen je Session und an Abos je Verbindung, als Einstellungen des RCON-Admins (Design 2.3, 5.6) | Backend | Soll | R1 | 2 |
| SEC-009 | Sicherheitsheader | Content-Security-Policy ohne Inline-Skripte, X-Content-Type-Options, Referrer-Policy, frame-ancestors none, auf jeder Antwort des Web-Ports | Backend | Muss | R1 | 2 |
| SEC-010 | Socket-Proxy | Doku empfiehlt einen Socket-Proxy vor dem Docker-Socket, der nur die vom Agent benötigten Endpunkte durchlässt; der Agent funktioniert damit unverändert | Agent, Doku | Kann | R1 | 1 |
| SEC-011 | Audit-Hash-Kette | Manipulationssichere Verkettung der Audit-Einträge mit Prüffunktion | Backend, DB | Kann | Backlog | 1 |
| SEC-012 | Keine Geheimnisse in Logs | Passwörter, RCON-Zugangsdaten, Tokens, Schlüssel und Session-IDs erscheinen auf keinem Log-Level; Sessions nur als kurzer Hash; einzige Ausnahme ist die einmalige Ausgabe des Initialpassworts (AUTH-007) | Backend, Agent | Muss | R1 | 1 |
| SEC-013 | Lieferkette | Images für amd64 und arm64 signiert (cosign keyless) mit SBOM (syft); govulncheck als Pflicht-Check; Actions auf Commit-SHA gepinnt; Abhängigkeits-Updates per Renovate ohne ungeprüftes Automerge: Merge durch Claude Code nach grüner CI und Sichtung des Changelogs zulässig, Major nur nach Freigabe; Release-Tags erst nach Veröffentlichung des Repositories; Signatur und SBOM gelten für ghcr.io und Docker Hub (PRJ-004) | alle | Muss | R1 | 3 |
| PRJ-001 | Open Source | Veröffentlichung auf GitHub unter GNU AGPL-3.0; Spielprofile sind Teil des Repositories | alle | Muss | R1 | 1 |
| PRJ-002 | Eigenständigkeit | Eigene Implementierung; Ideen anderer Tools nur sinngemäß, kein fremder Code, kein fremdes Design; ausgenommen kopierte Komponenten mit kompatibler Lizenz, die gekennzeichnet sind | alle | Muss | R1 | 2 |
| PRJ-003 | GitHub-Release | Je Version ein GitHub-Release mit Tag, Änderungsnotizen, SBOM und Verweis auf die signierten Images | alle | Muss | R1 | 1 |
| PRJ-004 | Docker Hub | Images zusätzlich auf Docker Hub mit denselben Tags, Signatur und SBOM wie auf ghcr.io | alle | Muss | R1 | 1 |

### 2.2 Benutzerverwaltung

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| AUTH-001 | Lokale Benutzer | Benutzer werden immer lokal angelegt; lokale Passwörter mit argon2id | Backend, DB | Muss | R1 | 1 |
| AUTH-002 | Benutzergruppen | Lokale Benutzergruppen; Rechte können an Gruppen vergeben werden | Backend, UI | Muss | R1 | 1 |
| AUTH-003 | Notfall-Admin | Lokaler RCON-Admin, nie in Keycloak, immer lokal erreichbar; standardmäßig aktiv, per Env-Flag deaktivierbar | Backend | Muss | R1 | 1 |
| AUTH-004 | Keycloak-Auth | Lokal angelegte Benutzer optional gegen Keycloak (OIDC) authentifizieren, wie bei Nextcloud/Portainer; Architektur sieht das ab R1 vor | Backend | Soll | R2 | 1 |
| AUTH-005 | Inaktivitätssperre | Konten ohne Anmeldung über eine einstellbare Frist optional automatisch sperren | Backend | Soll | R2 | 1 |
| AUTH-006 | Sessions | Session-Management für alle Benutzer; Zuordnung Session zu Request-ID wird geloggt; welche Änderungen Sessions beenden, regelt D-044 | Backend | Muss | R1 | 2 |
| AUTH-007 | Initialpasswort | Beim ersten Start des Containers wird ein zufälliges Passwort für den Notfall-Admin erzeugt und im Container-Log ausgegeben; Änderung in der Oberfläche | Backend | Muss | R1 | 1 |
| AUTH-008 | Zwei-Faktor | Optionale Zwei-Faktor-Anmeldung (TOTP); RCON-Admin kann sie für alle erzwingen, jeder Benutzer kann sie für sich aktivieren | Backend, UI | Soll | R2 | 1 |
| AUTH-011 | Wiederherstellung Notfall-Admin | Mit RCD\_EMERGENCY\_ADMIN\_RESET=true erhält der Notfall-Admin beim Start ein neues Initialpasswort (Ausgabe wie AUTH-007) mit must\_change\_password; alle seine Sessions enden; Audit-Eintrag und Ereigniscode | Backend | Muss | R1 | 1 |
| AUTH-010 | Erzwungener Passwortwechsel | Benutzer mit must\_change\_password dürfen nur ihr Passwort ändern, sich abmelden und ihre Session abfragen; alles andere wird mit eigenem Grund-Code abgelehnt. Das Flag wird beim Initialpasswort (AUTH-007) und bei einem Passwort-Reset durch einen Verwaltenden gesetzt und mit dem Wechsel gelöscht; ein Reset beendet alle Sessions des Betroffenen, das temporäre Passwort erhält nur der Verwaltende einmal in der Antwort | Backend, UI | Muss | R1 | 1 |
| AUTH-009 | Benutzerprofil | Pseudonym als primärer Anzeigename; optional Echtname, Profilbild-Upload und Kontaktangaben (z. B. Telegram, WhatsApp) | Backend, UI, DB | Soll | R1 | 1 |

### 2.3 Rechteverwaltung

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| ROLE-001 | Feste Rollen | Vier Rollen: RCON-Admin, Server-Admin, Moderator, Supporter; normale Spieler erhalten keinen Zugang | Backend | Muss | R1 | 1 |
| ROLE-002 | RCON-Admin | Volle Rechte auf alle Server und die gesamte Verwaltung | Backend | Muss | R1 | 1 |
| ROLE-003 | Scope Server-Admin | RCON-Admin weist Server-Admins Servergruppen und/oder einzelne Server zu; Server-Admin kann seinen Scope nicht ändern | Backend, UI | Muss | R1 | 1 |
| ROLE-004 | Moderator zuweisen | Server-Admin weist Moderatoren je Server oder Servergruppe innerhalb seines Scopes zu | Backend, UI | Muss | R1 | 1 |
| ROLE-005 | Moderatorgruppen | Server-Admin definiert beliebig viele Moderatorgruppen mit eigener Befehlsliste; Moderatoren wählen nur daraus | Backend, UI | Muss | R1 | 1 |
| ROLE-006 | Supporter-Freigabe | Supporter global schaltbar (RCON-Admin) und je Moderator freigebbar (Server-Admin) | Backend | Muss | R1 | 1 |
| ROLE-007 | Weitergebbare Befehle | Server-Admin markiert weitergebbare Befehle; Moderator vergibt nur diese an Supporter je Server; mindestens ein Befehl bleibt immer gesperrt | Backend, UI | Muss | R1 | 1 |
| ROLE-008 | Rechtefluss | Rechte fließen nur nach unten; keine Rolle legt gleich- oder höhergestellte an, ändert oder degradiert sie; Verwaltungsrechte sind nie weitergebbar | Backend | Muss | R1 | 1 |
| ROLE-009 | Verlaufssichtbarkeit | Server-Admin legt je Server oder Servergruppe fest, ob Moderatoren und Supporter nur eigene oder alle Befehle sehen | Backend, UI | Soll | R2 | 1 |
| ROLE-010 | Befehlsliste je Server | Ist ein Moderator in mehreren Moderatorgruppen, gilt auf einem Server nur die Liste der Gruppe(n), über die er diesen Server hat; mehrere Wege zum selben Server werden vereinigt | Backend | Muss | R1 | 1 |

### 2.4 Serververwaltung

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| SRV-001 | Server anlegen | Name, Spielprofil, Protokoll, Host, Port, Zugangsdaten, Agent und Container (Agent und Container setzbar ab M3) | Backend, UI | Muss | R1 | 2 |
| SRV-002 | Zugangsdaten verschlüsselt | RCON-Zugangsdaten mit AES-256-GCM verschlüsselt; der Master-Key wird beim ersten Start automatisch erzeugt und als Datei in /secrets abgelegt, getrennt von Datenbank und Backups (D-015, SEC-005), ohne Zutun des Betreibers; bei Verlust müssen die Zugangsdaten neu eingetragen werden | Backend, DB | Muss | R1 | 2 |
| SRV-003 | Servergruppen | RCON-Admin legt Servergruppen an und ordnet Server zu | Backend, UI | Muss | R1 | 1 |
| SRV-004 | Erreichbarkeit | Status je Server: verbunden, Auth-Fehler, offline | Backend | Muss | R1 | 1 |
| SRV-005 | Spielports | Festlegen, welche Ports Spielports sind, über Spielprofil oder durch RCON-Admin am Server | Backend, Profile | Soll | R1 | 1 |
| SRV-006 | Laufzeitdaten | Spielerliste und ähnliche Laufzeitdaten werden nicht dauerhaft gespeichert, sondern per Befehl bzw. über den Agent abgerufen und im Cache des Panels gepuffert | Backend | Muss | R1 | 1 |
| SRV-007 | Verbindungstest | RCON-Admin prüft beim Anlegen und Bearbeiten die eingegebenen RCON-Daten, ohne zu speichern; Ergebnis ok, auth\_failed oder unreachable; mit Audit und Ratenlimit (D-049) | Backend, UI | Soll | R1 | 1 |

### 2.5 Protokolle

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| PROT-001 | Source RCON | Adapter für Source RCON (TCP): Minecraft, ARK, Factorio u. a. | Backend | Muss | R1 | 1 |
| PROT-002 | Eine Verbindung je Server | Genau eine persistente Verbindung je Spieleserver mit Reconnect; Befehle in Warteschlange, Antworten per Request-ID zugeordnet. Einzige Ausnahme ist der Verbindungstest mit neuen oder geänderten Daten (SRV-007): ein kurzer Aufbau nur mit Authentifizierung, ohne Befehl, danach sofort geschlossen | Backend | Muss | R1 | 2 |
| PROT-003 | HTTP-API | Adapter für HTTPS-APIs, z. B. Satisfactory Dedicated Server API | Backend | Soll | R2 | 1 |
| PROT-004 | Weitere Adapter | BattlEye RCon, WebRCON, Telnet nach Bedarf | Backend | Kann | R3 | 1 |

### 2.6 Spielprofile und Befehlskatalog

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| CAT-001 | Spielprofile | Profildatei je Spiel im Repository: Protokoll, Standardport, Spielports, Befehlskatalog, Muster für Version und Spieler, Parameter der Katalogbefehle; Format laut Design 7 | Profile | Muss | R1 | 2 |
| CAT-002 | Nur Orientierung | Katalog dient Autovervollständigung und Schnelleingabe und erlaubt nichts; seine Parameter schränken nur Moderator und Supporter ein (Design 3.3, Schritt 7b); Befehlslisten dürfen beliebige Befehle enthalten | Backend, UI | Muss | R1 | 2 |
| CAT-003 | Hot Reload | Profildateien sind die Quelle; beim Start und bei jeder Dateiänderung werden sie in die Datenbank importiert, ohne Neustart des Panels; die Oberfläche arbeitet ausschließlich aus der Datenbank | Backend | Muss | R1 | 1 |
| CAT-004 | Kategorien | Katalogbefehle kategorisiert (spielerbezogen, Welt, Server, kritisch); daraus Standard-Befehlsliste für Moderatoren | Profile, UI | Soll | R2 | 1 |
| CAT-005 | Befehlssyntax je Spiel | Spielprofil legt Groß-/Kleinschreibung, Präfixe (z. B. /), Wrapper-Befehle und maximale Befehlslänge fest; Standard: ohne Groß-/Kleinschreibung, führender / wird entfernt | Profile, Backend | Muss | R1 | 1 |

### 2.7 Befehlseingabe

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| CMD-001 | Admin-Konsole | Live-Konsole für Admins mit Favoriten und Serverwechsel ohne Seitenwechsel; Darstellung und Eingabe gemäß CMD-012 bis CMD-015, ab R2 auch CMD-016 und CMD-017 | UI | Muss | R1 | 2 |
| CMD-002 | Freie Eingabe | Freie Befehlseingabe nur für RCON- und Server-Admins | Backend | Muss | R1 | 1 |
| CMD-003 | Befehlsseite Moderator | Befehle der Befehlsliste als Buttons mit Parameterfeldern; Spielernamen aus aktueller Spielerliste wählbar; Verlauf mit Serverantworten | UI | Muss | R1 | 1 |
| CMD-004 | Token-Matching | Befehl ist ein Pfad aus ganzen Tokens; alles danach geht beim Pfadabgleich unverändert durch („whitelist add tralala“). Für Moderator und Supporter gilt bei Befehlen mit Katalogeintrag zusätzlich die Parameterprüfung (Design 3.3, Schritt 7b) | Backend | Muss | R1 | 2 |
| CMD-005 | Wrapper-Schutz | Wrapper wie Minecraft „execute … run“ für Moderatoren und Supporter immer gesperrt; Wrapper-Befehle sind im Spielprofil markiert (D-026) | Backend, Profile | Muss | R1 | 2 |
| CMD-006 | Eingabebereinigung | Zeilenumbrüche und Steuerzeichen werden vor dem Senden entfernt | Backend | Muss | R1 | 1 |
| CMD-007 | Klassifizierung | Jeder Befehl trägt Request-ID, Session, Benutzer und Rolle; die Antwort erbt die Markierung | Backend | Muss | R1 | 1 |
| CMD-008 | Vorlagen | Befehlsvorlagen mit Platzhaltern je Server (z. B. kick {player}) | Backend, UI | Soll | R2 | 1 |
| CMD-009 | Argument-Einschränkung | Einzelne Argumente einschränkbar (z. B. nur Spielernamen) | Backend | Kann | R2 | 1 |
| CMD-010 | Mehrfachbefehle | Befehl auf mehrere Server gleichzeitig (z. B. Broadcast vor Wartung) | Backend, UI | Soll | R2 | 1 |
| CMD-011 | Zeitsteuerung | Zeitgesteuerte Befehle mit Cron-Syntax | Backend, UI | Soll | R2 | 1 |
| CMD-012 | Blockdarstellung | Jeder Befehl bildet mit seiner Antwort einen Block; einklappbar, kopierbar, mit Zeit, Benutzer und Rolle | UI | Muss | R1 | 1 |
| CMD-013 | Zeitleiste | Leiste links mit einem Marker je Befehl; Klick springt zum Block; Farbe nach Rolle bzw. Ergebnis, Fehler rot | UI | Muss | R1 | 1 |
| CMD-014 | Spuren und Filter | Getrennte, umschaltbare Spuren: eigene Befehle, Befehle anderer, Logs; Filter oben | UI, Backend | Muss | R1 | 1 |
| CMD-015 | Eingabezeile | Eingabe unten; Autovervollständigung der Befehlsnamen (ohne Argumente), Übernahme per Tab, Verlauf per Pfeiltasten | UI | Muss | R1 | 1 |
| CMD-016 | Befehlspalette | Befehlspalette über eine Schaltfläche, bei aktivierten Tastaturkürzeln zusätzlich per Strg+K, mit Suche über Befehle, Vorlagen und Favoriten | UI | Soll | R2 | 1 |
| CMD-017 | Farbcodes | Spielspezifische Formatierung der Antworten darstellen (z. B. Minecraft-Farbcodes) | UI, Profile | Soll | R2 | 1 |
| CMD-018 | Eigene Befehlsdefinitionen | RCON-Admins und Server-Admins erweitern Befehlslisten um eigene Befehle mit Argumenten (Arg 1, Arg 2, …); Syntax orientiert sich an den Möglichkeiten der Spiele und wird später festgelegt; dazu Autovervollständigung der Argumente | Backend, UI | Kann | Backlog | 1 |
| CMD-019 | Ratenlimit | Befehle je Benutzer und Server begrenzt, nach Rolle gestaffelt und vom RCON-Admin einstellbar; Überschreitungen landen im Audit-Log | Backend | Muss | R1 | 1 |

### 2.8 Host-Agent

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| AGT-001 | Socket-Isolation | Agent pro Host ist die einzige Komponente mit Docker-Socket-Zugriff | Agent | Muss | R1 | 1 |
| AGT-002 | Label-Freigabe | Nur Container mit Label (Standard rconductor.manage=true) sind sichtbar | Agent | Muss | R1 | 2 |
| AGT-003 | Funktionsumfang | Start, Stop, Restart, Status, CPU, RAM, Netz, Disk-I/O, Logs, ausschließlich für freigegebene Container | Agent | Muss | R1 | 1 |
| AGT-004 | Verbotene Operationen | Kein Erstellen, Löschen, Exec oder Ändern von Containern, keine Image- oder Volume-Operationen, keine Hostdaten | Agent | Muss | R1 | 1 |
| AGT-005 | Kopplung | Kopplung ohne externe CA über zwei Wege: Einmal-Token (immer verfügbar) und Freigabe per Fingerprint (standardmäßig aus); nur der RCON-Admin koppelt und widerruft; geführter Dialog mit fertigem Startbefehl | Agent, Backend, UI | Muss | R1 | 2 |
| AGT-006 | Versionserkennung | Panel schickt Muster aus dem Spielprofil; Agent durchsucht Logs ab Containerstart lokal und liefert nur den Treffer | Agent | Soll | R2 | 1 |
| AGT-007 | Volume-Größen | Belegter Speicher je Volume, im Hintergrund ermittelt und zwischengespeichert | Agent | Kann | R2 | 1 |
| AGT-008 | Agent-Rechte | RCON-Admin alle Agent-Funktionen überall; Server-Admin nur im Scope; Moderator und Supporter keine | Backend | Muss | R1 | 1 |
| AGT-009 | Betrieb ohne Agent | Ohne Agent Neustart per RCON-Befehl stop plus Restart-Policy unless-stopped | Backend, Profile | Kann | R1 | 1 |
| AGT-010 | Weitere Backends | Agent-Backends für systemd (LinuxGSM) und Kubernetes | Agent | Kann | R3 | 1 |
| AGT-011 | Verbindungsrichtung | Agent baut die Verbindung zum Panel auf; Panel-Adresse per Env; Agent benötigt keinen offenen Port | Agent, Backend | Muss | R1 | 1 |
| AGT-012 | Gegenseitige Authentifizierung | Beide Seiten erzeugen Ed25519-Schlüssel und pinnen den Schlüssel der Gegenseite; Verbindung über TLS 1.3 direkt zum Agent-Port des Panels, nie über einen Reverse Proxy (D-019); Widerruf eines Agents im Panel | Agent, Backend | Muss | R1 | 2 |
| AGT-013 | Versionsregel | Panel und Agent brauchen dieselbe Hauptversion; bei Abweichung lehnt das Panel die Verbindung mit Hinweis im Panel-Log und im Agent-Log ab; neue Funktionen innerhalb einer Hauptversion meldet der Agent über eine Fähigkeitsliste | Agent, Backend | Muss | R1 | 2 |

### 2.9 Landingpage und Dashboard

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| UI-002 | Landingpage je Rolle | Landingpage je Rolle mit den für die Rolle relevanten Informationen | UI | Muss | R1 | 1 |
| UI-003 | Gesundheitsübersicht | Für RCON-Admins: Restart-Schleifen, Agent-Erreichbarkeit, RCON-Fehler, aktive und gesperrte Benutzer | UI, Backend | Muss | R1 | 1 |
| UI-004 | Server-Karten | Statusrand und -punkt, Name, Image (bei sha256 Spielprofil), CPU und RAM, letzter Restart, Spielversion (ab R2, AGT-006), Spieler online; Felder ohne Quelle ausgeblendet, auch die Spielerzahl, wenn die Antwort nicht zum Profil passt; die ganze Karte ist klickbar | UI | Muss | R1 | 3 |
| UI-005 | Ports und Volumes | Server-Admin sieht Spielports, optional Steam-Connect-Link; RCON-Admin alle relevanten Ports; Volumes mit Name und Größe | UI | Soll | R2 | 1 |
| UI-006 | Karten Moderator | Nur eigene Server; Status per RCON-Erreichbarkeit, Version (ab R2, AGT-006), Spieler; keine Agent-Daten | UI | Muss | R1 | 2 |
| UI-010 | Topologie | Live-Graph Host → Agent → Spieleserver, gruppiert nach Servergruppen | UI | Kann | R2 | 1 |

### 2.10 Oberfläche und Bedienung

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| UI-001 | Stil | Aufgeräumt wie Portainer, Diagramme wie Grafana, keine verschiebbaren Widgets, Design hell, dunkel oder automatisch nach Systemeinstellung, Standard automatisch; sichtbarer Hover- und Fokus-Zustand aller klickbaren Elemente; Kontrast 4,5:1 für Text, 3:1 für Symbole, Rahmen von Controls, Fokusring und Statuspunkte (WCAG 1.4.11) | UI | Muss | R1 | 3 |
| UI-007 | Verwaltungslisten | Durchsuchbare Listen, Details im Seitenpanel; die Benutzerliste zusätzlich filterbar und mit Mehrfachauswahl und Massenaktionen | UI | Muss | R1 | 2 |
| UI-008 | Schnelleingabe | Rechte und Zuweisungen eines bestehenden Benutzers als Vorlage übernehmen; Mehrfachauswahl mit Massenaktionen; neue Benutzer per Einladungslink mit Ablaufzeit statt Passwortvergabe; Inline-Bearbeitung in Listen; expliziter Save-Knopf speichert sofort, danach Rückgängig für einige Sekunden statt Bestätigungsdialog (Ausnahmen D-048), Bestätigung nur bei destruktiven Aktionen; Nachfrage beim Verlassen mit ungespeicherten Änderungen; Tastaturkürzel für häufige Aktionen, standardmäßig deaktiviert, vom RCON-Admin global aktivierbar | UI | Soll | R1 | 2 |
| UI-009 | Logansicht | Live-Logs für berechtigte Rollen, übertragen nur bei aktiver Ansicht; optional letzte Logzeilen auf der Karte | UI, Agent | Soll | R1 | 1 |
| UI-011 | Navigation und Serverseite | Einklappbare Seitenleiste mit Einträgen je Rolle; Serverseite mit Kopf (Status, Aktionen) und Reitern Übersicht, Konsole oder Befehle, Logs, Verlauf; Einträge und Reiter ohne Recht werden ausgeblendet | UI | Muss | R1 | 1 |
| UI-012 | Mobile Nutzung | Karten, Befehlsseite und Konsole auf dem Smartphone voll bedienbar, Mitteilungen ab R2; Verwaltung mobil nur lesend, ausgenommen Sperren, Entsperren und Sitzungen beenden mit Bestätigungsdialog | UI | Soll | R1 | 2 |
| UI-013 | Passwort einblendbar | Passwortfelder von Anmeldung, Passwortwechsel und Einladung lassen sich kurz einblenden; beim Absenden und nach 10 s sind sie wieder verborgen | UI | Soll | R1 | 1 |
| UI-014 | Erklärungen einblendbar | Die Erklärungen in den Einstellungen lassen sich ein- und ausblenden; der Zustand gilt je Benutzer im Browser | UI | Kann | R1 | 1 |

### 2.11 Audit und Statistik

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| AUD-001 | Audit-Log | Wer, wann, Server, Befehl, Ergebnis, abgelehnt ja/nein; auch Agent-Aktionen und Verwaltungsänderungen; ein Befehl erzeugt die Einträge cmd.sent und cmd.result (Design 3.3). Ausnahme: Bleibt die Datenbank länger als 2 min gesperrt, steht ein Eintrag außerhalb einer Transaktion gekennzeichnet und vollständig als audit.entry\_lost im Log | Backend, DB | Muss | R1 | 2 |
| AUD-002 | Statistiken | Interne Statistiken aus den klassifizierten Befehlen | Backend, UI | Kann | R2 | 1 |
| AUD-003 | Aufbewahrung | Einträge werden standardmäßig 90 Tage aufbewahrt und danach automatisch gelöscht; nur der RCON-Admin kann die Frist ändern | Backend | Muss | R1 | 1 |
| AUD-004 | Einsicht | RCON-Admin sieht das gesamte Audit-Log; Server-Admins sehen nur Einträge ihres Scopes und können die Frist nicht ändern | Backend, UI | Muss | R1 | 1 |

### 2.12 Mitteilungen

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| MSG-001 | Mitteilung RCON-Admin | RCON-Admin sendet Mitteilungen an alle, nur Server-Admins, nur Moderatoren oder einzelne Benutzer | Backend, UI | Soll | R2 | 1 |
| MSG-002 | Mitteilung Server-Admin | Server-Admin sendet Mitteilungen an alle Benutzer seines Scopes (Moderatoren und Supporter) oder nur an Moderatoren | Backend, UI | Soll | R2 | 1 |
| MSG-003 | Rückmeldung | Empfänger kommentieren eine Mitteilung; der Absender sieht die Rückmeldungen | Backend, UI | Soll | R2 | 1 |
| MSG-004 | Bewertung | Up- und Downvotes zu Mitteilungen | Backend, UI | Kann | R2 | 1 |
| MSG-005 | Umfragen | Einfache Umfragen als Mitteilung mit Auswahloptionen | Backend, UI | Kann | R2 | 1 |

### 2.13 Dokumentation

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| DOC-001 | Installation | Quick-Start; jede Umgebungsvariable und jede Einstellung mit Zweck, Typ, Standard und erlaubten Werten oder Bereichen | Doku | Muss | R1 | 1 |
| DOC-002 | Überblick | Idee des Projekts, Panel und Agent, warum es einen Agent gibt | Doku | Muss | R1 | 1 |
| DOC-003 | Rollen und Rechte | Rollen, ihre Rechte und ihre Zuweisung | Doku | Muss | R1 | 1 |
| DOC-004 | Oberfläche | Aufbau und Bedienung der Oberfläche | Doku | Soll | R1 | 1 |
| DOC-005 | Technik | Spielprofile und Befehlslisten (Ablage, Laden, Vorrang); Kopplung, Schlüssel, Pinning, Widerruf; Verschlüsselung | Doku | Soll | R1 | 1 |

## 3. Rollen- und Rechtemodell

Vier feste Rollen in strikter Hierarchie. Rechte fließen nur nach unten; Verwaltungsrechte werden nie weitergegeben.

| Rolle | Scope | Befehle | Agent-Funktionen | Verwaltet |
| --- | --- | --- | --- | --- |
| RCON-Admin | alle Server | frei, ohne Filter | alle, überall | alles: Server, Servergruppen, Agents, Server-Admins, globale Supporter-Freigabe |
| Server-Admin | zugewiesene Servergruppen und/oder einzelne Server | frei, ohne Filter | alle, nur im Scope | Moderatoren, Moderatorgruppen, Befehlslisten, weitergebbare Befehle, Supporter-Freigabe je Moderator, Verlaufssichtbarkeit |
| Moderator | zugewiesene Server oder Servergruppen im Scope seines Server-Admins | nur Befehlsliste seiner Moderatorgruppe | keine | Supporter je Server, falls freigegeben |
| Supporter | zugewiesene Server seines Moderators | nur vom Moderator vergebene, als weitergebbar markierte Befehle | keine | nichts |

Zuweisungskette: RCON-Admin → Server-Admin (Servergruppe oder Server) → Moderator (Server oder Servergruppe) → Supporter (Server).

- Ein Benutzer kann mehrere Zuweisungen haben, z. B. Server-Admin der Gruppe Minecraft und zusätzlich eines einzelnen ARK-Servers.
- Der Notfall-Admin ist ein lokaler RCON-Admin außerhalb jeder Identitätsquelle.
- Der Befehlskatalog schränkt niemanden ein; Einschränkung entsteht nur über Befehlslisten.

## 4. Architektur

### 4.1 Komponenten

| Komponente | Technik | Aufgabe |
| --- | --- | --- |
| Backend | Go, ein Binary | Auth, Rechte, Protokolladapter, Verbindungspool, Cache, Audit, API |
| UI | React mit TypeScript (Vite), statisch ins Binary eingebettet | Weboberfläche, Live-Updates per WebSocket |
| Agent | gleiches Go-Binary im Agent-Modus | Container-Steuerung und -Daten über den Docker-Socket, nur für freigegebene Container |
| Profile | YAML-Dateien im Repository | Protokoll, Ports, Befehlskatalog, Erkennungsmuster je Spiel |
| DB | SQLite, optional Postgres | Benutzer, Rechte, Server, Agents, Audit |

### 4.2 Befehlspfad

1. Benutzer sendet Befehl über UI.
2. Backend prüft Session, Rolle und Scope.
3. Bei Moderator und Supporter: Token-Matching gegen Befehlsliste, Wrapper-Prüfung.
4. Bereinigung von Steuerzeichen.
5. Markierung mit Request-ID, Session, Benutzer, Rolle; Einreihung in die Warteschlange des Servers.
6. Senden über die eine Verbindung zum Spieleserver; Antwort per Request-ID zurückgeordnet.
7. Audit-Log und Verlauf; Ablehnungen ebenfalls geloggt.

### 4.3 Agent

| Aspekt | Festlegung |
| --- | --- |
| Freigabe | Label am Container, Standard rconductor.manage=true |
| Funktionen | Start, Stop, Restart, Status, CPU, RAM, Netz, Disk-I/O, Logs, Volume-Größen, Versionserkennung |
| Verboten | Erstellen, Löschen, Exec, Ändern, Image- und Volume-Operationen, Hostdaten |
| Kopplung | Agent baut die Verbindung zum Panel auf (Panel-Adresse per Env, Agent ohne offenen Port); Pairing per Einmal-Token, danach gegenseitig gepinnte Ed25519-Schlüssel über TLS 1.3 |
| Datenfluss | Agent sammelt; Panel puffert im Cache; Live-Logs nur bei aktiver Ansicht |

### 4.4 Protokolle

| Adapter | Spiele (Beispiele) | Verschlüsselt | Target |
| --- | --- | --- | --- |
| Source RCON (TCP) | Minecraft, GTNH, ARK, Factorio | nein | R1 |
| HTTP-API | Satisfactory Dedicated Server API | ja, HTTPS | R2 |
| BattlEye RCon (UDP) | ARMA, DayZ | nein | R3 |
| WebRCON (WebSocket) | Rust | nur mit wss | R3 |
| Telnet | 7 Days to Die | nein | R3 |

Verschlüsseltes RCON gibt es praktisch nicht; Verschlüsselung nur bei Spielen mit HTTPS- oder wss-API. Deshalb gilt SEC-001.

### 4.5 Datenmodell (Kern)

| Tabelle | Inhalt |
| --- | --- |
| users | lokal angelegt; Pseudonym, Echtname, Profilbild, Kontaktangaben, Rolle, Auth-Quelle, Zwei-Faktor, Supporter-Freigabe je Moderator, angelegt von, letzte Anmeldung |
| sessions, invites | serverseitige Sessions und Einladungslinks, Tokens nur als Hash |
| user\_groups, user\_group\_members | lokale Benutzergruppen, u. a. Moderatorgruppen mit ihrer Befehlsliste |
| servers | Name, Profil, Protokoll, Host, Port, Zugangsdaten verschlüsselt, Agent, Container |
| server\_groups, server\_group\_members | Servergruppen und ihre Server |
| admin\_scopes | Server-Admin ↔ Servergruppe oder Server |
| grants | Moderator oder Moderatorgruppe ↔ Server oder Servergruppe, vergeben von |
| command\_lists, command\_list\_entries | Befehlslisten mit Token-Pfaden, weitergebbar je Eintrag |
| supporter\_grants | Supporter ↔ Moderator ↔ Server |
| agents, agent\_tokens | gekoppelte Agents mit Schlüssel, Status, Version; Kopplungs-Tokens |
| game\_profiles, catalog\_commands | importierte Spielprofile und Befehlskatalog |
| audit\_log | alle Befehle (zugleich Konsolenverlauf), Agent-Aktionen, Verwaltungsänderungen und Anmeldungen mit Ergebnis |
| settings | globale sowie je Server oder Servergruppe geltende Einstellungen |
| command\_templates, api\_tokens, Mitteilungstabellen | ab R2 |

Felder und Regeln im Detail: Tab Design, Kapitel 4.

Der Befehlskatalog wird aus den Profildateien in eine Tabelle catalog importiert. Detailliertes Datenmodell folgt.

### 4.6 Cache-Regeln

Der Cache liegt nur im Arbeitsspeicher des Panels und wird nach einem Neustart neu aufgebaut. Veraltete Werte werden mit Zeitstempel und Markierung „veraltet“ angezeigt, nie stillschweigend.

| Daten | Quelle | Aktualisierung | Gilt als veraltet nach | Invalidierung |
| --- | --- | --- | --- | --- |
| Containerstatus | Agent (Docker-Events) | sofort bei Ereignis | – | Agent getrennt → Status „unbekannt“ |
| CPU, RAM, Netz, Disk-I/O | Agent | alle 10 s | 30 s | Agent getrennt, Container gestoppt |
| RCON-Erreichbarkeit | Panel | bei Verbindungsereignis | – | Verbindungsabbruch |
| Spielerliste | RCON-Befehl bzw. Agent | alle 60 s, nur solange jemand die Server ansieht; sofort bei Bedarf | 120 s | Server-Neustart, RCON-Abbruch |
| Spielversion (ab R2, AGT-006) | Agent (Logmuster) | einmal nach Containerstart | bis zum nächsten Start | Container-Neustart, Image-Wechsel |
| Volume-Größen | Agent | alle 2 h | 6 h | Agent getrennt |
| Letzte Logzeilen | Agent | Ringpuffer der letzten 50 Zeilen | – | Container-Neustart |

## 5. Oberfläche

| Ansicht | Rollen | Inhalt |
| --- | --- | --- |
| Landingpage RCON-Admin | RCON-Admin | Gesundheitsübersicht (UI-003), alle Server-Karten nach Servergruppen |
| Landingpage Server-Admin | Server-Admin | Server-Karten im Scope mit Last, Restart, Version, Spielern; Spielports ab R2 |
| Landingpage Moderator | Moderator, Supporter | reduzierte Karten der eigenen Server |
| Konsole | RCON-Admin, Server-Admin | freie Eingabe mit Autovervollständigung, Verlauf, Favoriten |
| Befehlsseite | Moderator, Supporter | Befehls-Buttons mit Parameterfeldern, Verlauf |
| Logs | RCON-Admin, Server-Admin | Live- und rückwirkende Container-Logs |
| Verwaltung | je nach Rolle | Benutzer, Gruppen, Server, Servergruppen, Agents, Befehlslisten |
| Audit | RCON-Admin, Server-Admin (Scope) | Audit-Log mit Filter |
| Topologie | RCON-Admin, Server-Admin | Live-Graph, ab R2 |

## 6. Entscheidungen

| ID | Entscheidung | Begründung | Verworfene Alternative |
| --- | --- | --- | --- |
| D-001 | Panel und Agent in Go (nach Architektur und Design bestätigt) | ein Binary, gute RCON- und Docker-Bibliotheken, RE2-Regex | Node.js |
| D-002 | UI in React mit TypeScript (Vite), ins Binary eingebettet | ein Container, ein Web-Port; größte Basis an Mitwirkenden, React Flow für die Topologie | SvelteKit, Vue, getrennter Frontend-Container |
| D-003 | SQLite als Standard, Postgres für größere Installationen | keine Zusatzdienste im Kleinen; Postgres robuster bei Last; nur zwei Datenbanken zu pflegen | MariaDB als dritte Option |
| D-004 | Eigener Agent mit Label-Freigabe | Hoster behalten Kontrolle, Panel nie am Docker-Socket | Socket im Panel, Portainer als Voraussetzung |
| D-005 | Kopplung per Schlüsselpaar | einfach für Nicht-Profis | interne Mini-CA, step-ca |
| D-006 | Befehlskatalog in Dateien gepflegt, in die DB importiert, nur Orientierung | Erweiterung ohne DB-Migration, keine Scheinsicherheit | Katalog als Erlaubnisliste |
| D-007 | Keycloak nur zur Authentifizierung | Rollen bleiben im Panel, keine doppelte Wahrheit | Rollen aus Token-Claims |
| D-008 | Keine Checkbox zum Umgehen von Sperren | ohne Sperrlisten gegenstandslos | Bypass je Benutzer oder Zuweisung |
| D-009 | Versionserkennung im Agent | kein Logtransfer zum Panel | Parsen im Panel |
| D-010 | Eine RCON-Verbindung je Spieleserver | Verbindungslimits, saubere Zuordnung, gemeinsamer Verlauf | Verbindung je Benutzer |
| D-011 | Kein HTTPS für die Weboberfläche | TLS ist Aufgabe des Reverse Proxy | eingebautes TLS |
| D-012 | Lizenz GNU AGPL-3.0 | auch gehostete, veränderte Fassungen müssen ihren Quellcode offenlegen | MIT, GPL-3.0 |
| D-013 | Projektname ShadowGhost.NET Game RCONductor | RCON plus Dirigent, der viele Server führt | Gatehouse, Consul, Rcontrol |
| D-014 | Konsole als Blöcke mit Zeitleiste und getrennten Spuren | Antworten bleiben auffindbar, auch wenn Logs und Befehle anderer dazwischenkommen | reines Fließtext-Terminal |
| D-015 | Master-Key automatisch erzeugt, getrennt in /secrets | kein Aufwand für Betreiber; ein Datenbank-Leck allein gibt keine Passwörter preis; Recovery-Export verhindert Totalverlust | Key per Env-Pflicht, Key im Datenverzeichnis |
| D-016 | Agent baut die Verbindung zum Panel auf | Agent ohne offenen Port | Panel verbindet sich zum Agent |
| D-017 | Postgres als eigener Compose-Dienst | ein Container kann keinen zweiten Container anlegen; Panel übernimmt Datenbank, Schema und Migrationen selbst | Postgres im Panel-Image |
| D-018 | Versionierte, eingebettete Schema-Migrationen (z. B. goose) | vorwärtsgerichtet und der Reihe nach angewendet, damit Sprünge über mehrere Versionen funktionieren; gleiche Logik für SQLite und Postgres | manuelle SQL-Skripte, automatischer Schema-Abgleich per ORM |
| D-019 | Eigener Agent-Port mit TLS 1.3 im Panel, ohne Reverse Proxy | Schlüssel-Pinning funktioniert nur Ende zu Ende; zwischen Agent und Panel kommt nie ein Proxy zum Einsatz | Agent über den Reverse Proxy mit Signatur auf Anwendungsebene |
| D-020 | Zwei Installationsvarianten: getrennt (empfohlen) und All-in-One | Host-Betreiber rollt den Agent selbst aus und koppelt ihn an ein fremdes Panel; Einzelbetreiber braucht nur eine Datei | nur All-in-One |
| D-021 | Getrennte Images für Panel und Agent | eigener Release-Stand, Agent klein und ohne Panel-Code | ein Image mit zwei Betriebsarten |
| D-022 | Agent-Protokoll als WebSocket mit JSON | einfach zu debuggen, geringes Datenvolumen | gRPC mit Protobuf, WebSocket mit CBOR |
| D-023 | Panel und Agent brauchen dieselbe Hauptversion | einfache, klar prüfbare Regel | Protokollversion N und N-1, nur Fähigkeitsliste |
| D-024 | Kopplung per Token, zusätzlich per Fingerprint-Freigabe (standardmäßig aus); nur RCON-Admin | Token sichert die Panel-Identität; Freigabe erspart den Token-Austausch, öffnet aber Angriffsfläche | nur ein Weg, Kopplung auch durch Server-Admins |
| D-025 | REST-API mit serverseitigen Sessions und WebSocket-Abos | läuft ohne Sonderkonfiguration hinter HAProxy; Sessions sofort widerrufbar | GraphQL, JSON-RPC, JWT, Server-Sent Events |
| D-026 | Wrapper-Befehle für Moderatoren und Supporter immer gesperrt | keine Umgehung über Verschachtelung, keine rekursive Prüfung nötig | rekursive Prüfung des inneren Befehls |
| D-027 | UUIDv7 als ID für alle Datensätze | zeitlich sortierbar, nicht erratbar, gleich in SQLite und Postgres | fortlaufende Zahlen, ULID |
| D-028 | Soft-Delete, endgültiges Entfernen nach Ablauf der Audit-Frist | Audit-Einträge bleiben verknüpft und lesbar | hartes Löschen mit Namenskopie im Audit |
| D-029 | Audit-Log als Tabelle in derselben Datenbank, zugleich Konsolenverlauf | ein Backup, Auswertung per Join, keine doppelte Speicherung von Befehlen | eigene SQLite-Datei für das Audit |
| D-030 | AES-256-GCM mit Key-ID je verschlüsseltem Wert | späterer Key-Wechsel ohne Schemaänderung | ein fester Key ohne Kennung |
| D-031 | Umgebungsvariablen mit Präfix RCD\_ | keine Kollision mit anderen Diensten in einer gemeinsamen Compose-Datei | RCONDUCTOR\_, kein Präfix |
| D-032 | Standardports 8080 (Web) und 8443 (Agent) | gängig und sofort als HTTP bzw. TLS erkennbar | 4326 und 4327 wie rcon-web-admin |
| D-033 | Nur Umgebungsvariablen, Geheimnisse zusätzlich per \_FILE; Laufzeit-Einstellungen im Panel | eine Konfigurationsquelle, Docker-Secrets nutzbar | zusätzliche YAML-Datei |
| D-034 | Kopplung auch in der All-in-One-Variante per Token | ein einheitlicher Ablauf für beide Varianten | automatische Kopplung über ein gemeinsames Volume, Freigabe per Klick |
| D-035 | Seitenleiste links, Serverseite mit Reitern, Verwaltung als Tabelle mit Seitenpanel | vertraut aus Portainer, Rollen blenden nur Reiter und Einträge aus, Liste bleibt beim Bearbeiten sichtbar | obere Leiste, Serverliste links, eine Seite ohne Reiter, Detailseiten, Dialoge |
| D-036 | Mobil voll für Moderation, Verwaltung nur lesend | Moderation passiert unterwegs, Verwaltung am Schreibtisch; spart Aufwand bei komplexen Formularen | alles mobil, nur Desktop |
| D-037 | Stilrichtung laut den Ansichten in Design 6.5; automatisches Design nach Systemeinstellung als Standard, hell und dunkel wählbar | Mockups abgenommen; Statusfarben und Monospace-Namen lesen sich auf dunklem Grund am besten | Standard nach Systemeinstellung |
| D-038 | Datenbankzugriff mit sqlc; SQLite über modernc.org/sqlite, Postgres über pgx | typsicher und SQL-nah; reines Go ohne CGO erlaubt statische Binaries | Bun, ent, mattn/go-sqlite3 (CGO) |
| D-039 | OpenAPI 3.1 als einzige Quelle für Server-Stubs (oapi-codegen) und Client-Typen (openapi-typescript) | API, Backend und Oberfläche laufen nicht auseinander; Doku der externen API ab R2 fällt mit ab | Typen von Hand pflegen |
| D-040 | Images für amd64 und arm64, signiert und mit SBOM auf ghcr.io und Docker Hub (PRJ-004) | x86-Server und Raspberry Pi; nachprüfbare Lieferkette | zusätzlich armv7, nur amd64 |
| D-041 | Parametertypen player, text, choice, number im Spielprofil; eigene Befehlsdefinitionen (CMD-018) sollen sie übernehmen | CMD-003 braucht Parameterfelder schon in R1, die Argument-Syntax ist vertagt | nur ein freies Argumentfeld je Befehl |
| D-042 | Abnahme teilweise über Ereigniscodes in den Container-Logs, gelesen per Portainer-MCP | nutzt vorhandenen Zugang ohne neuen Code im Panel; Claude kann Kriterien direkt prüfen | eigener MCP-Endpunkt im Panel |
| D-043 | Session-Cookie immer HttpOnly und SameSite=Lax; Secure und Präfix \_\_Host- nur über HTTPS | Anmeldung auch ohne TLS-Proxy im LAN (D-011); Lax lässt Links von außen angemeldet ankommen, CSRF bleibt durch die Origin-Prüfung abgedeckt | Secure und SameSite=Strict immer |
| D-044 | Jede Änderung von Rolle, Gruppenmitgliedschaft, Grants, Supporter-Freigabe oder Sperre beendet die Sessions des Benutzers; Listeninhalte und das Zuweisen oder Entfernen von Servern und Servergruppen im Scope eines Server-Admins wirken ohne Abmeldung, die Oberfläche aktualisiert sich live, entzogener Zugriff wirkt sofort auf REST und WebSocket | keine offene Oberfläche mit veraltetem Stand; Listenpflege meldet niemanden ab | nur Deaktivierung beendet Sessions |
| D-045 | Profilbild als BLOB in der Datenbank, neu kodiert und verkleinert | ein Backup enthält alle veränderlichen Daten; Metadaten wie EXIF entfallen | Datei unter /data |
| D-046 | Benutzer entstehen nur per Einladung, dazu der Notfall-Admin; das Token steht nur im URL-Fragment | niemand vergibt Passwörter für andere; ein Weg statt zwei; das Token erscheint in keinem Proxy- oder Panel-Log | POST /users mit Passwortvergabe |
| D-047 | Versionen sind zweistellig (1.0, 1.1); Inkompatibles erhöht die erste Stelle (D-023), alles andere die zweite; Release-Tags wie v1.0, Image-Tags 1 und 1.0. Testversionen 0.x haben nur das Image-Tag 0.x, kein Haupt- oder latest-Tag, sind GitHub-Pre-Releases und stehen nicht im CHANGELOG | die erste Stelle trägt allein die Regel für Panel und Agent, eine dritte Stelle sagt nichts Zusätzliches | dreistellige Versionen (1.0.0) |
| D-048 | Verwaltung speichert mit explizitem Save sofort; Rückgängig ist eine Gegenänderung binnen 5 s mit eigenem Audit-Eintrag. Kein Rückgängig bei Änderung von Host, Port oder RCON-Passwort; beendete Sessions stellt Rückgängig nicht wieder her | Änderungen sind sofort wirksam und ohne Überraschung; das RCON-Passwort ist nicht lesbar und wird nie im Panel aufgehoben | Senden erst nach 5 s (bis 2.7) |
| D-049 | Verbindungstest mit unveränderten Daten antwortet aus dem Status der laufenden Verbindung; nur neue oder geänderte Daten lösen einen kurzen Testaufbau aus (nur Authentifizierung, kein Befehl, sofort geschlossen) | PROT-002 bleibt für bestehende Server gewahrt; das gespeicherte Passwort geht nie an eine neue Adresse | immer eine zweite Verbindung |

## 7. Releaseplan

| Release | Inhalt |
| --- | --- |
| R1 | Lokale Benutzer und Gruppen mit Profil (Pseudonym, Bild, Kontakte), Notfall-Admin, Login-Schutz, Rollenmodell, Servergruppen, Source RCON, Spielprofile, Konsole und Befehlsseite, Schnelleingabe, Agent mit Kopplung, Landingpages, Audit, Volumes, Migrationen, Backups, Ratenlimit, Sicherheitsmaßnahmen aus dem Bedrohungsmodell, Handy-Ansicht, zwei Installationsvarianten, Englisch |
| R2 | Mitteilungen mit Rückmeldung, Deutsch, Zwei-Faktor, Inaktivitätssperre, Befehlsvorlagen, Befehlspalette, Farbcodes, Befehlskategorien, Versionserkennung, Volume-Größen, Ports auf Karten, Verlaufssichtbarkeit, Recovery-Export, Postgres-Anbindung, Keycloak-Auth, HTTP-API-Adapter, Mehrfach- und Zeitbefehle, Argument-Einschränkung, Topologie, Metriken, Statistiken, Votes und Umfragen, externe API |
| R3 | Weitere Protokolladapter, Agent-Backends systemd und Kubernetes |
| Backlog | Benachrichtigungen (z. B. Matrix), spielspezifische Auswertung (Chat, Kick/Ban per Klick), eigene Befehlsdefinitionen mit Argumenten, Audit-Hash-Kette |

## 8. Offene Punkte

1. Argument-Syntax für eigene Befehlsdefinitionen (CMD-018), in einer späteren Version; Ausgangspunkt sind die Parametertypen aus D-041.
2.

## 9. Änderungshistorie

| Version | Datum | Änderung |
| --- | --- | --- |
| 1.0 | 01.11.2026 | Erstfassung aus Konzeptsitzung; Entwurf, nicht freigegeben |
| 1.1 | 01.11.2026 | Tabs Architektur und Design angelegt; Design-Entscheidungen eingearbeitet: D-019 bis D-026, neue Anforderungen ROLE-010, CAT-005, CMD-019, AGT-013, NFR-014, NFR-015; Revision 2 für CMD-005, AGT-005, AGT-012, NFR-001, NFR-003; Datenmodell mit D-027 bis D-030, Abschnitt 4.5 überarbeitet; Konfiguration mit D-031 bis D-034, Revision 2 für NFR-011; Oberfläche mit D-035, D-036, UI-011, UI-012; Landingpage Server-Admin an UI-005 angeglichen; Mockups mit D-037 verlinkt (Design 6.5) |
| 1.2 | 01.11.2026 | Design: Zeitwerte des Agent-Protokolls und Ratenlimits recherchiert und festgelegt (Ping 30 s, Abbruch 60 s, Backoff nach gRPC bis 120 s, Token 24 h); Handy-Mockups auf der Mockup-Fläche |
| 1.3 | 01.11.2026 | Tab Technologie angelegt; D-001 bestätigt, D-002 auf React geändert, D-038 bis D-040 neu |
| 1.4 | 01.11.2026 | Design 7 Spielprofile; Tabs Abnahme R1, Sicherheit (Bedrohungsmodell) und Plan R1 angelegt; SEC-006 bis SEC-011 und D-041 neu; Revision 2 für SRV-002 (Master-Key in /secrets) und CAT-001 |
| 1.5 | 01.11.2026 | Testbarkeit über Ereignis-Logs: Design 8, Abnahme R1 Abschnitt 9, NFR-016, SEC-012, D-042 |
| 2.0 | 01.11.2026 | Freigabe durch Rob; Basis für R1. Änderungen ab jetzt nur mit neuer Revision der betroffenen Anforderung und Eintrag hier |
| 2.1 | 01.11.2026 | Freigabe durch Rob: Erkenntnisse aus M0 und M1 eingearbeitet (Kapitel 10); Revision 2 für NFR-010; AUTH-010, AUTH-011, SEC-013 und D-043 bis D-046 neu. Änderungen ab jetzt wieder nur mit neuer Revision der betroffenen Anforderung und Eintrag hier |
| 2.2 | 02.11.2026 | Freigabe durch Rob: Erkenntnisse aus M2 eingearbeitet (Kapitel 11); Revision 2 für SEC-006, SEC-008 und SRV-001; Design 3.8 (RCON-Verbindung) neu; Einstellungen profiles.poll\_interval, ws.max\_sockets\_per\_session, ws.max\_subscriptions\_per\_socket; Grund-Code timeout. Änderungen ab jetzt wieder nur mit neuer Revision der betroffenen Anforderung und Eintrag hier |
| 2.3 | 03.11.2026 | Freigabe durch Rob: Erkenntnisse aus M3 eingearbeitet (Kapitel 12); Design 1.6 mit den Grenzen von SEC-007, Containername statt Container-ID, Fremdschlüssel servers.agent\_id, Einstellung agents.fingerprint\_pairing, Ereigniscode agent.disconnected, Grund-Codes agent\_unavailable und action\_failed, Grenzen bis zum welcome. Änderungen ab jetzt wieder nur mit neuer Revision der betroffenen Anforderung und Eintrag hier |
| 2.4 | 05.11.2026 | Freigabe durch Rob: Erkenntnisse aus M4 eingearbeitet (Kapitel 13); Revision 3 für SEC-006, Revision 2 für SEC-009, SEC-013, PRJ-002, CMD-001, CMD-004, UI-001, UI-004, UI-006, UI-007 und UI-012; DOC-001 bis DOC-005, PRJ-003, PRJ-004 und D-047 neu; neue Ressourcen in Design 2.2, Katalogprüfung in Design 3.3, optimistische Sperre, Ereigniscode logs.tail |
| 2.5 | 05.11.2026 | Freigabe durch Rob: Begründungen aus den Kandidaten eingearbeitet, Trigger der optimistischen Sperre und group\_add nachgetragen, Verweise auf Mockups und Kandidaten-Dateien entfernt (Kapitel 14). Keine Anforderung ändert sich |
| 2.6 | 06.11.2026 | Freigabe durch Rob: D-037 und Design 6.5 ohne Verweis auf Mockups, D-047 um Testversionen 0.x ergänzt (Kapitel 15). Keine Anforderung ändert sich |
| 2.7 | 06.11.2026 | Freigabe durch Rob: Ergebnisse der Abnahme und des Reviews des Bedrohungsmodells eingearbeitet (Kapitel 16); Revision 2 für CAT-002, AGT-013, AUD-001, NFR-009; Revision 3 für SEC-013 |
| 2.8 | 06.11.2026 | Freigabe durch Rob: Funde aus dem Test von 0.9 eingearbeitet (Kapitel 17); Revision 2 für AUTH-006, AGT-002, PROT-002, UI-008; Revision 3 für UI-001, UI-004; SRV-007, UI-013, UI-014, D-048 und D-049 neu; D-037 und D-044 geändert |

## 10. Änderungen in Version 2.1

Eingearbeitet aus den Kandidaten von M0 und M1.

| Nr. | Änderung | Fundstelle in 2.1 |
| --- | --- | --- |
| 1 | /healthz und /metrics reserviert, nie von der Oberfläche beantwortet | Design 2.1 |
| 2 | Regeln zur Auslieferung der eingebetteten Oberfläche | Design 6.1 |
| 3 | Read-only Root-Dateisystem für Panel und Agent, read\_only in Compose | Design 5.4 bis 5.6, Technologie 4, Abnahme SEC-002 |
| 4 | base href je RCD\_BASE\_PATH für Deep-Links | Design 6.1, Abnahme NFR-003 |
| 5 | AGT-001 prüft das Panel-Binary statt go.mod | Abnahme AGT-001 |
| 6 | Tests dürfen CGO nutzen (Race-Detector) | Technologie 1 |
| 7 | Neue Anforderung SEC-013 Lieferkette | 2.1, Sicherheit 4, Abnahme, Plan M0, Technologie 4 |
| 8 | Socket-Zugriff des Agents wird in M3 entschieden, Stub als Nicht-Root | Technologie 5 |
| 9 | Verzeichnisse scripts und deploy/compose im Repository | Technologie 4 |
| 10 | Session-Cookie: Secure und \_\_Host- nur über HTTPS, SameSite=Lax; Proxy-Auswertung in M1 | Design 2.1, D-043, Sicherheit T-001 und T-007, Plan M1 |
| 11 | Rechteänderungen beenden Sessions | Design 2.1, D-044 |
| 12 | Sichtbarkeit der Profilfelder | Design 3.6 |
| 13 | Profilbild als BLOB; Revision 2 von NFR-010 | 2.1 NFR-010, Design 4.2, D-045 |
| 14 | grants nennt immer die Moderatorgruppe | Design 4.2, 4.3 |
| 15 | Benutzer nur per Einladung, kein POST /users | Design 2.2, D-046 |
| 16 | Regel, wer wen verwaltet | Design 3.6 |
| 17 | Zuweisungen ersetzen nur eigene Einträge; Servergruppe nur ganz im Scope | Design 2.2, 3.6 |
| 18 | Rollenwechsel entfernt die Zuweisungen | Design 3.6 |
| 19 | Listenänderungen beenden keine Sessions | Design 2.1, D-044 |
| 20 | Login-Schutz mit Werten und Zählern | Design 3.7 |
| 21 | Neue Variable RCD\_PASSWORD\_HASH\_CONCURRENCY | Design 5.2, 3.7 |
| 22 | Initialpasswort als Hinweisblock auf stderr | Design 8.1 |
| 23 | Liste der Laufzeit-Einstellungen mit Standardwerten | Design 5.6 |
| 24 | Grund-Codes forbidden und busy | Design 3.5 |
| 25 | Zusätzliche API-Pfade | Design 2.2 |
| 26 | Profilbild nur PNG und JPEG, verkleinert auf 512 Pixel | Design 4.3 |
| 27 | Teilabnahmen über Meilensteine | Plan R1, Kapitel 3 |
| 28 | Neue Anforderung AUTH-010: erzwungener Passwortwechsel mit Grund-Code password\_change\_required; Passwort-Reset durch Verwaltende | 2.2 AUTH-010, Design 2.2, 3.5, 3.6, 4.2, Abnahme, Plan M2 |
| 29 | Ratenlimits als Einstellungen ratelimit.<rolle>.per\_minute und .burst | Design 3.4, 5.6 |
| 30 | Neue Anforderung AUTH-011: Wiederherstellung des Notfall-Admins per RCD\_EMERGENCY\_ADMIN\_RESET, Ereigniscode auth.emergency\_admin.reset; Verdrängung im Login-Schutz festgelegt | 2.2 AUTH-011, Design 3.6, 3.7, 5.2, 8.3, Abnahme, Plan M2 |

## 11. Änderungen in Version 2.2

Eingearbeitet aus den Kandidaten von M2, Nr. 1 bis 10; Nr. 11 ergibt sich aus dem Abschluss von M2, Nr. 12 aus dem Review.

| Nr. | Änderung | Fundstelle in 2.2 |
| --- | --- | --- |
| 1 | Beispielprofil Minecraft auf Englisch wie das ausgelieferte Profil | Design 7.4 |
| 2 | Hot Reload mit Abgleich in festem Intervall, Einstellung profiles.poll\_interval | Design 5.6, 7.1, Technologie 2 |
| 3 | Sichtbarkeit der Serverfelder je Rolle | Design 2.2 |
| 4 | Änderungen an Servergruppen beenden keine Sessions; WebSocket-Abos werden sofort neu geprüft | Design 2.1, 2.3 |
| 5 | Revision 2 von SRV-001: Agent und Container am Server erst ab M3 setzbar | 2.4 SRV-001, Design 2.2, Plan R1 Kapitel 3 |
| 6 | Eigener Source-RCON-Adapter statt gorcon/rcon; Endmarker erst nach dem ersten Antwortpaket | Design 3.8, Technologie 2 |
| 7 | RCON-Verbindung: Prüfung im Leerlauf alle 2 s, Backoff höchstens 5 s; Design 1.1 gilt für den Agent | Design 1.1, 3.8 |
| 8 | Audit eines Befehls nur anhängend (cmd.sent, cmd.result); 504 mit Grund-Code timeout | Design 3.3, 3.5, 3.8, 4.1 |
| 9 | Revision 2 von SEC-006: Steuer- und Formatzeichen aus Bezeichnern, Steuerzeichen aus Freitext entfernt | 2.1 SEC-006, Design 3.3, Sicherheit 4, Abnahme SEC-006 |
| 10 | Revision 2 von SEC-008: Sockets je Session und Abos je Socket als Einstellungen, ältester Socket weicht; Abo-Bestätigung subscribed | 2.1 SEC-008, Design 2.3, 5.6, Sicherheit T-018 und 4, Abnahme SEC-008 |
| 11 | Teilabnahmen aus M2: SEC-006, SRV-003 und AUTH-006 mit E2E-Anteil werden in M4 abgenommen | Plan R1, Kapitel 3 |
| 12 | RCON-Verbindung: TCP-Keepalive und Probe im Leerlauf alle 30 s gegen halboffene Verbindungen; Zeitlimit 10 s ab Annahme einschließlich Warteschlange, volle Warteschlange 503 busy | Design 3.5, 3.8 |

## 12. Änderungen in Version 2.3

Eingearbeitet aus den Kandidaten von M3, Nr. 1 bis 12; Nr. 13 aus dem Review.

| Nr. | Änderung | Fundstelle in 2.3 |
| --- | --- | --- |
| 1 | Container über den Namen statt über die ID; Label-Prüfung bei jeder Auflösung | Design 1.6, 4.2 |
| 2 | Zahlenwerte der Agent-Grenzen; Fehlercodes rate\_limited und error | Design 1.5, 1.6 |
| 3 | Schalter agents.fingerprint\_pairing mit Selbstabschaltung nach 30 min und Audit | Design 1.2, 5.6 |
| 4 | Token-Format rcd1., \_FILE-Variante, Agent-ID vom Panel, reject pending und invalid | Design 1.2, 1.3 |
| 5 | Ereigniscode agent.disconnected | Design 8.3 |
| 6 | Grund-Codes agent\_unavailable (503) und action\_failed (502); Aktionen im Audit | Design 2.2, 3.5 |
| 7 | Agent als Nicht-Root mit group\_add, Socket-Proxy linuxserver/socket-proxy | Technologie 2, 5; Sicherheit T-022 |
| 8 | Cache-Kapitel: Spielversion ab R2, Disk-I/O bei den Messwerten; Verweis der Abnahme NFR-005 | 4.6, Abnahme NFR-005 |
| 9 | Teilabnahme AGT-005 in M4 | Plan R1, Kapitel 3 |
| 10 | Fremdschlüssel servers.agent\_id mit ON DELETE SET NULL; Widerruf löst Server vom Agent | Design 1.7, 4.2 |
| 11 | Socket-Proxy: ALLOW\_RESTARTS erlaubt auch kill (Restrisiko) | Sicherheit T-022 |
| 12 | WebSocket-Themen metrics und logs: Rechte, Startwert, Logs nur bei Abo | Design 2.3 |
| 13 | TLS-Handshake lässt unbekannte Agent-Schlüssel zu, Entscheidung im hello, pin\_mismatch nur bei bekannter Agent-ID; bis zum welcome höchstens 16 KiB je Nachricht und hello binnen 10 s | Design 1.3, 1.6 |

## 13. Änderungen in Version 2.4

Eingearbeitet aus den Kandidaten von M4, Nr. 1 bis 31; Nr. 32 bis 36 aus der Freigabe durch Rob.

| Nr. | Änderung | Fundstelle in 2.4 |
| --- | --- | --- |
| 1 | /backups und die Backup-Seite folgen nach M5, nicht in 1.0 | Design 2.2, 6.2; Plan R1, Kapitel 4 |
| 2 | started\_at und restart\_count in snapshot und container.event; Regel für Restart-Schleifen | Design 1.5, 6.1 |
| 3 | servers.overview mit Laufzeitdaten aller Server im Scope | Design 2.3 |
| 4 | runtime im Server-Objekt | Design 2.2 |
| 5 | GET /servers/{id}/commands | Design 2.2 |
| 6 | Favoriten je Benutzer, PUT /me/favorites, Tabelle user\_favorites | Design 2.2, 4.2 |
| 7 | Einstellung ui.shortcuts\_enabled, lesbar über /auth/session | Design 2.2, 5.6 |
| 8 | Rückgängig im Browser mit 5 s Verzögerung | Design 6.1 |
| 9 | Farbcodes in R1 entfernt, Stilliste mit CMD-017 | SEC-006, T-008, Sicherheit, Abnahme SEC-006 |
| 10, 25 | Mitteilungen in UI-012 erst mit R2 | UI-012, Design 6.1 |
| 11 | Spielversion auf Karten erst mit R2 | UI-004, UI-006 |
| 12 | CMD-001 in R1 mit CMD-012 bis CMD-015 | CMD-001 |
| 13 | shadcn/ui als gekennzeichneter Fremdcode | PRJ-002, Technologie 3 |
| 14 | Ablehnung nach Rolle vor dem Objekt | Design 3.1, 3.5 |
| 15 | supporters\_enabled und supporters\_allowed in Session.ui | Design 2.2 |
| 16 | Neustart ohne Agent in der Oberfläche | Design 6.3 |
| 17 | Kopieren ohne sicheren Kontext | Design 6.1, README |
| 18 | Betriebshinweis zu parallelen RCON-Clients | README |
| 19 | Prüfung der Katalogparameter im Backend, syntax.player\_pattern | CMD-004, Design 3.3, 3.5, 7.2, 7.3; Abnahme CMD-004; Sicherheit T-006 |
| 20 | GET /servers/{id}/logs, Ereigniscode logs.tail | Design 2.2, 8.3 |
| 21 | DELETE /users/{id}/sessions | Design 2.2 |
| 22 | GET /agents/{id}/containers | Design 2.2 |
| 23 | Einladungstoken nur im Fragment | Design 2.2, D-046 |
| 24 | Optimistische Sperre mit version und If-Match | Design 2.1, 3.5, 4.2 |
| 26 | Sperren, Entsperren und Sitzungen beenden auf dem Handy | UI-012, Design 6.1, 6.6, Abnahme UI-012 |
| 27 | Privater Link auf die Mockups entfernt | Design 6.5 |
| 28 | Bidi-Steuerzeichen im Freitext als sichtbarer Platzhalter | SEC-006, Abnahme SEC-006, Design 6.1 |
| 29 | SEC-009 gilt für den Web-Port | SEC-009, Sicherheit, Abnahme SEC-009 |
| 30 | Nicht-Text-Kontrast 3:1 (WCAG 1.4.11) | UI-001, Abnahme UI-001, Design 6.1 |
| 31 | Suche in allen Verwaltungslisten; Filter und Mehrfachauswahl nur in der Benutzerliste | UI-007, Abnahme UI-007, Design 6.1 |
| 32 | Bestätigungsdialog auch bei Rollenwechsel und beim globalen Abschalten der Supporter | Design 6.1 |
| 33 | Kapitel 2.13 Dokumentation: DOC-001 bis DOC-005, Umsetzung in M5 | 2.13, Abnahme 10, Plan R1 |
| 34 | GitHub-Release je Version | PRJ-003, Abnahme PRJ-003, Plan R1 |
| 35 | Images zusätzlich auf Docker Hub | PRJ-004, SEC-013, D-040, Sicherheit, Technologie 4, Abnahme PRJ-004 und SEC-013, Plan R1 |
| 36 | Zweistellige Versionen; die Versionsprüfung von AGT-013 folgt in M5 | D-047, Design 1.4, Technologie 4, Plan R1 |

## 14. Änderungen in Version 2.5

Vorbereitung des Release 1.0: Die Kandidaten-Dateien und die Mockups liegen nicht mehr im Repository. Begründungen, die nur dort standen, sind übernommen.

| Nr. | Änderung | Fundstelle in 2.5 |
| --- | --- | --- |
| 1 | Begründung: Escaping verhindert keine Täuschung durch Bidi-Zeichen; andere Cf-Zeichen wie der Zero Width Joiner bleiben im Freitext | Design 6.1 |
| 2 | Begründung: Rollenwechsel entfernt Zuweisungen, Reste würden nach einem Rückwechsel wieder wirken | Design 3.6 |
| 3 | Begründung: Label-Prüfung bei jeder Auflösung des Containernamens | Design 4.2 |
| 4 | Begründung: Reservierte Pfade verhindern einen falschen Healthcheck | Design 2.1 |
| 5 | Begründung: WebP entfällt beim Profilbild | Design 4.2 |
| 6 | Begründung: Filter und Mehrfachauswahl nur bei Benutzern, die übrigen Listen sind kurz | Design 6.1 |
| 7 | Beispiele und Abgrenzung der Parameterprüfung zu CMD-004 und CMD-009 | Design 3.3, 7.3 |
| 8 | Trigger der optimistischen Sperre nachgetragen (aus Kandidat 24 von M4) | Design 4.1 |
| 9 | group\_add in den Compose-Beispielen | Design 5.4, 5.5 |
| 10 | Satz zu den Mockup-Bildern entfernt, die Beschreibung der Ansichten bleibt; Dateiverweise in den Kapiteln 10 bis 13 durch die Kandidaten des Meilensteins ersetzt | Design 6.5; Kapitel 10 bis 13 |
| 11 | Platzhalter eingebetteter Grafiken entfernt; Abschnitt Mockups heißt Ansichten | Architektur 1, Design 6.4, 6.5, 6.6, Plan R1 |

## 15. Änderungen in Version 2.6

| Nr. | Änderung | Fundstelle in 2.6 |
| --- | --- | --- |
| 1 | Stilrichtung verweist auf die Ansichten | D-037 |
| 2 | Spaltenkopf Ansicht, Absatz Stil ohne Verweis auf die Mockup-Fläche | Design 6.5 |
| 3 | Testversionen 0.x: nur Image-Tag 0.x, kein Haupt- oder latest-Tag, GitHub-Pre-Release, kein CHANGELOG-Eintrag | D-047 |

## 16. Änderungen in Version 2.7

Aus der Abnahme in M5 und dem Review des Bedrohungsmodells.

| Nr. | Änderung | Fundstelle in 2.7 |
| --- | --- | --- |
| 1 | CAT-002 präzisiert: Der Katalog erlaubt nichts, seine Parameter schränken nur Moderator und Supporter ein | CAT-002 |
| 2 | Hinweis bei Versionsabweichung steht im Panel-Log und im Agent-Log | AGT-013, Design 1.4, Abnahme AGT-013 |
| 3 | Ein Befehl erzeugt zwei Audit-Einträge | AUD-001, Abnahme AUD-001 |
| 4 | Auth-Anbieter und Datenbanktreiber als Schnittstellen erst in R2 | NFR-009, Abnahme NFR-009, Architektur 7 |
| 5 | Kein ungeprüftes Automerge; Merge durch Claude Code nach grüner CI und Sichtung des Changelogs, Major nach Freigabe | SEC-013, Sicherheit |
| 6 | Header gelten für Antworten des Panels; Fehler von net/http vor dem Handler sind ausgenommen | Abnahme SEC-009 |
| 7 | Mit gelöschtem erzeugtem Master-Key startet das Panel; ein fehlender RCD\_MASTER\_KEY\_FILE stoppt den Start | Abnahme SRV-002, Design 5.2 |
| 8 | Abnahme SEC-007: echtes TLS mit gefälschter Registry genügt | Abnahme SEC-007 |
| 9 | Aufbewahrung des Audit-Logs mindestens 30 Tage | Design 5.6, Sicherheit T-009 |
| 10 | Befehlstrenner im Spielprofil (syntax.separators), abgelehnt für Moderator und Supporter | Design 3.3, 7.2, Sicherheit T-006 |
| 11 | Änderung von Host oder Port verlangt das RCON-Passwort | Design 2.2, Sicherheit T-011 |
| 12 | Grenzen für Anfragen und Login-Schutz | Design 2.1, 3.7, Sicherheit T-002, T-016, T-025 |
| 13 | Bedrohungsmodell: Status aktualisiert, T-019 im Panel, T-025 erweitert | Sicherheit |
| 14 | Compose-Beispiele mit cap\_drop, no-new-privileges und Limits | Design 5.4, 5.5 |
| 15 | Nachtrag: Eine erfolgreiche Anmeldung löscht nur die Zähler des eigenen Clients | Design 3.7, Sicherheit T-002 |
| 16 | Nachtrag: Offene Verbindungen am Web-Port insgesamt und je Client begrenzt (RCD\_MAX\_CONNECTIONS, RCD\_MAX\_CLIENT\_CONNECTIONS); schreibende Anfragen ohne Session werden vor dem Body abgelehnt, ohne Session höchstens 16 KiB | Design 2.1, 5.2, Sicherheit T-025 |
| 17 | Nachtrag: Größte echte Anfragen je Route belegt; Restrisiken für erfundene Namen, Verbindungslimit, 429 und gemeinsame Adressen | Design 2.1, Sicherheit Kapitel 5 |
| 18 | Nachtrag: Sperre je Client über alle Namen (login.max\_client\_failures); volle Zähltabelle lässt nichts ungeschützt durch, verdrängt werden nur beendete Zähler | Design 3.7, Sicherheit T-002, T-016, Kapitel 5 |
| 19 | Nachtrag: Bodys in Arbeit unabhängig von der Rolle begrenzt; Zähltabelle mit 100 000 Einträgen (login.max\_tracked), bei voller Tabelle Warteschlange mit Überlauf-Zähler; Verbindungen ohne Session enden nach RCD\_MAX\_ANONYMOUS\_CONNECTION\_SECONDS; Audit-Einträge gehen unter Last nicht verloren, sonst stehen sie im Log (audit.entry\_lost); Profilbilder nach Pixeln begrenzt, CMYK abgelehnt; Datenbank-Verbindungen begrenzt; GOMEMLIMIT 80 % der Speichergrenze; Schwereskala und Regel für Restrisiken | Design 2.1, 3.7, 5.2, Sicherheit Kapitel 3, 5, 6 |
| 20 | Nachtrag: AUD-001 mit Ausnahme bei Datenbanksperre über 2 min (audit.entry\_lost im Log); RCD\_PASSWORD\_HASH\_CONCURRENCY höchstens 2 bis zu einer Messung; Restrisiken freigegeben | AUD-001, Design 5.2, Sicherheit Kapitel 5 |

## 17. Änderungen in Version 2.8

Aus dem Test von 0.9 auf hydra.

| Nr. | Änderung | Fundstelle in 2.8 |
| --- | --- | --- |
| 1 | Standardlabel rconductor.manage=true statt rcon.manage=true, ohne Fallback auf den alten Namen (F-03) | AGT-002, 4.3, Design 5.2, Abnahme AGT-002 |
| 2 | Kein verzögertes Senden mehr: expliziter Save speichert sofort, danach Rückgängig als Gegenänderung; Ausnahmen Host, Port, RCON-Passwort; Nachfrage bei ungespeicherten Änderungen (F-07) | UI-008, D-048, Design 6.1, Abnahme UI-008 |
| 3 | Zuweisen und Entfernen von Servern und Servergruppen im Scope eines Server-Admins beendet keine Sessions mehr; WebSocket-Thema user.rights für die Aktualisierung (F-09) | D-044, AUTH-006, Design 2.1, 2.3, Abnahme AUTH-006 |
| 4 | Verbindungstest beim Anlegen und Bearbeiten; PROT-002 mit genau einer Ausnahme; Einstellung servers.connection\_test\_per\_minute; Ereigniscode rcon.connection\_test (F-04) | SRV-007, PROT-002, D-049, Design 2.2, 3.8, 5.6, 8.3, Sicherheit T-011, Abnahme SRV-007, PROT-002 |
| 5 | Design hell, dunkel oder automatisch, Standard automatisch (F-10) | UI-001, D-037, Design 6.1, Abnahme UI-001 |
| 6 | Ganze Karte klickbar, Hover und Fokus sichtbar (F-12) | UI-001, UI-004, Design 6.1, Abnahme UI-001, UI-004 |
| 7 | Passt die Antwort nicht zum Muster des Profils, ist die Spielerzahl unbekannt statt 0 (F-11) | UI-004, Design 2.3, Abnahme UI-004 |
| 8 | Passwort kurz einblendbar (F-01) | UI-013, Abnahme UI-013 |
| 9 | Erklärungen in den Einstellungen ein- und ausblendbar (F-18) | UI-014, Abnahme UI-014 |
| 10 | Server-Dialog schließt nach dem Anlegen; Sperren als Schalter, Einschalten mit Dialog (F-05, F-08) | Design 6.1 |
| 11 | Nachtrag: Betriebsgrenzen und Zeiten als Einstellung mit Standard und Mindestwert; Standardwerte unverändert (Inventar der Grenzen) | Design 5.2, 5.6 |
| 12 | Nachtrag: Verzögerung je Konto wächst bis 30 s; Grenze für falsche Passwörter beim Passwortwechsel | Design 3.7, Sicherheit R-11 |
