# Architektur

Dokumentversion 2.5, freigegeben; Änderungen gegenüber 2.4 siehe Gesamtkonzept, Kapitel 14, gegenüber 2.3 Kapitel 13, gegenüber 2.2 Kapitel 12, gegenüber 2.1 Kapitel 11, gegenüber 2.0 Kapitel 10. Die Technikentscheidungen D-001 und D-002 sind im Tab Technologie gegen diese Architektur geprüft: Go bestätigt, React statt SvelteKit.

Das System besteht aus einem zentralen Panel, das als einziger Baustein Zustand hält, schlanken Agents je Spiele-Host und den Spieleservern, die nur über ihr Protokoll angesprochen werden.

## 1. Überblick

Das Panel spricht die Spieleserver direkt per RCON an; Container steuert ausschließlich der Agent auf dem jeweiligen Host.

Gestrichelt: optionale Bausteine außerhalb des Projekts oder ab R2.

## 2. Bausteine

| Baustein | Aufgabe | Zustand | Anzahl |
| --- | --- | --- | --- |
| Browser | Oberfläche für alle Rollen | keiner | beliebig |
| Reverse Proxy (optional) | TLS und externer Zugriff, nicht Teil des Projekts | keiner | 0 oder 1 |
| Panel | Oberfläche ausliefern, API, Rechte, Befehle, RCON-Verbindungen, Cache, Audit | Datenbank, Key, Backups | genau 1 |
| Agent | Container-Steuerung und -Daten über den lokalen Docker-Socket | nur eigener Schlüssel | 1 je Spiele-Host |
| Spieleserver | Spiel mit RCON-Schnittstelle, als gelabelter Container | eigene Spieldaten, nicht unsere | beliebig |
| Datenhaltung | SQLite in /data, Key in /secrets, Sicherungen in /backups; Postgres ab R2 | – | 1 |
| Spielprofile | YAML-Dateien im Repository, Quelle des Befehlskatalogs | – | 1 je Spiel |
| Keycloak (optional, R2) | Prüfung von Benutzername und Passwort per OIDC | extern | 0 oder 1 |

## 3. Verbindungen

| Von | Nach | Zweck | Protokoll | Aufgebaut von | Absicherung | Target |
| --- | --- | --- | --- | --- | --- | --- |
| Browser | Panel | Oberfläche, API, Live-Updates | HTTP, WebSocket | Browser | Session, optional TLS am Proxy | R1 |
| Agent | Panel | Steuerung, Status, Logs in beide Richtungen über eine Verbindung | WebSocket über TLS 1.3 auf eigenem Agent-Port, ohne Proxy | Agent | gegenseitig gepinnte Ed25519-Schlüssel | R1 |
| Panel | Spieleserver | Befehle und Antworten | Source RCON (TCP) | Panel | RCON-Passwort, nur internes Netz | R1 |
| Panel | Spieleserver | Befehle über HTTPS-API (z. B. Satisfactory) | HTTPS | Panel | API-Token | R2 |
| Agent | Docker-Socket | Container steuern und auslesen | Docker-API, lokal | Agent | nur gelabelte Container, Filter im Agent | R1 |
| Panel | Datenbank | Persistenz | SQLite-Datei; Postgres ab R2 | Panel | lokal bzw. Zugangsdaten | R1 |
| Panel | Keycloak | Authentifizierung | OIDC | Panel | Client-Secret | R2 |

Der Agent braucht Schreibzugriff auf den Docker-Socket, weil Start und Stop schreibende Operationen sind. Die Einschränkung auf gelabelte Container und erlaubte Operationen leistet der Agent selbst.

## 4. Innerer Aufbau des Panels

Das Panel ist ein modularer Monolith: ein Prozess, darin klar getrennte Module mit festen Schnittstellen. Keines davon läuft als eigener Dienst.

| Modul | Aufgabe | Anforderungen |
| --- | --- | --- |
| Web-Schicht | HTTP, WebSocket, Auslieferung der Oberfläche, Proxy-Header | NFR-003 |
| Authentifizierung | Login, Sessions, Login-Schutz, Notfall-Admin; Zwei-Faktor und Keycloak ab R2 | AUTH-\*, SEC-004 |
| Rechteprüfung | Rolle, Scope und Befehlsliste je Anfrage; einzige Stelle, die über Erlaubnis entscheidet | ROLE-\*, CMD-004, CMD-005 |
| Befehlsdienst | Pipeline aus Prüfung, Bereinigung, Markierung, Warteschlange, Audit | CMD-\*, Haupttab 4.2 |
| Verbindungsmanager | Eine Verbindung je Spieleserver, Reconnect, Zuordnung per Request-ID; Protokolladapter als Schnittstelle | PROT-\* |
| Agent-Hub | Eingehende Agent-Verbindungen, Pairing, Widerruf, Weiterleitung von Aufträgen | AGT-\* |
| Cache | Laufzeitdaten im Speicher mit Altersangabe | NFR-005, Haupttab 4.6 |
| Ereignisbus | Interne Verteilung von Änderungen an die WebSocket-Verbindungen, gefiltert nach Rechten | CMD-014, UI-\* |
| Katalog-Import | Profildateien beobachten und in die Datenbank übernehmen | CAT-\* |
| Audit | Einträge schreiben, Aufbewahrungsfrist durchsetzen | AUD-\* |
| Persistenz | Datenbankzugriff, Migrationen, Verschlüsselung der Zugangsdaten | NFR-002, NFR-013, SRV-002 |
| Betrieb | Backups, Schlüsselverwaltung, Healthcheck | NFR-008, SEC-005 |
| Mitteilungen | Mitteilungen, Rückmeldungen, Votes, Umfragen | MSG-\* (R2) |

## 5. Datenflüsse

1. Befehl: wie im Haupttab unter 4.2 beschrieben, vom Browser über Rechteprüfung und Befehlsdienst zum Verbindungsmanager und zurück.
2. Live-Zustand: Agent meldet Ereignisse und Messwerte an den Agent-Hub, dieser schreibt in den Cache, der Ereignisbus verteilt an alle berechtigten Browser.
3. Katalog: Profildatei ändert sich, der Katalog-Import übernimmt sie in die Datenbank, die Oberfläche liest nur aus der Datenbank.
4. Start des Panels: Migrationen ausführen, Key prüfen oder erzeugen, Katalog importieren, RCON-Verbindungen aufbauen, Agents annehmen.

## 6. Auslieferung

| Variante | Compose-Dateien | Dienste | Betrieben von | Einsatz |
| --- | --- | --- | --- | --- |
| Getrennt (empfohlen) | panel und agent, je eine Datei | panel, postgres ab R2 optional / agent | RCON-Admin / Host-Betreiber je Spiele-Host | maximale Sicherheit, getrennte Verantwortung |
| All-in-One | eine Datei | panel, agent, postgres ab R2 optional | eine Person für alles | Einzelbetreiber auf einem Host |

| Dienst | Volumes |
| --- | --- |
| panel | /data, /secrets, /backups |
| agent | Docker-Socket des Hosts, /secrets (eigener und gepinnter Panel-Schlüssel) |
| postgres | eigenes Datenvolume |

Die getrennte Variante ist der empfohlene Weg: Der Host-Betreiber rollt den Agent selbst aus und koppelt ihn an das Panel eines RCON-Admins, ohne dessen Installation anzufassen. Die All-in-One-Variante bündelt alles in einer Datei; Panel und Agent bleiben auch dort getrennte Container, das Panel hängt nie am Docker-Socket (D-004). Auf einem Host verbinden sie sich über ein gemeinsames Docker-Netzwerk, sonst baut der Agent die Verbindung zur Panel-Adresse auf (D-016). Panel und Agent sind getrennte Images mit eigenem Release-Stand. Die Konfiguration erfolgt per Umgebungsvariablen, die genaue Liste folgt im Design.

## 7. Architekturprinzipien

- Das Panel ist die einzige Quelle der Wahrheit; Agents halten außer ihrem Schlüssel keinen Zustand.
- Rechte werden ausschließlich im Backend geprüft, nie nur in der Oberfläche.
- Erweiterungspunkte sind Schnittstellen: Protokolladapter, Agent-Backends, Auth-Anbieter, Datenbanktreiber, Sprachen (NFR-009).
- Ein-Instanz-Betrieb, keine Hochverfügbarkeit; Agents verbinden sich nach einem Neustart des Panels selbstständig wieder.
- Modular im Inneren, damit einzelne Module später bei Bedarf ausgelagert werden können.

## 8. Offene Architekturfragen

1. Nachrichtenformat der Agent-Verbindung; wird im Design festgelegt.
2. Kompatibilität zwischen Panel- und Agent-Versionen, da beide getrennt aktualisiert werden; wird im Design festgelegt.
