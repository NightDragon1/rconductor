# Design

Dokumentversion 2.7, freigegeben; Änderungen gegenüber 2.6 siehe Gesamtkonzept, Kapitel 16, gegenüber 2.5 Kapitel 15, gegenüber 2.4 Kapitel 14, gegenüber 2.3 Kapitel 13, gegenüber 2.2 Kapitel 12, gegenüber 2.1 Kapitel 11, gegenüber 2.0 Kapitel 10. Baut auf dem Tab Architektur auf; Die Zeit- und Ratenwerte in 1.1, 1.2 und 3.4 sind mit vergleichbaren Systemen abgeglichen und festgelegt; übrige Zahlenwerte werden bei der Umsetzung bestätigt.

## 1. Agent-Protokoll

### 1.1 Transport

| Punkt | Festlegung |
| --- | --- |
| Endpunkt | Eigener Agent-Port im Panel, getrennt vom Web-Port |
| TLS | TLS 1.3, vom Panel selbst terminiert; kein Reverse Proxy zwischen Agent und Panel. D-011 gilt nur für die Weboberfläche |
| Identität | Ed25519-Schlüsselpaar je Seite, selbstsignierte Zertifikate aus diesen Schlüsseln, gegenseitig gepinnt, keine CA (D-005) |
| Protokoll | WebSocket über diese TLS-Verbindung, eine Verbindung je Agent |
| Format | JSON, ein Nachrichtenumschlag für alle Typen (1.5) |
| Aufbau | Immer vom Agent (D-016) |
| Lebenszeichen | WebSocket-Ping alle 30 s; ohne Pong nach 60 s gilt die Verbindung als getrennt |
| Wiederverbindung | Exponentielles Backoff: Start 1 s, Faktor 1,6, Zufallsanteil ±20 %, höchstens 120 s. Gilt für den Agent; RCON-Verbindungen siehe 3.8 |

Grundlage: Socket.IO pingt standardmäßig alle 25 s und wartet 20 s auf Antwort ([Socket.IO](https://socket.io/zh-CN/docs/v4/server-options/)); das Chat-Beispiel von gorilla/websocket wartet 60 s auf ein Pong und pingt nach 54 s ([gorilla/websocket](https://chromium.googlesource.com/external/github.com/gorilla/websocket/+/v1.2.0/examples/chat/client.go)). Das Backoff übernimmt die Standardwerte von gRPC: 1 s, Faktor 1,6, Jitter 0,2, höchstens 120 s ([grpc-go](https://github.com/grpc/grpc-go/blob/e90fd41673eabe322f7e825312a5511c660fb241/backoff/backoff.go)). Die Token-Laufzeit von 24 h entspricht dem Standard für Bootstrap-Tokens in kubeadm ([Kubernetes](https://kubernetes.io/docs/reference/setup-tools/kubeadm/generated/kubeadm_token_create)).

### 1.2 Kopplung

Zwei Wege, beide enden im selben Zustand: Beide Seiten kennen und pinnen den öffentlichen Schlüssel der Gegenseite.

| Schritt | Weg A: Token | Weg B: Freigabe |
| --- | --- | --- |
| 1 | Admin erzeugt im Panel ein Einmal-Token; es enthält Panel-Adresse und Panel-Fingerprint und läuft nach 24 h ab. Format: rcd1. + base64url(Adresse, Fingerprint, 32 Byte Zufall); das Panel speichert nur den SHA-256 des Geheimnisses | Host-Betreiber setzt im Agent nur die Panel-Adresse |
| 2 | Host-Betreiber setzt das Token per Umgebungsvariable im Agent-Compose | Agent verbindet sich und schreibt den Fingerprint des Panels und seinen eigenen ins Log |
| 3 | Agent prüft den Panel-Fingerprint aus dem Token beim TLS-Handshake und sendet danach im hello das Geheimnis des Tokens; seinen öffentlichen Schlüssel zeigt er im Handshake | Panel führt den Agent als wartend mit seinem Fingerprint |
| 4 | Panel speichert den Agent-Schlüssel, das Token ist verbraucht | Admin vergleicht den Fingerprint mit dem Host-Betreiber und gibt frei oder lehnt ab |
| Absicherung | Panel-Identität ist durch das Token gesichert | Panel-Identität beim ersten Kontakt übernommen (Trust on First Use); Host-Betreiber sollte den Panel-Fingerprint vergleichen. Wartende Anfragen begrenzt und nach 24 h verworfen |

Der Agent speichert seinen Schlüssel und den gepinnten Panel-Schlüssel in seinem Volume /secrets.

- Weg A ist immer verfügbar. Weg B ist standardmäßig aus und wird vom RCON-Admin im Panel eingeschaltet (agents.fingerprint\_pairing, 5.6); er schaltet sich nach 30 min selbst ab. Ein- und Ausschalten, auch das automatische, stehen im Audit. Wartende Agents beantwortet das Panel mit reject pending; der Agent versucht es mit Backoff erneut. Der Agent pinnt den Panel-Schlüssel schon beim ersten Kontakt.
- Ein Token wird nach der Kopplung ignoriert, auch wenn es weiter in der Umgebung steht; RCD\_PAIRING\_TOKEN\_FILE liest es aus einer Datei (D-033).
- Die Agent-ID vergibt das Panel bei der Kopplung; der Agent speichert sie mit dem gepinnten Panel-Schlüssel in /secrets und sendet sie im hello.
- Koppeln und Widerrufen darf nur der RCON-Admin. Server-Admins nutzen gekoppelte Agents nur in ihrem Scope.
- In der All-in-One-Variante ist der Agent-Port nur im Docker-Netz erreichbar. Für entfernte Agents muss er bewusst veröffentlicht werden.

### 1.3 Verbindungsaufbau

1. TLS-Handshake. Der Agent prüft den gepinnten Panel-Schlüssel (bei Weg B ohne Pin beim ersten Kontakt). Das Panel lässt im Handshake jeden Ed25519-Schlüssel eines Agents zu, auch unbekannte, weil ein Agent vor der Kopplung (Weg A oder B) noch nicht bekannt ist; die Entscheidung über den Agent fällt im hello.
2. Agent sendet hello mit Agent-ID, Release-Version, Backend (docker) und Fähigkeitsliste.
3. Panel antwortet mit welcome samt Agent-ID oder reject samt Grund (version, revoked, unknown, pending, invalid). pin\_mismatch gibt es nur, wenn ein Agent eine bekannte Agent-ID mit einem fremden Schlüssel nennt: Das Panel lehnt mit reject unknown ab und loggt agent.rejected mit pin\_mismatch. Zeigt das Panel einen anderen als den gepinnten Schlüssel, bricht der Agent den TLS-Handshake ab; dann gibt es kein reject, beide Seiten loggen agent.rejected mit pin\_mismatch.
4. Agent sendet einen Snapshot aller freigegebenen Container.
5. Danach laufen Ereignisse, Messwerte und Aufträge über dieselbe Verbindung.

### 1.4 Versionsregel

Agent und Panel müssen dieselbe Hauptversion des Releases haben, also dieselbe erste Stelle (D-047). Bei Abweichung lehnt das Panel mit Grund version ab; der Hinweis erscheint im Panel-Log und im Agent-Log. Innerhalb einer Hauptversion meldet der Agent neue Funktionen über seine Fähigkeitsliste; das Panel blendet aus, was der Agent nicht kann.

### 1.5 Nachrichten

Umschlag: `{"type": "…", "id": "…", "ref": "…", "ts": "…", "payload": {}}`. id ist eindeutig je Nachricht, ref verweist bei Antworten auf die id des Auftrags.

| Typ | Richtung | Inhalt |
| --- | --- | --- |
| hello | Agent → Panel | Agent-ID, Version, Backend, Fähigkeiten |
| welcome / reject | Panel → Agent | Annahme oder Ablehnung mit Grund |
| snapshot | Agent → Panel | Alle freigegebenen Container mit Status, Ports, Volumes, started\_at und restart\_count; die beiden Felder sind additiv (neue Fähigkeit), ein Agent ohne sie bleibt gültig, die Karten blenden die Werte dann aus |
| container.event | Agent → Panel | Statusänderung eines Containers, mit started\_at und restart\_count wie bei snapshot |
| metrics | Agent → Panel | CPU, RAM, Netz, Disk-I/O aller Container, alle 10 s |
| container.action | Panel → Agent | start, stop oder restart eines Containers |
| logs.tail | Panel → Agent | Letzte n Logzeilen eines Containers |
| logs.follow / logs.stop | Panel → Agent | Laufende Logs nur bei offener Ansicht |
| log.lines | Agent → Panel | Logzeilen |
| version.detect | Panel → Agent | Regex für die Versionserkennung (D-009) |
| volumes.size | Panel → Agent | Belegte Größe der Volumes |
| result | Agent → Panel | Ergebnis eines Auftrags: ok oder Fehlercode |
| revoked | Panel → Agent | Kopplung widerrufen, Verbindung wird getrennt |

Fehlercodes: forbidden (Container nicht freigegeben oder Operation nicht erlaubt), not\_found, invalid, timeout, unsupported, rate\_limited (Rate je Container überschritten), error (Docker hat die Aktion abgelehnt). version.detect und volumes.size kommen mit R2; bis dahin antwortet der Agent mit unsupported.

### 1.6 Regeln im Agent

- Jeder Auftrag wird gegen die Label-Freigabe des Containers geprüft, auch wenn das Panel ihn bereits geprüft hat.
- Nur die Operationen aus 1.5 sind möglich; unbekannte Typen werden mit unsupported beantwortet.
- Regex nur RE2, mit Zeilenlimit und Timeout; zurück geht nur der Treffer.
- Aufträge je Container sind in der Rate begrenzt.
- Container werden über ihren Namen angesprochen; der Agent löst Namen bei jedem Auftrag neu auf und prüft dabei das Label. Ein Container ohne Label existiert für ihn nicht.

Grenzen (SEC-007):

| Grenze | Wert |
| --- | --- |
| Nachricht vom Agent | bis zum welcome höchstens 16 KiB, danach höchstens 1 MiB; darüber trennt das Panel die Verbindung (agent.limit mit reason message\_size) |
| hello | binnen 10 s nach dem TLS-Handshake, sonst trennt das Panel die Verbindung (agent.disconnected mit reason timeout) |
| Auftrag vom Panel | höchstens 64 KiB |
| log.lines | höchstens 500 Zeilen und 512 KiB Nutzdaten je Nachricht, sonst gekürzt mit truncated; je Zeile höchstens 8 KiB |
| Verbindungsaufbau je Quelladresse | 10 je Minute, 5 am Stück |
| Wartende Kopplungen (Weg B) | höchstens 10, verworfen nach 24 h |
| Aufträge je Container im Agent | 10 je Minute, 3 am Stück, darüber rate\_limited |
| Laufende Logs je Agent | höchstens 16 Container gleichzeitig |

### 1.7 Widerruf

Das Panel markiert den Agent als widerrufen, löst seine Server von ihm (agent\_id und container\_name werden leer), sendet revoked und trennt die Verbindung. Der Agent schreibt den Widerruf ins Log und verbindet sich nicht mehr automatisch; eine neue Kopplung braucht Weg A oder B und setzt den bestehenden Datensatz wieder aktiv.

## 2. API zwischen Browser und Panel

### 2.1 Grundsätze

| Punkt | Festlegung |
| --- | --- |
| Stil | REST mit JSON, Basis /api/v1, Version im Pfad |
| Live-Daten | Ein WebSocket je Browser-Tab mit Abos (2.3) |
| Session | Serverseitig; Session-ID im Cookie, immer HttpOnly und SameSite=Lax; über HTTPS (RCD\_PUBLIC\_URL mit https oder X-Forwarded-Proto: https von einem vertrauenswürdigen Proxy) zusätzlich Secure und mit Präfix \_\_Host-, sonst ohne Secure und mit Warnung beim Start; in der DB nur als Hash gespeichert. Inaktivität und Höchstdauer laut 5.6 (D-043) |
| Rechte | Bei jeder Anfrage neu geprüft, nie aus der Session übernommen. Sperre, Löschen und jede Änderung von Rolle, Scope, Gruppenmitgliedschaft, Grants oder Supporter-Freigabe eines Benutzers beenden seine Sessions sofort (AUTH-006, D-044). Änderungen am Inhalt einer Befehlsliste und an den Servern einer Servergruppe (auch ihr Löschen) wirken ab der nächsten Anfrage, ohne Abmeldung, weil sich keine Zuweisung eines Benutzers ändert; offene WebSocket-Abos werden dabei sofort neu geprüft (2.3) |
| CSRF | SameSite=Lax (D-043) plus Prüfung des Origin-Headers bei allen schreibenden Anfragen und beim WebSocket-Aufbau |
| Proxy | X-Forwarded-For und X-Forwarded-Proto werden nur von konfigurierten vertrauenswürdigen Proxys übernommen; Client ist die rechteste Adresse in X-Forwarded-For, die kein vertrauenswürdiger Proxy ist |
| Reservierte Pfade | /api (jede Version), /healthz und /metrics werden nie von der Oberfläche beantwortet; /healthz und /metrics werden mit NFR-006 (R2) befüllt. Ohne die Reservierung beantwortete die Auslieferung sie mit index.html, und ein Healthcheck meldete fälschlich „gesund“ |
| HAProxy | Normales HTTP/1.1, kein HTTP/2 nötig; für den WebSocket genügt ein ausreichendes timeout tunnel |
| Fehler | Einheitlich als Problem Details nach RFC 9457 |
| Größe | Request-Bodys sind auf 256 KiB begrenzt, bei den ohne Session offenen Operationen (Anmeldung, Einlösen einer Einladung) auf 16 KiB, ein Profilbild auf 2 MiB, darüber 413, auch bei unbekannter Länge. Schreibende Anfragen ohne gültige Session werden abgelehnt, bevor der Body gelesen wird. Beim Dekodieren kostet ein Body bis zum 57-fachen seiner Größe, die Rechte prüft die Operation erst danach; deshalb sind unabhängig von der Rolle höchstens 1 MiB JSON-Bodys und 8 MiB Profilbilder gleichzeitig in Arbeit, weitere warten bis zu 5 s und erhalten dann 503 busy. Die ohne Session offenen Operationen zählen nicht mit: Ihre Bodys sind klein, und ohne Anmeldung darf niemand das Budget der Angemeldeten aufbrauchen. Die größten echten Anfragen bleiben darunter: 500 Befehle mit 256 Zeichen sind 142 kB, Zuweisungen mit je 500 Einträgen 208 kB, Einstellungen 1 kB; Spielprofile sind Dateien und kommen nicht über die API; Passwörter über 512 Byte gelten bei der Anmeldung als falsch; der Login-Schutz merkt sich höchstens 256 Byte eines Pseudonyms |
| Gleichzeitige Änderungen | Benutzergruppen, Befehlslisten, Server und Servergruppen tragen ein Feld version, /settings eine gemeinsame Version als ETag. Schreiben nennt die gelesene Version in If-Match (Pflicht, sonst 428); die Prüfung ist atomar in der Datenbank in derselben Transaktion wie das Schreiben, bei Abweichung 409 und nichts wird geschrieben. Rolle und Scope werden vor der Version geprüft (3.1). Die Oberfläche zeigt „Changed by someone else – reload“ und sendet den Entwurf nicht |
| Listen | Cursorbasierte Paginierung für Audit und Verlauf |
| Externe API | R1 nur für die eigene Oberfläche. Ab R2 dokumentiert mit OpenAPI 3.1 und API-Tokens je Benutzer (Bearer), gleicher Rechteumfang wie der Benutzer, eigene Ablaufzeit, widerrufbar |

### 2.2 Ressourcen (R1)

| Pfad | Methoden | Zweck | Rollen |
| --- | --- | --- | --- |
| /auth/login, /auth/logout, /auth/session | POST, POST, GET | Anmeldung, Abmeldung, aktuelle Session; Session.ui enthält shortcuts\_enabled, supporters\_enabled und supporters\_allowed für Navigation und Router-Guards, über Rechte entscheidet weiter nur das Backend | alle |
| /me | GET, PATCH | Eigenes Profil | alle |
| /me/password | POST | Eigenes Passwort ändern; alle anderen Sessions enden; löscht must\_change\_password | alle |
| /users/{id}/password-reset | POST | Temporäres Passwort für einen verwalteten Benutzer setzen, nur in dieser Antwort enthalten; setzt must\_change\_password und beendet alle seine Sessions (AUTH-010) | wie /users |
| /me/avatar | PUT, DELETE | Eigenes Profilbild | alle |
| /me/favorites | PUT | Eigene Favoriten (Server), gelten auf allen Geräten | alle |
| /users, /users/{id} | GET, PATCH, DELETE | Benutzerverwaltung im eigenen Scope (3.6); Benutzer entstehen nur per Einladung (D-046) | RCON-Admin, Server-Admin, Moderator (nur Supporter) |
| /users/{id}/assignments | GET, PUT | Rollen, Gruppen und Server eines Benutzers; Vorlage für Schnelleingabe; PUT ersetzt nur Einträge, über die der Aufrufer verfügt (3.6) | wie /users |
| /users/{id}/avatar | GET | Profilbild (4.2) | der Benutzer, RCON-Admin, wer ihn verwaltet |
| /users/{id}/sessions | DELETE | Alle Sessions eines verwalteten Benutzers beenden, seine WebSockets schließen sofort; Audit user.sessions.end, Ereigniscode auth.session.revoked | wie /users |
| /invites, /invites/{id} | GET, POST, DELETE | Einladungslinks mit Ablaufzeit; ein Moderator lädt Supporter nur bei Supporter-Freigabe ein | wie /users |
| /invites/redeem | POST | Einladung einlösen mit eigenem Pseudonym und Passwort. Der Link trägt das Token nur im URL-Fragment (/invite#<token>); die Seite liest es einmal, entfernt es aus der Adresszeile und sendet es nur im Body | ohne Session |
| /user-groups, /user-groups/{id} | GET, POST, PATCH, DELETE | Benutzer- und Moderatorgruppen mit Mitgliedern und Grants an die ganze Gruppe | RCON-Admin, Server-Admin |
| /command-lists, /command-lists/{id} | GET, POST, PATCH, DELETE | Befehlslisten und weitergebbare Befehle | RCON-Admin, Server-Admin |
| /server-groups | GET, POST, PATCH, DELETE | Servergruppen | RCON-Admin; Server-Admin lesend im Scope |
| /servers, /servers/{id} | GET, POST, PATCH, DELETE | Spieleserver und RCON-Zugangsdaten; eine Änderung von Host oder Port verlangt das Passwort erneut, damit es nie unbestätigt an eine neue Adresse geht; GET liefert runtime (Containerzustand, Startzeit, Restarts, letzte Messwerte mit Zeit und veraltet, Spielerzahl) als Startwert für Karten und Kopf, Agent-Teile nur für Admins | RCON-Admin; übrige lesend im Scope |
| /servers/{id}/commands | GET, POST | GET: Pfade, die der Aufrufer senden darf, mit den Katalogparametern des Profils und player\_pattern; für Admins der Katalog des Profils (CMD-015). POST: Befehl senden; Antwort synchron mit Timeout, zusätzlich als Ereignis an alle Abonnenten | alle im Scope, gefiltert |
| /servers/{id}/history | GET | Befehlsverlauf | im Scope, laut Verlaufssichtbarkeit |
| /servers/{id}/logs | GET | Letzte Logzeilen über logs.tail, lines Standard 200, höchstens 500; Fremddaten (SEC-006), nur an den Aufrufer, nie in den Live-Ringpuffer. Ohne Agent 503 agent\_unavailable; das Ratenlimit des Agents kommt als 429 rate\_limited an | RCON-Admin; Server-Admin im Scope |
| /servers/{id}/actions | POST | start, stop, restart über den Agent; ohne Agent nur restart über den stop-Befehl des Profils (AGT-009); jede Aktion im Audit | RCON-Admin, Server-Admin |
| /profiles | GET | Spielprofile und Befehlskatalog | alle |
| /agents, /agents/{id} | GET, DELETE | Gekoppelte Agents, Widerruf | RCON-Admin |
| /agents/{id}/containers | GET | Container eines Agents aus dem Cache des Panels mit Name, Image und Zustand, nur mit dem Label des Agents (AGT-002), als Fremddaten, dazu connected; ist der Agent getrennt, die zuletzt bekannte Liste mit Zustand unknown | RCON-Admin |
| /agents/tokens | POST | Kopplungs-Token erzeugen, mit fertigem Compose-Ausschnitt | RCON-Admin |
| /agents/{id}/approve | POST | Wartende Kopplung über Fingerprint freigeben (Weg B) | RCON-Admin |
| /audit | GET | Audit-Log | RCON-Admin; Server-Admin im Scope |
| /settings | GET, PATCH | Globale Einstellungen | RCON-Admin |
| /backups | GET, POST | Sicherungen anzeigen und auslösen; folgt nach M5, nicht in 1.0 | RCON-Admin |
| /ws | GET (Upgrade) | WebSocket für Live-Daten | alle |

Felder eines Servers je Rolle: RCON-Admin und Server-Admin sehen Protokoll, Host, Port, Zustand der Zugangsdaten (ok, missing, reenter), Agent, Container und Spielports; Moderatoren und Supporter nur Name, Profil und die Servergruppen in ihrem Scope. Das RCON-Passwort gibt die API nie zurück. Agent und Container sind erst mit den Agents (M3) setzbar.

### 2.3 WebSocket

- Aufbau über /api/v1/ws mit dem Session-Cookie; Origin wird geprüft.
- Nachrichten vom Browser: subscribe und unsubscribe mit einem Thema, höchstens 4 KiB je Nachricht. Nachrichten vom Panel: subscribed mit Thema als Bestätigung eines Abos, event mit Thema und Daten, error mit Thema und Grund.
- Themen: servers.overview, server.{id}.status, server.{id}.metrics, server.{id}.console, server.{id}.logs; notifications ab R2. server.{id}.metrics und server.{id}.logs sind Agent-Funktionen: nur RCON-Admin und Server-Admin im Scope, sonst error forbidden. Ein neues Abo erhält zuerst den aktuellen Stand (letzter Messwert mit Zeit und Kennzeichen veraltet bzw. die letzten 50 Logzeilen); Logs werden nur übertragen, solange jemand sie abonniert (UI-009). Bei Agent-Trennung kommen die Messwerte sofort als veraltet mit Status unknown.
- servers.overview liefert für alle Server im Scope Status, Containerzustand, CPU und RAM mit Kennzeichen veraltet sowie die Spielerzahl; Agent-Daten nur für RCON-Admin und Server-Admin (UI-006). Solange die Übersicht abonniert ist, gelten ihre Server als angesehen (Spielerliste alle 60 s, Gesamtkonzept 4.6).
- Rechte werden beim Abonnieren geprüft und bei jeder Änderung von Rechten, Servergruppen, Scopes oder Grants sofort neu bewertet; entzogene Abos beendet das Panel mit error revoked, auch wenn die Session bleibt. Endet die Session, schließt das Panel ihre Sockets.
- Grenzen (SEC-008): höchstens ws.max\_sockets\_per\_session WebSockets je Session (Standard 20) und ws.max\_subscriptions\_per\_socket Abos je WebSocket (Standard 16), beide in 5.6. Ein weiteres Abo erhält error limit (ws.subscribe.denied). Öffnet eine Session einen Socket über der Grenze, schließt das Panel ihren ältesten Socket mit error replaced; der neue bleibt. Ein Browser, der 64 Nachrichten nicht abholt, wird getrennt.
- Lebenszeichen per Ping alle 30 s; ohne Pong binnen 30 s gilt die Verbindung als getrennt, bei jedem Ping wird die Session geprüft. Nach einer Wiederverbindung holt die Oberfläche den aktuellen Stand per REST und abonniert neu.

## 3. Rechteprüfung und Befehlspipeline

### 3.1 Grundsätze

- Standard ist Ablehnung; erlaubt ist nur, was eine Regel ausdrücklich erlaubt.
- Ein einziges Modul entscheidet (Rechteprüfung). REST, WebSocket-Abos und Agent-Aufträge nutzen dieselbe Prüfung.
- Rechte werden aus der Datenbank berechnet und im Speicher zwischengespeichert; jede Änderung an Benutzern, Gruppen, Zuweisungen oder Listen verwirft den Zwischenstand sofort.
- Server außerhalb des Scopes werden wie nicht vorhanden behandelt (404), damit ihre Existenz nicht sichtbar wird.
- Die Rollenprüfung kommt immer vor dem Objekt-Lookup: Darf eine Rolle die Ressourcenart nicht nutzen, antwortet das Panel mit 403 forbidden; darf sie sie nutzen und liegt das Objekt außerhalb ihres Scopes, mit 404 not\_found. Für eine abgelehnte Rolle sind die Antworten bei existierender und nicht existierender ID gleich.

### 3.2 Scope je Rolle

| Rolle | Server im Scope | Erlaubte Befehle je Server |
| --- | --- | --- |
| RCON-Admin | alle | alle |
| Server-Admin | Vereinigung aus zugewiesenen Servergruppen und Einzelservern | alle |
| Moderator | Server aus seinen Moderatorgruppen, begrenzt auf den Scope des zuständigen Server-Admins | Befehlsliste der Moderatorgruppe, über die er diesen Server hat; mehrere Wege zum selben Server werden vereinigt |
| Supporter | Ihm zugewiesene Server seines Moderators, begrenzt auf dessen Scope | Als weitergebbar markierte Befehle der Liste, die für seinen Moderator auf diesem Server gilt |

Verkleinert sich ein Scope oben, verkleinert er sich sofort für alle darunter. Agent-Funktionen stehen nur RCON-Admin und Server-Admin im jeweiligen Scope zur Verfügung.

### 3.3 Ablauf eines Befehls

1. Annahme über REST: Session gültig, Benutzer aktiv, Rolle bestimmt.
2. Scope: Server im Scope, sonst 404.
3. Bereinigung: Steuerzeichen und Zeilenumbrüche entfernen, Länge laut Spielprofil begrenzen, Präfix wie / laut Spielprofil entfernen.
4. Zerlegung in Tokens nach den Regeln des Spielprofils.
5. RCON-Admin und Server-Admin: weiter mit Schritt 8.
6. Wrapper: Ist der Befehlsname im Spielprofil als Wrapper markiert, wird er für Moderator und Supporter immer abgelehnt, unabhängig von der Liste.
7. Liste: Token-Pfad-Abgleich gegen die für diesen Server geltende Liste (3.2). Ein Eintrag erlaubt nur ganze Tokens und alles danach; Vergleich mit Groß-/Kleinschreibung laut Spielprofil, Standard ohne.
   7b. Katalogparameter (Moderator, Supporter): Befehle mit Katalogeintrag werden gegen die Parameter des Spielprofils geprüft (7.3); maßgeblich ist der längste Katalogpfad, mit dem der Befehl beginnt. Enthält der Befehl einen Befehlstrenner des Spielprofils (syntax.separators), wird er abgelehnt, denn der Abgleich sähe nicht, was danach folgt. Weitere Tokens nach den Parametern sind nur erlaubt, wenn der letzte Parameter vom Typ text ist, so scheitern time set day extra und whitelist add x y. Bei Abweichung 422 invalid mit dem Parameternamen im Detail oder „unexpected argument“, Audit-Eintrag cmd.denied. CMD-004 regelt den Pfad, nicht die Form der Argumente. Befehle ohne Katalogeintrag und die freie Eingabe von RCON-Admin und Server-Admin bleiben unverändert. Einschränkungen je Listeneintrag, die ein Server-Admin festlegt, folgen mit CMD-009 (R2); die Prüfung in R1 kennt nur die festen Parameter aus dem Spielprofil und gilt für alle Listen gleich.
8. Ratenlimit (3.4).
9. Markierung mit Request-ID, Session, Benutzer und Rolle.
10. Audit-Eintrag cmd.sent vor dem Senden (ID = Request-ID); eine Ablehnung ist ein einzelner Eintrag cmd.denied mit Grund.
11. Warteschlange je Server, Versand über die eine RCON-Verbindung, Zuordnung der Antwort per Request-ID.
12. Antwort im Response und als Ereignis an alle Abonnenten der Konsole; das Ergebnis (ok, error oder timeout, Dauer in Millisekunden) ist ein zweiter, angehängter Audit-Eintrag cmd.result mit Verweis auf die Request-ID. Das Audit-Log ist nur anhängend, kein Eintrag wird nachträglich geändert. Der Verlauf verbindet beide Einträge; fehlt das Ergebnis, zeigt er error mit not\_answered. Die Antwort ist Fremddaten (SEC-006): Steuerzeichen außer Zeilenumbruch und Tabulator werden entfernt, gespeichert und verteilt werden höchstens 64 KiB.

### 3.4 Ratenlimit

Token-Bucket je Benutzer und Server, vom RCON-Admin über die Einstellungen ratelimit.<rolle>.per\_minute und ratelimit.<rolle>.burst änderbar (5.6). Standardwerte:

| Rolle | Befehle je Minute | Kurzzeitig am Stück |
| --- | --- | --- |
| RCON-Admin | 120 | 20 |
| Server-Admin | 60 | 10 |
| Moderator | 20 | 5 |
| Supporter | 10 | 3 |

Bei Überschreitung: 429 mit Wartezeit, Audit-Eintrag.

Grundlage: Für Befehle an Spieleserver gibt es keinen Branchenstandard. Pterodactyl begrenzt seine gesamte Client-API auf 256 Anfragen pro Minute und Benutzer und den Aufbau von Konsolenverbindungen auf 5 pro Minute ([Pterodactyl](https://deepwiki.com/pterodactyl/panel/7.4-rate-limiting-and-throttling)). Unsere Werte gelten nur für Befehle je Server und liegen deutlich darunter, damit eine Befehlsflut einen Spieleserver nicht blockiert, normale Moderation aber nie bremst.

### 3.5 Antworten bei Ablehnung

| Status | Grund-Code | Bedeutung |
| --- | --- | --- |
| 401 | unauthenticated | Keine gültige Session |
| 404 | not\_found | Server nicht vorhanden oder nicht im Scope |
| 403 | not\_in\_list | Befehl nicht in der geltenden Liste |
| 403 | wrapper\_blocked | Wrapper-Befehl für diese Rolle gesperrt |
| 422 | invalid | Leerer oder zu langer Befehl nach der Bereinigung, oder Katalogparameter verletzt (3.3, Schritt 7b) |
| 429 | rate\_limited | Ratenlimit erreicht |
| 503 | server\_unavailable | Keine RCON-Verbindung zum Spieleserver |
| 504 | timeout | Der Spieleserver hat nicht binnen 10 s geantwortet, oder der Agent nicht rechtzeitig |
| 503 | agent\_unavailable | Server ohne verbundenen Agent; start und stop ohne Agent |
| 502 | action\_failed | Agent oder Docker hat die Aktion abgelehnt, z. B. Container ohne Label |
| 403 | forbidden | Rolle darf die Ressourcenart nicht nutzen (3.1) oder Verwaltungsaktion nicht erlaubt, z. B. Rolle auf eigene Stufe heben (3.6) |
| 409 | conflict | Objekt wurde seit dem Lesen geändert (2.1) |
| 428 | precondition\_required | If-Match fehlt beim Schreiben eines versionierten Objekts (2.1) |
| 403 | password\_change\_required | Passwortwechsel erzwungen; erlaubt sind nur /me/password, /auth/logout und /auth/session (AUTH-010) |
| 503 | busy | Panel ausgelastet, z. B. alle Plätze der Passwortberechnung belegt oder die Warteschlange eines Spieleservers voll (3.8); mit Retry-After |

### 3.6 Verwaltungsrechte

- Ein Benutzer verwaltet nur Benutzer mit niedrigerer Rolle in seinem Scope.
- Zuweisungen an andere sind immer eine Teilmenge des eigenen Scopes und der eigenen Befehle.
- Verwaltungsrechte selbst sind nie weitergebbar.
- Beim Markieren weitergebbarer Befehle bleibt mindestens ein Befehl der Liste gesperrt; das Panel lehnt das Speichern sonst ab.
- Wer wen verwaltet: der RCON-Admin alle niedrigeren Rollen; ein Server-Admin die Moderatoren, die er eingeladen hat oder die Mitglied einer seiner Moderatorgruppen sind, deren Supporter und die von ihm eingeladenen Supporter; ein Moderator nur seine Supporter (eingeladen oder zugewiesen), solange die Supporter-Freigabe global und für ihn gilt; ein Supporter niemanden.
- Zuweisungen: Ein Ersetzen ändert nur die Einträge, über die der Aufrufer verfügt (eigene Gruppen, eigene Moderatoren); Einträge anderer bleiben. Eine Servergruppe darf nur zugewiesen werden, wenn alle ihre Server im Scope liegen.
- Ein Rollenwechsel entfernt Scopes, Gruppenmitgliedschaften und Grants der alten Rolle; stehen gelassene Reste würden nach einem Rückwechsel unbemerkt wieder wirken.
- Wiederherstellung des Notfall-Admins (AUTH-011): Mit RCD\_EMERGENCY\_ADMIN\_RESET=true erzeugt das Panel beim Start ein neues Initialpasswort, ausgegeben wie in AUTH-007, setzt must\_change\_password, beendet alle Sessions des Notfall-Admins, schreibt einen Audit-Eintrag ohne Passwort und auth.emergency\_admin.reset. Gesperrt (disabled) bleibt er nicht; die Variable wirkt bei jedem Start, solange sie gesetzt ist, deshalb warnt das Panel dabei.
- Ein Passwort-Reset setzt ein zufälliges temporäres Passwort, setzt must\_change\_password und beendet alle Sessions des Benutzers (wie D-044); nur wer den Benutzer verwaltet, darf ihn auslösen (AUTH-010). Das temporäre Passwort steht genau einmal in der API-Antwort an den Verwaltenden, nie in Log oder Audit; der Audit-Eintrag nennt nur Verwaltenden, Benutzer und Ergebnis. Der Notfall-Admin wird nicht zurückgesetzt.
- Echtname, Profilbild und Kontaktangaben sehen der Benutzer selbst, der RCON-Admin und wer ihn verwaltet; alle anderen nur das Pseudonym (AUTH-009).

### 3.7 Login-Schutz

| Punkt | Festlegung |
| --- | --- |
| Verzögerung | Je Konto ab dem dritten Fehlversuch in Folge 1 s, dann 2 s, dann 4 s vor der Passwortprüfung |
| Sperre | Je Konto und Client-Adresse nach login.max\_failures (Standard 5) Fehlversuchen für login.lockout\_duration (Standard 15 min); auch das richtige Passwort wird dann mit 429 abgelehnt |
| Sperre je Client | Je Client-Adresse über alle Namen nach login.max\_client\_failures (Standard 50) Fehlversuchen je Sperrfenster für login.lockout\_duration; das Fenster beginnt mit dem ersten Fehlversuch. Client-Adresse ist die Adresse, IPv6 je /64, hinter einem vertrauenswürdigen Proxy die echte Adresse des Besuchers (2.1). Auch hier wird das richtige Passwort mit 429 abgelehnt. Eine erfolgreiche Anmeldung ändert diesen Zähler nicht, sonst könnte ein eigenes Konto ihn zurücksetzen |
| Unbekannte Benutzer | Zählen wie bekannte; gleiche Antwort, gleiche Fehlermeldung, gleiche Argon2id-Arbeit über einen Ersatz-Hash |
| Zähler | Im Arbeitsspeicher, höchstens login.max\_tracked Einträge (Standard 100 000, 1000 bis 1 000 000); enden nach der Sperrdauer ohne Fehlversuch und beim Neustart. Eine erfolgreiche Anmeldung löscht nur die Zähler des eigenen Clients: die Sperr-Zähler dieses Clients und seinen Anteil am Konto-Zähler. Fehlversuche anderer Clients bleiben, auch ihre Verzögerung; Anmeldungen des Besitzers verschaffen einem Angreifer so keine weiteren Versuche. Jeder Versuch reserviert vor der Passwortprüfung die Plätze für seine Zähler. Ist die Tabelle voll, werden nur beendete Zähler verdrängt, also nicht gesperrte ohne Fehlversuch seit der Sperrdauer; laufende Zähler und Sperren bleiben. Wer schon vollständig gezählt wird, ist vom Folgenden nicht betroffen |
| Volle Tabelle | Ein Versuch ohne Platz kommt in eine gemeinsame Warteschlange: höchstens 30 Clients, je Client ein Platz, alle 250 ms einer. Wer die wenigsten Fehlversuche hat, kommt zuerst dran und verdrängt in einer vollen Schlange den mit den meisten; so bleibt die Anmeldung möglich, und wer weiter rät, fällt zurück. Gezählt wird in einem Zähler fester Größe (1 MiB, gehashte Schlüssel mit Zufallswert je Start, 65 536 Felder), der die drei Zähler nachbildet: Je Konto und Client und je Client über alle Namen höchstens login.max\_failures Fehlversuche je Sperrfenster, je Konto die Verzögerung wie oben. Teilen sich zwei Schlüssel ein Feld, zählt das nur strenger. Auch ein Fehlversuch, dessen Zähler während der Prüfung endete und dessen Platz vergeben wurde, landet dort. Eine Anmeldung löscht in diesem Zähler nichts. Abgewiesene erhalten 429; höchstens einmal je Minute entsteht ein auth.lockout (scope table) mit Audit-Eintrag |
| Passwortberechnung | Argon2id mit t=3, 64 MiB, p=4; höchstens RCD\_PASSWORD\_HASH\_CONCURRENCY Berechnungen gleichzeitig, sonst nach 5 s Wartezeit 503 busy |
| Passwörter | 12 bis 128 Zeichen |

Laufende Versuche zählen schon vor ihrem Ergebnis, so kommt auch ein paralleler Schub nicht über die Grenze. IPv6-Adressen zählen je /64. Ein gesperrter Client erzeugt je Sperre einen Audit-Eintrag, nicht einen je Versuch; ein über alle Namen gesperrter Client einen für alle Namen. Weil laufende Versuche zählen, kann ein Client bei vielen gleichzeitigen richtigen Anmeldungen desselben Kontos 429 erhalten (Sicherheit, Restrisiken).

### 3.8 RCON-Verbindung

| Punkt | Festlegung |
| --- | --- |
| Verbindung | Genau eine je Spieleserver (PROT-002); Befehle aller Benutzer laufen nacheinander über eine Warteschlange, Antworten gehen per Request-ID an ihren Absender. Höchstens 64 Befehle warten je Server; ist die Warteschlange voll, antwortet die API sofort mit 503 busy (3.5) |
| Prüfung im Leerlauf | TCP-Keepalive auf jeder RCON-Verbindung (Leerlauf 10 s, Intervall 3 s, 3 Proben). Zusätzlich alle 2 s ein kurzer Lesevorgang ohne Senden, der ein vom Spieleserver geschlossenes Ende sofort erkennt, und im Leerlauf alle 30 s ein leeres Paket als Probe wie der Endmarker; ohne Antwort binnen 5 s gilt der Server als offline und die Verbindung wird neu aufgebaut. Halboffene Verbindungen nach Host-Absturz oder Netzausfall liefern kein FIN oder RST; ein Lesevorgang allein erkennt sie nicht (SRV-004) |
| Wiederverbindung | Nach einem Abbruch sofort, danach Backoff wie 1.1 (Start 1 s, Faktor 1,6, Zufallsanteil ±20 % je Wartezeit), aber höchstens 5 s, damit ein Wechsel binnen 10 s sichtbar ist. Der Zufallsanteil verhindert, dass nach einem Netzausfall alle Server im Gleichschritt neu verbinden |
| Zeitüberschreitung | 10 s je Befehl ab seiner Annahme, einschließlich der Wartezeit in der Warteschlange; danach Antwort 504 timeout (3.5). Ein Befehl, dessen Frist in der Warteschlange abläuft, wird nicht mehr gesendet. Die Verbindung bleibt, eine späte Antwort wird an ihrer Paket-ID erkannt und verworfen |
| Source RCON | Eigener Adapter (Technologie 2): Lange Antworten kommen in mehreren Paketen ohne Endmarke. Nach dem ersten Antwortpaket eines Befehls sendet der Adapter ein leeres Paket als Endmarker und sammelt bis zu dessen Antwort; Pakete mit fremder ID werden verworfen. Befehl und Marker gehen nie zusammen hinaus, weil Minecraft nur ein Paket je Lesevorgang verarbeitet und die Verbindung sonst trennt. Grenzen: 64 KiB je Paket, 1 MiB je Antwort (SEC-006) |

## 4. Datenmodell

### 4.1 Grundsätze

| Punkt | Festlegung |
| --- | --- |
| IDs | UUIDv7 für alle Datensätze; die Request-ID eines Befehls ist zugleich die ID seines ersten Audit-Eintrags; Folgeeinträge desselben Befehls verweisen auf sie (3.3) |
| Zeit | Alle Zeitstempel in UTC |
| Löschen | Soft-Delete über deleted\_at für users, servers, server\_groups, user\_groups, command\_lists; endgültiges Entfernen erst nach Ablauf der Audit-Frist. Eindeutige Namen gelten nur für nicht gelöschte Datensätze |
| Geheimnisse | AES-256-GCM; jeder Wert speichert Key-ID, Nonce und Chiffrat. Der Schlüssel je Key-ID wird per HKDF aus dem Master-Key abgeleitet. Tabelle, Datensatz-ID und Spalte gehen als zusätzliche Authentisierungsdaten ein, damit verschlüsselte Werte nicht zwischen Datensätzen vertauscht werden können |
| Passwörter | Argon2id-Hash, nie verschlüsselt gespeichert |
| Tokens | Session-IDs, Einladungs- und Kopplungs-Tokens nur als SHA-256-Hash |
| Audit | Eigene Tabelle in derselben Datenbank. Der Befehlsverlauf der Konsole ist eine Sicht auf das Audit-Log, keine eigene Tabelle |
| Schema | Beide Datenbanken nutzen dasselbe Schema über eingebettete Migrationen (D-018) |
| Versionen | Jede Änderung eines versionierten Objekts (2.1) erhöht seine Version, auch über andere Wege wie Mitglieder, Einträge und Servergruppen-Mitglieder; dafür sorgen Trigger in der Datenbank. Für Postgres (R2) gelten dieselben Trigger in der Postgres-Migration |

### 4.2 Tabellen

| Tabelle | Wichtige Felder | Release |
| --- | --- | --- |
| users | pseudonym (eindeutig), real\_name, avatar (BLOB) mit avatar\_type, contacts, must\_change\_password, role, auth\_source, password\_hash, totp\_secret (verschlüsselt, R2), is\_emergency, supporters\_allowed, created\_by, last\_login\_at, disabled\_at, deleted\_at | R1 |
| sessions | id\_hash, user\_id, created\_at, last\_seen\_at, expires\_at, ip, user\_agent | R1 |
| invites | token\_hash, created\_by, role, template\_user\_id, expires\_at, used\_at | R1 |
| user\_groups | name, kind (allgemein oder Moderatorgruppe), owner\_id, command\_list\_id (nur Moderatorgruppe), version, deleted\_at | R1 |
| user\_favorites | user\_id, server\_id | R1 |
| user\_group\_members | user\_group\_id, user\_id | R1 |
| servers | name, profile\_id, protocol, host, port, secret (verschlüsselt), agent\_id (Fremdschlüssel auf agents, ON DELETE SET NULL), container\_name, version, deleted\_at. Gespeichert wird der Containername, weil Docker beim Neuanlegen eine neue ID vergibt; die Label-Prüfung bei jeder Auflösung verhindert, dass ein gleichnamiger Container ohne Freigabe übernommen wird; agent\_id und container\_name sind nur gemeinsam gesetzt | R1 |
| server\_groups | name, version, deleted\_at | R1 |
| server\_group\_members | server\_group\_id, server\_id | R1 |
| admin\_scopes | user\_id (Server-Admin), genau eines von server\_group\_id oder server\_id, assigned\_by | R1 |
| grants | user\_group\_id (Moderatorgruppe, Pflicht), user\_id optional (leer: alle Mitglieder, gesetzt: nur dieses Mitglied), genau eines von server\_id oder server\_group\_id, granted\_by | R1 |
| command\_lists | name, owner\_id, version, deleted\_at | R1 |
| command\_list\_entries | command\_list\_id, path (Token-Pfad), passable | R1 |
| supporter\_grants | supporter\_id, moderator\_id, server\_id, granted\_by | R1 |
| agents | name, public\_key, fingerprint, status (wartend, aktiv, widerrufen), version, capabilities, last\_seen\_at, paired\_by, revoked\_at | R1 |
| agent\_tokens | token\_hash, created\_by, expires\_at, used\_at, agent\_id | R1 |
| game\_profiles | slug, name, source\_file, content\_hash, rules (Syntax, Ports, Muster), imported\_at | R1 |
| catalog\_commands | profile\_id, path, description, category (R2) | R1 |
| audit\_log | ts, kind (command, agent, admin, auth), actor\_id, actor\_role, session\_id, server\_id, target\_type, target\_id, action, payload, result (ok, denied, error, timeout), reason\_code | R1 |
| settings | key, value, scope\_type (global, server, server\_group), scope\_id | R1 |
| settings\_version | Gemeinsame Version der globalen Einstellungen (2.1) | R1 |
| command\_templates | server\_id, name, template | R2 |
| api\_tokens | user\_id, token\_hash, name, expires\_at, revoked\_at | R2 |
| messages, message\_recipients, message\_comments, message\_votes, polls, poll\_options, poll\_votes | Mitteilungen mit Rückmeldung, Votes und Umfragen | R2 |

### 4.3 Rechte im Datenmodell

- Scope eines Server-Admins: alle Server aus admin\_scopes, Servergruppen aufgelöst über server\_group\_members.
- Scope und Liste eines Moderators: grants seiner Moderatorgruppen, an die ganze Gruppe oder an ihn als Mitglied. Die Liste kommt aus user\_groups.command\_list\_id der Gruppe, über die der Grant läuft; der Eigentümer der Gruppe ist der zuständige Server-Admin. Ein direkter Grant nennt deshalb immer die Moderatorgruppe, und der Moderator muss ihr Mitglied sein.
- Profilbild: PNG oder JPEG (WebP nicht, die Standardbibliothek dekodiert es nicht), höchstens 1 MiB und 4096 × 4096 Pixel, am Inhalt geprüft, neu kodiert ohne Metadaten, verkleinert auf höchstens 512 × 512, ausgeliefert mit nosniff und einer Content-Security-Policy, die nichts erlaubt (D-045).
- Supporter: supporter\_grants je Server; erlaubt sind die Einträge mit passable der Liste, die für seinen Moderator auf diesem Server gilt.
- Supporter-Freigabe: global in settings, je Moderator in users.supporters\_allowed.

### 4.4 Aufräumen

Ein täglicher Job entfernt Audit-Einträge nach Ablauf der Frist (Standard 90 Tage), abgelaufene Sessions, Einladungen und Kopplungs-Tokens sowie soft-gelöschte Datensätze, auf die kein Audit-Eintrag mehr verweist.

## 5. Konfiguration

### 5.1 Grundsätze

- Umgebungsvariablen nur für Start und Infrastruktur, alle mit Präfix RCD\_. Alles, was sich im Betrieb ändert (Fristen, Ratenlimits, Supporter-Freigabe, Tastaturkürzel), wird im Panel eingestellt und liegt in settings.
- Jede Variable mit Geheimnis gibt es zusätzlich mit Endung \_FILE; dann wird der Wert aus der genannten Datei gelesen (Docker-Secrets).
- Ohne Angaben startet das Panel lauffähig: SQLite, Ports 8080 und 8443, Verzeichnisse /data, /secrets, /backups.

### 5.2 Panel

| Variable | Standard | Zweck |
| --- | --- | --- |
| RCD\_HTTP\_ADDR | :8080 | Adresse der Weboberfläche |
| RCD\_AGENT\_ADDR | :8443 | Adresse des Agent-Ports (TLS) |
| RCD\_AGENT\_PUBLIC\_ADDR | leer | Adresse, unter der Agents das Panel erreichen; wird in Kopplungs-Tokens geschrieben. Leer: Hinweis im Kopplungsdialog |
| RCD\_PUBLIC\_URL | leer | Öffentliche Adresse der Oberfläche für Einladungslinks und Origin-Prüfung. Leer: aus der Anfrage abgeleitet |
| RCD\_BASE\_PATH | / | Basispfad hinter dem Reverse Proxy |
| RCD\_TRUSTED\_PROXIES | leer | IP-Bereiche, deren X-Forwarded-Header übernommen werden |
| RCD\_DB\_DRIVER | sqlite | sqlite oder postgres (ab R2) |
| RCD\_DB\_URL / \_FILE | leer | Verbindung zu externem Postgres; leer bei postgres: interner Compose-Dienst |
| RCD\_DATA\_DIR | /data | SQLite, Profilbilder, eigene Spielprofile |
| RCD\_SECRETS\_DIR | /secrets | Master-Key, Panel-Schlüssel |
| RCD\_BACKUP\_DIR | /backups | Sicherungen |
| RCD\_MASTER\_KEY\_FILE | leer | Master-Key aus einer anderen Datei, z. B. Docker-Secret; fehlt sie, startet das Panel nicht. Leer: /secrets, beim ersten Start erzeugt |
| RCD\_EMERGENCY\_ADMIN | true | Notfall-Admin aktiv |
| RCD\_EMERGENCY\_ADMIN\_RESET | false | true: beim Start neues Initialpasswort für den Notfall-Admin (AUTH-011); danach wieder entfernen |
| RCD\_PASSWORD\_HASH\_CONCURRENCY | 2 | Gleichzeitige Passwortberechnungen (1 bis 64), je 64 MiB |
| RCD\_MAX\_CONNECTIONS | 512 | Offene Verbindungen am Web-Port (16 bis 65536), WebSockets eingeschlossen; weitere warten, bis eine endet (Sicherheit T-025) |
| RCD\_MAX\_CLIENT\_CONNECTIONS | 128 | Offene Verbindungen einer Client-Adresse am Web-Port (1 bis 65536), ein IPv6-/64 zählt als eine; weitere werden sofort geschlossen; Adressen aus RCD\_TRUSTED\_PROXIES ausgenommen (Sicherheit T-025) |
| RCD\_MAX\_ANONYMOUS\_CONNECTION\_SECONDS | 30 | Eine Verbindung ohne gültige Session endet nach dieser Zeit (10 bis 3600), nach ihrer laufenden Anfrage; WebSockets und vertrauenswürdige Proxys sind ausgenommen (Sicherheit T-025) |
| RCD\_LOG\_LEVEL | info | debug, info, warn, error |
| RCD\_LOG\_FORMAT | text | text oder json |

### 5.3 Agent

| Variable | Standard | Zweck |
| --- | --- | --- |
| RCD\_PAIRING\_TOKEN / \_FILE | leer | Kopplungs-Token (Weg A); enthält Panel-Adresse und Fingerprint. Nach erfolgreicher Kopplung ignoriert |
| RCD\_PANEL\_ADDR | leer | Panel-Adresse für Weg B oder um die Adresse aus dem Token zu überschreiben |
| RCD\_AGENT\_NAME | Hostname | Anzeigename im Panel |
| RCD\_LABEL | rcon.manage=true | Label, das einen Container freigibt |
| RCD\_DOCKER\_HOST | unix:///var/run/docker.sock | Zugang zum Docker-Daemon |
| RCD\_SECRETS\_DIR | /secrets | Agent-Schlüssel und gepinnter Panel-Schlüssel |
| RCD\_LOG\_LEVEL, RCD\_LOG\_FORMAT | info, text | wie beim Panel |

### 5.4 Compose: getrennte Variante

Panel-Host:

```yaml
services:
  panel:
    image: ghcr.io/<owner>/rconductor-panel:1
    restart: unless-stopped
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    pids_limit: 256
    mem_limit: 1g
    ports:
      - "8080:8080"
      - "8443:8443"
    environment:
      RCD_AGENT_PUBLIC_ADDR: panel.example.net:8443
    volumes:
      - data:/data
      - secrets:/secrets
      - backups:/backups
volumes:
  data:
  secrets:
  backups:
```

Spiele-Host:

```yaml
services:
  agent:
    image: ghcr.io/<owner>/rconductor-agent:1
    restart: unless-stopped
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    pids_limit: 256
    mem_limit: 1g
    group_add: ["<GID des Docker-Sockets>"]
    environment:
      RCD_PAIRING_TOKEN: "<Token aus dem Panel>"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - agent-secrets:/secrets
volumes:
  agent-secrets:
```

Das Image-Tag nennt nur die Hauptversion, damit Panel und Agent automatisch zusammenpassen (D-023).

### 5.5 Compose: All-in-One

```yaml
services:
  panel:
    image: ghcr.io/<owner>/rconductor-panel:1
    restart: unless-stopped
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    pids_limit: 256
    mem_limit: 1g
    ports:
      - "8080:8080"
    environment:
      RCD_AGENT_PUBLIC_ADDR: panel:8443
    volumes:
      - data:/data
      - secrets:/secrets
      - backups:/backups
  agent:
    image: ghcr.io/<owner>/rconductor-agent:1
    restart: unless-stopped
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    pids_limit: 256
    mem_limit: 1g
    group_add: ["<GID des Docker-Sockets>"]
    environment:
      RCD_PAIRING_TOKEN: "<Token aus dem Panel>"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - agent-secrets:/secrets
volumes:
  data:
  secrets:
  backups:
  agent-secrets:
```

Der Agent-Port 8443 wird nicht veröffentlicht; der Agent erreicht das Panel über das Compose-Netz. Die Kopplung läuft wie in der getrennten Variante per Token: Ohne Token wartet der Agent und schreibt einen Hinweis ins Log. Der Betreiber erzeugt das Token im Panel, trägt es ein und startet den Agent neu.

### 5.6 Laufzeit-Einstellungen

Globale Einstellungen in settings; nur der RCON-Admin liest und ändert sie über /settings, nur bekannte Schlüssel mit Typ- und Bereichsprüfung, alles oder nichts.

| Schlüssel | Standard | Zweck |
| --- | --- | --- |
| backup.interval, backup.keep\_auto, backup.keep\_pre\_migration | 24 h, 7, 3 | Backups (NFR-008, NFR-013) |
| session.idle\_timeout, session.max\_lifetime | 12 h, 7 Tage | Sessions (AUTH-006) |
| login.max\_failures, login.max\_client\_failures, login.lockout\_duration | 5, 50, 15 min | Login-Schutz (SEC-004, 3.7) |
| invite.lifetime | 72 h | Gültigkeit von Einladungen (UI-008) |
| supporters.enabled | aus | Globale Supporter-Freigabe (ROLE-006); Abschalten beendet die Sessions aller Supporter |
| ui.shortcuts\_enabled | aus | Tastaturkürzel (6.1); für alle Benutzer lesbar über /auth/session (Feld ui) |
| audit.retention\_days | 90 (30 bis 3650) | Aufbewahrung des Audit-Logs (AUD-003) |
| ratelimit.<rolle>.per\_minute, ratelimit.<rolle>.burst | laut 3.4 (z. B. ratelimit.moderator: 20 und 5) | Ratenlimit je Rolle (CMD-019); <rolle> ist rcon\_admin, server\_admin, moderator oder supporter |
| profiles.poll\_interval | 30 s (1 s bis 1 h) | Abgleich der Profildateien zusätzlich zu Dateiereignissen (CAT-003, 7.1) |
| ws.max\_sockets\_per\_session | 20 (1 bis 100) | WebSockets je Session (SEC-008); darüber wird der älteste Socket mit error replaced geschlossen |
| ws.max\_subscriptions\_per\_socket | 16 (1 bis 64) | Abos je WebSocket (SEC-008); darüber error limit |
| agents.fingerprint\_pairing | aus | Kopplung über Fingerprint (Weg B, 1.2); schaltet sich nach 30 min selbst ab, mit Audit-Eintrag |

Panel und Agent laufen mit read-only Root-Dateisystem; geschrieben wird nur in die Volumes. Die Compose-Dateien setzen read\_only: true.

## 6. Oberfläche

### 6.1 Grundsätze

| Punkt | Festlegung |
| --- | --- |
| Navigation | Seitenleiste links, einklappbar; Einträge je Rolle ausgeblendet, nicht ausgegraut |
| Serverseite | Kopf mit Status und Aktionen, darunter Reiter; Reiter ohne Recht werden ausgeblendet |
| Verwaltung | Listen mit Suche; die Benutzerliste zusätzlich mit Filtern, Mehrfachauswahl und Massenaktionen (UI-007), die übrigen Listen sind kurz; Details im Seitenpanel von rechts, die Liste bleibt sichtbar |
| Mobil | Karten, Befehlsseite und Konsole voll bedienbar, Mitteilungen ab R2; Verwaltung nur lesend: Suche, Filter, Details und Kopieren sind erlaubt, Schreibendes entfällt. Ausnahme für den Notfall: Sperren, Entsperren und Sitzungen beenden im Seitenpanel eines Benutzers, jeweils mit Bestätigungsdialog. Grenze 768 px, sie betrifft nur die Darstellung; das Backend prüft jede Anfrage wie auf dem Desktop. Seitenleiste wird zum Menü, die Zeitleiste der Konsole wird ausgeblendet |
| Darstellung | Dunkles Design als Standard, helles Design wählbar; Stilrichtung laut den Ansichten (6.5, D-037) |
| Kontrast | Text mindestens 4,5:1; Symbole, Rahmen von Controls, Fokusring und Statuspunkte mindestens 3:1 auf jeder Fläche in beiden Designs (WCAG 1.4.11). Rahmen von Karten und Tabellen sind ausgenommen, sie gliedern nur; eine Karte ist an Titel und Status erkennbar, ihr Fokusring erreicht 3:1 |
| Fremdtext | Bidi-Steuerzeichen (U+061C, U+200E, U+200F, U+202A bis U+202E, U+2066 bis U+2069) wirken im Freitext nicht: Antworten, Log-Zeilen und angezeigte Befehle zeigen sie als Platzhalter wie ⟨U+202E⟩, jede Zeile steht in einem eigenen bdi-Element. Kopieren liefert den Originaltext ohne Farbcodes; das Backend behält die Zeichen im Freitext (SEC-006). Escaping verhindert Code, aber keine Täuschung: Bidi-Zeichen können einen Namen oder eine Zeile als etwas anderes erscheinen lassen (wie bei Trojan Source). Andere Cf-Zeichen wie der Zero Width Joiner bleiben im Freitext, Emoji und einige Schriften brauchen sie |
| Restart-Schleife | Mindestens 3 Starts binnen 10 min |
| Live-Daten | Veraltete Werte werden markiert statt ausgeblendet (Haupttab 4.6) |
| Bestätigung | Löschen und Widerruf bestätigt ein Dialog, ebenso ein Rollenwechsel (Hinweis „Zuweisungen werden entfernt, Sessions enden“) und das globale Abschalten der Supporter. Nicht destruktive Änderungen sendet die Oberfläche erst nach 5 s, bis dahin lassen sie sich rückgängig machen; beim Verlassen der Seite (pagehide) werden offene Änderungen sofort gesendet |
| Kopieren | Ohne Clipboard-API, also ohne sicheren Kontext wie über http://lan-ip, markiert der Knopf den Text und zeigt den Hinweis „Press Ctrl+C“ |
| Tastaturkürzel | Standardmäßig aus, nur global durch den RCON-Admin aktivierbar |
| Auslieferung | Pfade ohne Dateiendung erhalten index.html (no-cache) samt base href aus RCD\_BASE\_PATH, ohne Inline-Skript; Pfade mit Dateiendung sind Assets und antworten bei Fehlen mit 404; versteckte Dateien werden nicht ausgeliefert; nur GET und HEAD; ohne gebaute Oberfläche 503 |

### 6.2 Navigation je Rolle

| Eintrag | RCON-Admin | Server-Admin | Moderator | Supporter | Release |
| --- | --- | --- | --- | --- | --- |
| Übersicht (Landingpage) | ja | ja | ja | ja | R1 |
| Server | alle | Scope | eigene | eigene | R1 |
| Topologie | ja | Scope | – | – | R2 |
| Mitteilungen | ja | ja | ja | ja | R2 |
| Verwaltung: Benutzer | ja | Moderatoren, Supporter | Supporter, falls freigegeben | – | R1 |
| Verwaltung: Gruppen | ja | Moderatorgruppen | – | – | R1 |
| Verwaltung: Befehlslisten | ja | eigene | – | – | R1 |
| Verwaltung: Server, Servergruppen | ja | lesend im Scope | – | – | R1 |
| Verwaltung: Agents | ja | – | – | – | R1 |
| Verwaltung: Einstellungen | ja | – | – | – | R1 |
| Verwaltung: Backups | ja | – | – | – | nach M5 |
| Audit | ja | Scope | – | – | R1 |
| Eigenes Profil | ja | ja | ja | ja | R1 |

### 6.3 Reiter der Serverseite

| Reiter | RCON-Admin | Server-Admin | Moderator | Supporter | Inhalt |
| --- | --- | --- | --- | --- | --- |
| Übersicht | ja | ja | ja | ja | Status, Spieler, Version, Last (nur mit Agent und nur Admins), Ports und Volumes ab R2 |
| Konsole | ja | ja | – | – | Freie Eingabe, Blöcke, Zeitleiste, Spuren |
| Befehle | – | – | ja | ja | Erlaubte Befehle als Buttons mit Parameterfeldern |
| Logs | ja | ja | – | – | Container-Logs über den Agent |
| Verlauf | ja | ja | eigene oder alle | eigene oder alle | Befehlsverlauf laut Verlaufssichtbarkeit (R2), in R1 nur eigene Befehle für Moderator und Supporter |

Aktionen im Kopf (Start, Stop, Restart) erscheinen nur für RCON-Admin und Server-Admin und nur, wenn der Server einem Agent zugeordnet ist. Ohne Agent zeigt der Kopf für diese Rollen im Scope nur „Neustart über RCON (stop-Befehl)“. Der Bestätigungsdialog sagt: Der Server wird über den stop-Befehl beendet und startet nur neu, wenn ein Neustart-Mechanismus greift (z. B. Docker restart: always oder unless-stopped; on-failure greift bei sauberem Beenden nicht). Start und Stopp gibt es nur mit Agent.

### 6.4 Aufbau der Serverseite

Moderator und Supporter sehen statt der Konsole den Reiter Befehle; Seitenleiste und Kopf bleiben gleich.

### 6.5 Ansichten

| Ansicht | Rolle | Zeigt |
| --- | --- | --- |
| Übersicht | RCON-Admin | Gesundheitsleiste, Hinweise, Server-Karten nach Servergruppen mit allen Statusvarianten und veralteten Werten |
| Serverseite · Konsole | RCON-Admin, Server-Admin | Kopf mit Aktionen, Reiter, Spur-Schalter, Zeitleiste, Blöcke, Eingabe mit Autovervollständigung |
| Serverseite · Befehle | Moderator, Supporter | Befehle als Formularkarten mit Parameterfeldern und Vorschau, Spieler online, eigener Verlauf |
| Verwaltung · Benutzer | RCON-Admin, Server-Admin | Tabelle mit Mehrfachauswahl und Massenaktionen, Seitenpanel, Rückgängig-Hinweis |
| Handy · Befehle | Moderator, Supporter | Befehle als Karten untereinander, eine ausgeklappt mit Feldern, Spieler online als Bottom-Sheet, Ziele mindestens 44 px |
| Handy · Konsole | RCON-Admin, Server-Admin | Aktionen im Menü oben rechts, Spur-Chips, Blöcke ohne Zeitleiste, Vorschläge als Chips über der Eingabe |

Stil: dunkles Design mit Statusrand an den Karten, Monospace für Server- und Befehlsnamen, farbige Rahmen je Servergruppe. Port-Chips auf den Karten folgen in R2 (UI-005). Die Handy-Ansicht steht in 6.6.

### 6.6 Handy-Ansicht

Die Seitenleiste wird zum Menü, Start, Stopp und Neustart wandern ins Menü im Kopf. Die Befehlskarten stehen untereinander, die Spieler online liegen als Bottom-Sheet am unteren Rand. Die Verwaltung bleibt mobil nur lesend, ausgenommen Sperren, Entsperren und Sitzungen beenden mit Bestätigungsdialog (UI-012, 6.1).

## 7. Spielprofile

### 7.1 Grundsätze

- Ein Profil je Spiel als YAML-Datei. Mitgelieferte Profile liegen im Repository unter profiles/ und sind ins Panel-Image eingebettet; eigene Profile liegen in /data/profiles.
- Ein eigenes Profil mit derselben id ersetzt das mitgelieferte und wird in der Oberfläche als angepasst markiert.
- Änderungen kommen über Dateiereignisse; zusätzlich gleicht das Panel alle profiles.poll\_interval (5.6) Änderungszeit und Inhalts-Hash ab, weil Netzlaufwerke (NFS, SMB) und gemountete Host-Verzeichnisse unter Docker Desktop keine Ereignisse liefern. Dort gilt die 5-s-Grenze von CAT-003 nur mit entsprechend kurzem Intervall.
- Jede Datei trägt eine Schemaversion. Eine ungültige Datei wird nicht importiert; der zuletzt gültige Stand bleibt aktiv, der Fehler steht im Log und in der Gesundheitsübersicht.
- Der Katalog erlaubt oder verbietet nichts (CAT-002). Er liefert Autovervollständigung, Beschreibungen und die Parameterfelder der Befehlsseite.

### 7.2 Felder

| Feld | Pflicht | Inhalt | Release |
| --- | --- | --- | --- |
| schema | ja | Schemaversion, derzeit 1 | R1 |
| id, name | ja | Eindeutiger Schlüssel (Kleinbuchstaben, Ziffern, Bindestrich) und Anzeigename | R1 |
| match.images | nein | Image-Muster zur Vorauswahl des Profils beim Anlegen eines Servers | R1 |
| protocol.type, protocol.default\_port | ja | Protokolladapter (source-rcon in R1) und Standardport | R1 |
| ports.game | nein | Spielports mit Port, Protokoll und Name (SRV-005) | R1 |
| syntax.case\_sensitive | nein | Groß-/Kleinschreibung beim Abgleich, Standard false (CAT-005) | R1 |
| syntax.strip\_prefix | nein | Präfix, das vor dem Abgleich entfernt wird, Standard / | R1 |
| syntax.max\_length | nein | Höchstlänge eines Befehls nach der Bereinigung | R1 |
| syntax.wrappers | nein | Befehle, die für Moderator und Supporter immer gesperrt sind (D-026) | R1 |
| syntax.separators | nein | Zeichenfolgen, an denen die Konsole des Spiels Befehle trennt, z. B. ; bei Source-Servern; für Moderator und Supporter abgelehnt (3.3) | R1 |
| syntax.player\_pattern | nein | RE2-Muster für einen ganzen Spielernamen (7.3), z. B. Minecraft Java ^[A-Za-z0-9\_]{1,16}$ | R1 |
| players.command, players.pattern, players.separator | nein | Befehl und RE2-Muster für die Spielerliste | R1 |
| stop.command | nein | Befehl für den Neustart ohne Agent (AGT-009) | R1 |
| catalog | nein | Liste der Katalogbefehle (7.3) | R1 |
| version.pattern | nein | RE2-Muster für die Versionserkennung im Agent (AGT-006) | R2 |
| formatting | nein | Darstellung von Farbcodes, z. B. minecraft (CMD-017) | R2 |

### 7.3 Katalogbefehle und Parameter

Jeder Eintrag hat path (Token-Pfad wie in den Befehlslisten), description und optional params; category folgt in R2 (CAT-004). Parameter erzeugen die Felder der Befehlsseite (CMD-003) und werden in Reihenfolge an den Pfad angehängt.

| Parametertyp | Feld auf der Befehlsseite | Prüfung im Backend |
| --- | --- | --- |
| player | Auswahl aus der Spielerliste, freie Eingabe möglich | ein Spielername passend zu syntax.player\_pattern; ohne Muster mindestens kein führendes @ (Selektoren wie @a), keine Anführungszeichen und Steuerzeichen |
| text | Textfeld, nur als letzter Parameter | Rest der Zeile |
| choice | Umschalter mit festen Werten | nur die genannten Werte, Groß-/Kleinschreibung laut Profil |
| number | Zahlenfeld mit optionalem min und max | ganze Zahl im Bereich |

Die Prüfung gilt in R1 für Moderator und Supporter (3.3, Schritt 7b), Pflichtparameter müssen vorhanden sein; die Befehlsseite prüft nach denselben Regeln. Einschränkungen je Listeneintrag, die ein Server-Admin festlegt, folgen mit CMD-009 (R2); die Prüfung in R1 kennt nur die festen Parameter aus dem Spielprofil und gilt für alle Listen gleich.

Befehle einer Befehlsliste ohne Katalogeintrag erscheinen auf der Befehlsseite mit einem freien Feld für die restlichen Argumente. Eigene Befehlsdefinitionen (CMD-018) sollen später dieselben Parametertypen verwenden.

### 7.4 Beispiel Minecraft Java

```yaml
schema: 1
id: minecraft-java
name: Minecraft (Java Edition)
match:
  images: ["itzg/minecraft-server*"]
protocol:
  type: source-rcon
  default_port: 25575
ports:
  game:
    - { port: 25565, proto: tcp, name: game }
syntax:
  case_sensitive: false
  strip_prefix: "/"
  max_length: 1000
  wrappers: [execute]
players:
  command: list
  pattern: "players online: (.*)$"
  separator: ", "
stop:
  command: stop
version:
  pattern: "Starting minecraft server version (\\S+)"
formatting: minecraft
catalog:
  - path: list
    description: Show players online
  - path: whitelist add
    description: Add a player to the whitelist
    params:
      - { name: player, type: player }
  - path: kick
    description: Kick a player from the server
    params:
      - { name: player, type: player }
      - { name: reason, type: text, optional: true }
  - path: time set
    description: Set the time of day
    params:
      - { name: time, type: choice, values: [day, night] }
  - path: say
    description: Message to all players
    params:
      - { name: text, type: text }
```

Die Zahlenwerte im Beispiel (z. B. max\_length) werden beim ersten Profil gegen das Spiel geprüft. Beschreibungen und Parameternamen sind englisch wie die Oberfläche in R1 (NFR-007); Übersetzungen kommen ab R2 über die Sprachdateien, nicht über das Profil.

## 8. Diagnose und Testbarkeit

### 8.1 Grundsätze

- Panel und Agent schreiben jedes prüfrelevante Ereignis als eine JSON-Zeile mit festem Ereigniscode auf stdout. Damit lassen sich Abnahmekriterien über die Container-Logs nachweisen, z. B. per Portainer-MCP.
- Ereigniscodes sind eine Schnittstelle: dokumentiert, innerhalb einer Hauptversion nie umbenannt oder entfernt, nur ergänzt.
- RCD\_LOG\_LEVEL=debug ergänzt Details (Dauer, Zwischenschritte), ändert aber keine Ereigniscodes. Für die Abnahme läuft die Testinstanz mit RCD\_LOG\_FORMAT=json und RCD\_LOG\_LEVEL=debug.
- Keine Geheimnisse in Logs, auch nicht auf debug: Passwörter, RCON-Zugangsdaten, Tokens, Schlüssel und Session-IDs werden ersetzt; Sessions erscheinen nur als kurzer Hash. Einzige Ausnahme ist die einmalige Ausgabe des Initialpassworts (AUTH-007) als Hinweisblock auf stderr; stdout bleibt reines JSON, dort steht nur auth.initial\_password.issued ohne Passwort.
- Abnahme über Logs nur gegen eine Testinstanz, nie gegen den Produktivbetrieb.

### 8.2 Felder jeder Ereigniszeile

| Feld | Inhalt | Immer |
| --- | --- | --- |
| ts | Zeitstempel UTC, RFC 3339 mit Millisekunden | ja |
| level | debug, info, warn, error | ja |
| comp | panel oder agent, dazu Modul (z. B. panel.cmd) | ja |
| event | Ereigniscode aus 8.3 | ja |
| request\_id | Request-ID der Anfrage bzw. des Befehls (CMD-007) | wenn vorhanden |
| session | erste 8 Zeichen des Session-Hashes | wenn vorhanden |
| user, role | Pseudonym und Rolle | wenn vorhanden |
| server, agent | ID und Name | wenn vorhanden |
| reason | Grund-Code wie in der API (Design 3.5) | bei Ablehnung oder Fehler |
| duration\_ms | Dauer | nur debug |
| version | Release-Version | bei start und Handshake |

### 8.3 Ereigniscodes (R1)

| Bereich | Codes |
| --- | --- |
| Betrieb | app.start, app.ready, db.migrate.start, db.migrate.done, db.migrate.lock\_wait, key.created, key.loaded, backup.done, backup.failed, audit.purge, audit.entry\_lost |
| Anmeldung | auth.login.ok, auth.login.failed, auth.lockout, auth.logout, auth.session.revoked, auth.initial\_password.issued, auth.invite.used, auth.emergency\_admin.reset |
| Rechte | perm.denied (reason), perm.recomputed |
| Befehle | cmd.accepted, cmd.denied (reason: not\_in\_list, wrapper\_blocked, rate\_limited, invalid), cmd.sent, cmd.response, cmd.timeout |
| RCON | rcon.connected, rcon.auth\_failed, rcon.disconnected, rcon.reconnect |
| Profile | profile.imported, profile.rejected (reason), profile.overridden |
| Agent im Panel | agent.connected, agent.rejected (reason: version, revoked, unknown, pending, invalid, pin\_mismatch), agent.paired, agent.revoked, agent.limit (reason: connection\_rate, message\_size, pending\_pairings), agent.disconnected (reason: closed, timeout, revoked, too\_large, protocol) |
| Agent selbst | agent.handshake, agent.forbidden (Container ohne Label oder Operation nicht erlaubt), agent.unsupported, docker.action, logs.follow.start, logs.follow.stop, logs.tail |
| Live-Daten | ws.subscribe, ws.subscribe.denied, cache.stale |

### 8.4 Ablauf einer Abnahme über MCP

1. Testinstanz mit json-Logs und debug starten (beide Compose-Varianten möglich).
2. Testschritt auslösen: durch Rob in der Oberfläche, per API-Aufruf oder durch ein Testskript.
3. Container-Logs von Panel und Agent ab dem Zeitpunkt des Schritts über den Portainer-MCP lesen.
4. Erwartete Ereigniscodes und Felder prüfen (z. B. cmd.denied mit reason wrapper\_blocked und passender request\_id) und das Ergebnis im Tab Abnahme R1 vermerken.

Welche Kriterien so prüfbar sind, steht im Tab Abnahme R1, Abschnitt 9.
