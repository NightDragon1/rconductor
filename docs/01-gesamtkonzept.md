# ShadowGhost.NET Game RCONductor – Gesamtkonzept

Projektname: ShadowGhost.NET Game RCONductor (kurz RCONductor). Dokumentversion 2.2, Stand 02.11.2026, Status zur Freigabe vorgelegt; Änderungen gegenüber 2.1 in Kapitel 11, gegenüber 2.0 in Kapitel 10.

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
- Affected Component: Backend, UI, Agent, Profile, DB.
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
| NFR-009 | Erweiterbarkeit | Niedrig priorisierte Anforderungen werden in der Architektur von Anfang an berücksichtigt, damit spätere Releases nichts umbauen müssen | alle | Muss | R1 | 1 |
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
| SEC-006 | Fremddaten | Daten von Spieleservern und Agents (Antworten, Spielernamen, Logs, Versionen, Container-Namen) werden escaped dargestellt und in der Größe begrenzt; Farbcodes nur in eine feste Liste erlaubter Stile. Steuer- und Formatzeichen (Unicode Cc und Cf, u. a. Bidi-Steuerzeichen) werden aus Bezeichnern wie Spielernamen und Container-Namen entfernt, Steuerzeichen außer Zeilenumbruch und Tabulator auch aus Freitext | Backend, UI | Muss | R1 | 2 |
| SEC-007 | Grenzen Agent-Port | Höchstgröße je Nachricht, begrenzte Logzeilen je Nachricht, Rate für Verbindungsaufbau je Quelladresse, Obergrenze für wartende Kopplungen | Backend, Agent | Muss | R1 | 1 |
| SEC-008 | WebSocket-Grenzen | Höchstzahl an WebSocket-Verbindungen je Session und an Abos je Verbindung, als Einstellungen des RCON-Admins (Design 2.3, 5.6) | Backend | Soll | R1 | 2 |
| SEC-009 | Sicherheitsheader | Content-Security-Policy ohne Inline-Skripte, X-Content-Type-Options, Referrer-Policy, frame-ancestors none | Backend | Muss | R1 | 1 |
| SEC-010 | Socket-Proxy | Doku empfiehlt einen Socket-Proxy vor dem Docker-Socket, der nur die vom Agent benötigten Endpunkte durchlässt; der Agent funktioniert damit unverändert | Agent, Doku | Kann | R1 | 1 |
| SEC-011 | Audit-Hash-Kette | Manipulationssichere Verkettung der Audit-Einträge mit Prüffunktion | Backend, DB | Kann | Backlog | 1 |
| SEC-012 | Keine Geheimnisse in Logs | Passwörter, RCON-Zugangsdaten, Tokens, Schlüssel und Session-IDs erscheinen auf keinem Log-Level; Sessions nur als kurzer Hash; einzige Ausnahme ist die einmalige Ausgabe des Initialpassworts (AUTH-007) | Backend, Agent | Muss | R1 | 1 |
| SEC-013 | Lieferkette | Images für amd64 und arm64 signiert (cosign keyless) mit SBOM (syft); govulncheck als Pflicht-Check; Actions auf Commit-SHA gepinnt; Abhängigkeits-Updates per Renovate ohne Automerge; Release-Tags erst nach Veröffentlichung des Repositories | alle | Muss | R1 | 1 |
| PRJ-001 | Open Source | Veröffentlichung auf GitHub unter GNU AGPL-3.0; Spielprofile sind Teil des Repositories | alle | Muss | R1 | 1 |
| PRJ-002 | Eigenständigkeit | Eigene Implementierung; Ideen anderer Tools nur sinngemäß, kein fremder Code, kein fremdes Design | alle | Muss | R1 | 1 |

### 2.2 Benutzerverwaltung

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| AUTH-001 | Lokale Benutzer | Benutzer werden immer lokal angelegt; lokale Passwörter mit argon2id | Backend, DB | Muss | R1 | 1 |
| AUTH-002 | Benutzergruppen | Lokale Benutzergruppen; Rechte können an Gruppen vergeben werden | Backend, UI | Muss | R1 | 1 |
| AUTH-003 | Notfall-Admin | Lokaler RCON-Admin, nie in Keycloak, immer lokal erreichbar; standardmäßig aktiv, per Env-Flag deaktivierbar | Backend | Muss | R1 | 1 |
| AUTH-004 | Keycloak-Auth | Lokal angelegte Benutzer optional gegen Keycloak (OIDC) authentifizieren, wie bei Nextcloud/Portainer; Architektur sieht das ab R1 vor | Backend | Soll | R2 | 1 |
| AUTH-005 | Inaktivitätssperre | Konten ohne Anmeldung über eine einstellbare Frist optional automatisch sperren | Backend | Soll | R2 | 1 |
| AUTH-006 | Sessions | Session-Management für alle Benutzer; Zuordnung Session zu Request-ID wird geloggt | Backend | Muss | R1 | 1 |
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
| SRV-001 | Server anlegen | Name, Spielprofil, Protokoll, Host, Port, Zugangsdaten, Agent und Container (Agent und Container setzbar ab M3) | Backend, UI | Muss | R1 | 1 |
| SRV-002 | Zugangsdaten verschlüsselt | RCON-Zugangsdaten mit AES-256-GCM verschlüsselt; der Master-Key wird beim ersten Start automatisch erzeugt und als Datei in /secrets abgelegt, getrennt von Datenbank und Backups (D-015, SEC-005), ohne Zutun des Betreibers; bei Verlust müssen die Zugangsdaten neu eingetragen werden | Backend, DB | Muss | R1 | 2 |
| SRV-003 | Servergruppen | RCON-Admin legt Servergruppen an und ordnet Server zu | Backend, UI | Muss | R1 | 1 |
| SRV-004 | Erreichbarkeit | Status je Server: verbunden, Auth-Fehler, offline | Backend | Muss | R1 | 1 |
| SRV-005 | Spielports | Festlegen, welche Ports Spielports sind, über Spielprofil oder durch RCON-Admin am Server | Backend, Profile | Soll | R1 | 1 |
| SRV-006 | Laufzeitdaten | Spielerliste und ähnliche Laufzeitdaten werden nicht dauerhaft gespeichert, sondern per Befehl bzw. über den Agent abgerufen und im Cache des Panels gepuffert | Backend | Muss | R1 | 1 |

### 2.5 Protokolle

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| PROT-001 | Source RCON | Adapter für Source RCON (TCP): Minecraft, ARK, Factorio u. a. | Backend | Muss | R1 | 1 |
| PROT-002 | Eine Verbindung je Server | Genau eine persistente Verbindung je Spieleserver mit Reconnect; Befehle in Warteschlange, Antworten per Request-ID zugeordnet | Backend | Muss | R1 | 1 |
| PROT-003 | HTTP-API | Adapter für HTTPS-APIs, z. B. Satisfactory Dedicated Server API | Backend | Soll | R2 | 1 |
| PROT-004 | Weitere Adapter | BattlEye RCon, WebRCON, Telnet nach Bedarf | Backend | Kann | R3 | 1 |

### 2.6 Spielprofile und Befehlskatalog

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| CAT-001 | Spielprofile | Profildatei je Spiel im Repository: Protokoll, Standardport, Spielports, Befehlskatalog, Muster für Version und Spieler, Parameter der Katalogbefehle; Format laut Design 7 | Profile | Muss | R1 | 2 |
| CAT-002 | Nur Orientierung | Katalog dient Autovervollständigung und Schnelleingabe, erlaubt oder verbietet nichts; Befehlslisten dürfen beliebige Befehle enthalten | Backend, UI | Muss | R1 | 1 |
| CAT-003 | Hot Reload | Profildateien sind die Quelle; beim Start und bei jeder Dateiänderung werden sie in die Datenbank importiert, ohne Neustart des Panels; die Oberfläche arbeitet ausschließlich aus der Datenbank | Backend | Muss | R1 | 1 |
| CAT-004 | Kategorien | Katalogbefehle kategorisiert (spielerbezogen, Welt, Server, kritisch); daraus Standard-Befehlsliste für Moderatoren | Profile, UI | Soll | R2 | 1 |
| CAT-005 | Befehlssyntax je Spiel | Spielprofil legt Groß-/Kleinschreibung, Präfixe (z. B. /), Wrapper-Befehle und maximale Befehlslänge fest; Standard: ohne Groß-/Kleinschreibung, führender / wird entfernt | Profile, Backend | Muss | R1 | 1 |

### 2.7 Befehlseingabe

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| CMD-001 | Admin-Konsole | Live-Konsole für Admins mit Favoriten und Serverwechsel ohne Seitenwechsel; Darstellung und Eingabe gemäß CMD-012 bis CMD-017 | UI | Muss | R1 | 1 |
| CMD-002 | Freie Eingabe | Freie Befehlseingabe nur für RCON- und Server-Admins | Backend | Muss | R1 | 1 |
| CMD-003 | Befehlsseite Moderator | Befehle der Befehlsliste als Buttons mit Parameterfeldern; Spielernamen aus aktueller Spielerliste wählbar; Verlauf mit Serverantworten | UI | Muss | R1 | 1 |
| CMD-004 | Token-Matching | Befehl ist ein Pfad aus ganzen Tokens; alles danach geht unverändert durch („whitelist add tralala“) | Backend | Muss | R1 | 1 |
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
| AGT-002 | Label-Freigabe | Nur Container mit Label (z. B. rcon.manage=true) sind sichtbar | Agent | Muss | R1 | 1 |
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
| AGT-013 | Versionsregel | Panel und Agent brauchen dieselbe Hauptversion; bei Abweichung lehnt das Panel die Verbindung mit Hinweis im Panel und im Agent-Log ab; neue Funktionen innerhalb einer Hauptversion meldet der Agent über eine Fähigkeitsliste | Agent, Backend | Muss | R1 | 1 |

### 2.9 Landingpage und Dashboard

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| UI-002 | Landingpage je Rolle | Landingpage je Rolle mit den für die Rolle relevanten Informationen | UI | Muss | R1 | 1 |
| UI-003 | Gesundheitsübersicht | Für RCON-Admins: Restart-Schleifen, Agent-Erreichbarkeit, RCON-Fehler, aktive und gesperrte Benutzer | UI, Backend | Muss | R1 | 1 |
| UI-004 | Server-Karten | Statusrand und -punkt, Name, Image (bei sha256 Spielprofil), CPU und RAM, letzter Restart, Spielversion, Spieler online; Felder ohne Quelle ausgeblendet | UI | Muss | R1 | 1 |
| UI-005 | Ports und Volumes | Server-Admin sieht Spielports, optional Steam-Connect-Link; RCON-Admin alle relevanten Ports; Volumes mit Name und Größe | UI | Soll | R2 | 1 |
| UI-006 | Karten Moderator | Nur eigene Server; Status per RCON-Erreichbarkeit, Version, Spieler; keine Agent-Daten | UI | Muss | R1 | 1 |
| UI-010 | Topologie | Live-Graph Host → Agent → Spieleserver, gruppiert nach Servergruppen | UI | Kann | R2 | 1 |

### 2.10 Oberfläche und Bedienung

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| UI-001 | Stil | Aufgeräumt wie Portainer, Diagramme wie Grafana, keine verschiebbaren Widgets, helles und dunkles Design | UI | Muss | R1 | 1 |
| UI-007 | Verwaltungslisten | Durchsuchbare, filterbare Listen, Mehrfachauswahl mit Massenaktionen, Details im Seitenpanel | UI | Muss | R1 | 1 |
| UI-008 | Schnelleingabe | Rechte und Zuweisungen eines bestehenden Benutzers als Vorlage übernehmen; Mehrfachauswahl mit Massenaktionen; neue Benutzer per Einladungslink mit Ablaufzeit statt Passwortvergabe; Inline-Bearbeitung in Listen; Rückgängig für einige Sekunden statt Bestätigungsdialog, Bestätigung nur bei destruktiven Aktionen; Tastaturkürzel für häufige Aktionen, standardmäßig deaktiviert, vom RCON-Admin global aktivierbar | UI | Soll | R1 | 1 |
| UI-009 | Logansicht | Live-Logs für berechtigte Rollen, übertragen nur bei aktiver Ansicht; optional letzte Logzeilen auf der Karte | UI, Agent | Soll | R1 | 1 |
| UI-011 | Navigation und Serverseite | Einklappbare Seitenleiste mit Einträgen je Rolle; Serverseite mit Kopf (Status, Aktionen) und Reitern Übersicht, Konsole oder Befehle, Logs, Verlauf; Einträge und Reiter ohne Recht werden ausgeblendet | UI | Muss | R1 | 1 |
| UI-012 | Mobile Nutzung | Karten, Befehlsseite, Konsole und Mitteilungen auf dem Smartphone voll bedienbar; Verwaltung mobil nur lesend | UI | Soll | R1 | 1 |

### 2.11 Audit und Statistik

| ID | Kurztitel | Beschreibung | Komponente | Prio | Target | Rev |
| --- | --- | --- | --- | --- | --- | --- |
| AUD-001 | Audit-Log | Wer, wann, Server, Befehl, Ergebnis, abgelehnt ja/nein; auch Agent-Aktionen und Verwaltungsänderungen | Backend, DB | Muss | R1 | 1 |
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
| Freigabe | Label am Container, z. B. rcon.manage=true |
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
| CPU, RAM, Netz | Agent | alle 10 s | 30 s | Agent getrennt, Container gestoppt |
| RCON-Erreichbarkeit | Panel | bei Verbindungsereignis | – | Verbindungsabbruch |
| Spielerliste | RCON-Befehl bzw. Agent | alle 60 s, nur solange jemand die Server ansieht; sofort bei Bedarf | 120 s | Server-Neustart, RCON-Abbruch |
| Spielversion | Agent (Logmuster) | einmal nach Containerstart | bis zum nächsten Start | Container-Neustart, Image-Wechsel |
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
| D-037 | Stilrichtung laut Mockups; dunkles Design als Standard, helles wählbar | Mockups abgenommen; Statusfarben und Monospace-Namen lesen sich auf dunklem Grund am besten | Standard nach Systemeinstellung |
| D-038 | Datenbankzugriff mit sqlc; SQLite über modernc.org/sqlite, Postgres über pgx | typsicher und SQL-nah; reines Go ohne CGO erlaubt statische Binaries | Bun, ent, mattn/go-sqlite3 (CGO) |
| D-039 | OpenAPI 3.1 als einzige Quelle für Server-Stubs (oapi-codegen) und Client-Typen (openapi-typescript) | API, Backend und Oberfläche laufen nicht auseinander; Doku der externen API ab R2 fällt mit ab | Typen von Hand pflegen |
| D-040 | Images für amd64 und arm64, signiert und mit SBOM auf ghcr.io | x86-Server und Raspberry Pi; nachprüfbare Lieferkette | zusätzlich armv7, nur amd64 |
| D-041 | Parametertypen player, text, choice, number im Spielprofil; eigene Befehlsdefinitionen (CMD-018) sollen sie übernehmen | CMD-003 braucht Parameterfelder schon in R1, die Argument-Syntax ist vertagt | nur ein freies Argumentfeld je Befehl |
| D-042 | Abnahme teilweise über Ereigniscodes in den Container-Logs, gelesen per Portainer-MCP | nutzt vorhandenen Zugang ohne neuen Code im Panel; Claude kann Kriterien direkt prüfen | eigener MCP-Endpunkt im Panel |
| D-043 | Session-Cookie immer HttpOnly und SameSite=Lax; Secure und Präfix \_\_Host- nur über HTTPS | Anmeldung auch ohne TLS-Proxy im LAN (D-011); Lax lässt Links von außen angemeldet ankommen, CSRF bleibt durch die Origin-Prüfung abgedeckt | Secure und SameSite=Strict immer |
| D-044 | Jede Änderung von Rolle, Scope, Gruppenmitgliedschaft, Grants, Supporter-Freigabe oder Sperre beendet die Sessions des Benutzers; Listeninhalte wirken ohne Abmeldung | keine offene Oberfläche mit veraltetem Stand; Listenpflege meldet niemanden ab | nur Deaktivierung beendet Sessions |
| D-045 | Profilbild als BLOB in der Datenbank, neu kodiert und verkleinert | ein Backup enthält alle veränderlichen Daten; Metadaten wie EXIF entfallen | Datei unter /data |
| D-046 | Benutzer entstehen nur per Einladung, dazu der Notfall-Admin | niemand vergibt Passwörter für andere; ein Weg statt zwei | POST /users mit Passwortvergabe |

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
| 2.2 | 02.11.2026 | Zur Freigabe vorgelegt: Erkenntnisse aus M2 eingearbeitet (Kapitel 11); Revision 2 für SEC-006 und SEC-008; Design 3.8 (RCON-Verbindung) neu; Einstellungen profiles.poll\_interval, ws.max\_sockets\_per\_session, ws.max\_subscriptions\_per\_socket; Grund-Code timeout |

## 10. Änderungen in Version 2.1

Eingearbeitet aus den Kandidaten nach M0 und M1 (bisher docs/revision-2.1-kandidaten.md). Nummern wie dort.

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

Eingearbeitet aus den Kandidaten nach M2 (docs/revision-2.2-kandidaten.md, Nr. 1 bis 10, Nummern wie dort); Nr. 11 ergibt sich aus dem Abschluss von M2.

| Nr. | Änderung | Fundstelle in 2.2 |
| --- | --- | --- |
| 1 | Beispielprofil Minecraft auf Englisch wie das ausgelieferte Profil | Design 7.4 |
| 2 | Hot Reload mit Abgleich in festem Intervall, Einstellung profiles.poll\_interval | Design 5.6, 7.1, Technologie 2 |
| 3 | Sichtbarkeit der Serverfelder je Rolle | Design 2.2 |
| 4 | Änderungen an Servergruppen beenden keine Sessions; WebSocket-Abos werden sofort neu geprüft | Design 2.1, 2.3 |
| 5 | Agent und Container am Server erst ab M3 setzbar | 2.4 SRV-001, Design 2.2, Plan R1 Kapitel 3 |
| 6 | Eigener Source-RCON-Adapter statt gorcon/rcon; Endmarker erst nach dem ersten Antwortpaket | Design 3.8, Technologie 2 |
| 7 | RCON-Verbindung: Prüfung im Leerlauf alle 2 s, Backoff höchstens 5 s; Design 1.1 gilt für den Agent | Design 1.1, 3.8 |
| 8 | Audit eines Befehls nur anhängend (cmd.sent, cmd.result); 504 mit Grund-Code timeout | Design 3.3, 3.5, 3.8, 4.1 |
| 9 | Revision 2 von SEC-006: Steuer- und Formatzeichen aus Bezeichnern, Steuerzeichen aus Freitext entfernt | 2.1 SEC-006, Design 3.3, Sicherheit 4, Abnahme SEC-006 |
| 10 | Revision 2 von SEC-008: Sockets je Session und Abos je Socket als Einstellungen, ältester Socket weicht; Abo-Bestätigung subscribed | 2.1 SEC-008, Design 2.3, 5.6, Sicherheit T-018 und 4, Abnahme SEC-008 |
| 11 | Teilabnahmen aus M2: SEC-006, SRV-003 und AUTH-006 mit E2E-Anteil werden in M4 abgenommen | Plan R1, Kapitel 3 |
