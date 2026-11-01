# Kandidaten für Revision 2.1

> Eingearbeitet in Dokumentversion 2.1 (Gesamtkonzept, Kapitel 10). Diese Datei bleibt als Begründung der Änderungen erhalten.

Stand: Abschluss M1 (01.11.2026). Abweichungen und Lücken, die bei der Umsetzung von M0 und der Planung von M1 aufgefallen sind; die Punkte 10 bis 15 sind bereits entschieden; die Punkte 16 bis 27 sind in M1 so umgesetzt und warten auf Bestätigung. Keiner der Punkte ändert das freigegebene Konzept 2.0; über die Übernahme entscheidet Rob. Verweise auf Commits beziehen sich auf den Stand nach dem Umschreiben der Historie.

| Nr. | Thema | Betroffen |
| --- | --- | --- |
| 1 | Reservierte Pfade für Health und Metriken | NFR-006, Design 2 |
| 2 | Auslieferung der eingebetteten Oberfläche | Design 2 und 6, Architektur 4 |
| 3 | Read-only Root-Dateisystem | SEC-002, Technologie 4 |
| 4 | Deep-Links mit Basispfad | NFR-003, Design 6 |
| 5 | Wortlaut der Abnahme von AGT-001 | Abnahme R1, Abschnitt 7 |
| 6 | CGO bei Tests | Technologie 1 und 4, CLAUDE.md |
| 7 | Anforderung für Lieferkette und CI | D-040, Technologie 4 |
| 8 | Socket-Zugriff des Agent-Images | Technologie 5.1, AGT-001 |
| 9 | Verzeichnis scripts/ | Technologie 4 |
| 10 | Session-Cookie: Secure nur bei HTTPS, __Host-Präfix, SameSite=Lax | Design 2.1, D-011, NFR-003, Plan R1 |
| 11 | Rechteänderung beendet Sessions | AUTH-006, Design 2.1 und 2.3 |
| 12 | Sichtbarkeit der Profilfelder | AUTH-009, Haupttab 3, Design 3 |
| 13 | Profilbild als BLOB in der Datenbank | NFR-010, Design 4.2, AUTH-009 |
| 14 | Tabelle grants: Moderatorgruppe immer genannt | Design 4.2, 4.3 |
| 15 | Benutzer nur per Einladung, kein POST /users | Design 2.2, UI-008 |
| 16 | Wer verwaltet wen | Design 3.6, ROLE-008 |
| 17 | Zuweisungen: nur eigene Einträge ersetzen, Servergruppe nur ganz im Scope | Design 2.2, 3.6, ROLE-004 |
| 18 | Rollenwechsel entfernt die Zuweisungen | ROLE-008, Design 3.6 |
| 19 | Grenze von W2: Listenänderung beendet keine Sessions | AUTH-006, Punkt 11 |
| 20 | Login-Schutz: Zähler im Speicher, Ablauf | SEC-004 |
| 21 | Neue Variable RCD\_PASSWORD\_HASH\_CONCURRENCY | Design 5.2 |
| 22 | Initialpasswort auf stderr | Design 8.1, AUTH-007 |
| 23 | Neue Laufzeit-Einstellungen und Standardwerte | Design 5.1, 3.4, 4.4 |
| 24 | Grund-Codes außerhalb von Befehlen | Design 3.5, 8.2 |
| 25 | Zusätzliche API-Pfade | Design 2.2 |
| 26 | Profilbild: nur PNG und JPEG, Verkleinerung auf 512 Pixel | AUTH-009, Punkt 13 |
| 27 | Teilabnahmen in M1 ohne Umhängen | ROLE-003, AUD-001, Plan R1 |

## 1. Reservierte Pfade für Health und Metriken

- **Fundstelle:** 01-gesamtkonzept.md, NFR-006 (R2: „Prometheus-Metriken und Healthcheck-Endpoint“); 03-design.md, Kapitel 2.2 nennt keine Pfade dafür. Umsetzung: internal/web/web.go (`reservedPaths`), Commit 6730098.
- **Vorschlag:** In Design 2.2 festlegen: `/healthz` (Healthcheck) und `/metrics` (Prometheus) liegen außerhalb von `/api/v1`, sind ab R1 reserviert und werden nie von der Oberfläche beantwortet. Ab R2 mit NFR-006 befüllt; Zugriffsschutz für `/metrics` dort festlegen.
- **Begründung:** Ohne Reservierung würde der SPA-Fallback diese Pfade mit `index.html` beantworten; ein Healthcheck meldete dann fälschlich „gesund“. Die Namen sind Konvention (Kubernetes, Prometheus), aber bisher nicht im Konzept.

## 2. Auslieferung der eingebetteten Oberfläche

- **Fundstelle:** 02-architektur.md, Kapitel 4 (Web-Schicht: „Auslieferung der Oberfläche“); 03-design.md, Kapitel 2 und 6 ohne Regeln dazu. Umsetzung: internal/webui/webui.go, Commit 6730098.
- **Vorschlag:** In Design 6.1 (oder 2.1) aufnehmen:
  - Pfade ohne Dateiendung liefern `index.html` (Client-Routen nach Neuladen), mit `Cache-Control: no-cache`.
  - Pfade mit Dateiendung sind Assets; fehlt die Datei, antwortet das Panel mit 404 als Problem Details, nie mit `index.html`. Gehashte Assets werden als `immutable` gecacht.
  - Versteckte Dateien (Pfadteil beginnt mit Punkt) werden nicht ausgeliefert.
  - Nur GET und HEAD; sonst 405.
  - Ohne gebaute Oberfläche antwortet jede Seite mit 503 „web UI is not built“; die API bleibt nutzbar.
- **Begründung:** Das Verhalten ist für Abnahme und Fehlersuche relevant (z. B. 404 statt leerer Seite bei fehlendem Asset), stand aber nirgends. Ohne Festlegung könnten spätere Änderungen es unbemerkt ändern.

## 3. Read-only Root-Dateisystem

- **Fundstelle:** 01-gesamtkonzept.md, SEC-002 (Least Privilege); 04-technologie.md, Kapitel 4 (distroless static, Nicht-Root). Read-only ist nicht erwähnt. Manuell geprüft mit `docker run --read-only` für Panel und Agent (M0, Schritt 10): beide starten und arbeiten.
- **Vorschlag:** SEC-002 bzw. Technologie 4 ergänzen: Panel- und Agent-Image laufen mit read-only Root-Dateisystem; geschrieben wird nur in die Volumes. Compose-Dateien (M5) setzen `read_only: true`. Abnahme: Integrationstest startet das Panel-Image mit read-only Root-Dateisystem.
- **Begründung:** Kostet nichts, weil das Panel schon heute nur in /data, /secrets und /backups schreibt, und erschwert das Festsetzen nach einer Kompromittierung. Ohne Aufnahme ins Konzept ist es nicht abgesichert und könnte durch eine spätere Änderung (z. B. temporäre Dateien außerhalb der Volumes) brechen.

## 4. Deep-Links mit Basispfad

- **Fundstelle:** 01-gesamtkonzept.md, NFR-003 (Basispfad); 05-abnahme-r1.md, NFR-003; 03-design.md, Kapitel 6. Umsetzung: web/vite.config.ts (`base: './'`), internal/webui. Nachverfolgt in Issue #88 (M4).
- **Vorschlag:** In Design 6.1 festlegen: Das Panel setzt beim Ausliefern von `index.html` ein `<base href>` aus `RCD_BASE_PATH` (ohne Inline-Skript, wegen SEC-009); der Router der Oberfläche nutzt denselben Basispfad. Abnahme von NFR-003 um „Neuladen eines tiefen Pfads lädt alle Assets“ ergänzen.
- **Begründung:** Mit relativen Asset-Pfaden löst der Browser bei `/servers/1` die Assets nach `/servers/assets/…` auf; das Panel antwortet dort korrekt mit 404, die Seite bleibt leer. Das betrifft jeden Betrieb, mit und ohne Basispfad, sobald es Client-Routen gibt (M4).

## 5. Wortlaut der Abnahme von AGT-001

- **Fundstelle:** 05-abnahme-r1.md, Abschnitt 7, AGT-001: „das Panel-Image enthält keinen Docker-Client“. go.mod enthält seit Commit d6498ce `github.com/moby/moby/client` als indirekte Abhängigkeit von testcontainers-go (nur Integrationstests).
- **Vorschlag:** Kriterium präzisieren: „Das Panel-Binary enthält keinen Docker-Client (geprüft mit `go version -m` bzw. `go list -deps ./cmd/panel`); Testabhängigkeiten sind ausgenommen.“
- **Begründung:** Bei einem Review von go.mod könnte das Kriterium fälschlich als verletzt gelten. Entscheidend ist, dass das ausgelieferte Panel nicht mit Docker sprechen kann; das ist über das Binary prüfbar.

## 6. CGO bei Tests

- **Fundstelle:** 04-technologie.md, Kapitel 1 („ohne CGO“) und Kapitel 4; CLAUDE.md, Abschnitt Technik. .github/workflows/ci.yml führt `go test -race` aus; der Race-Detector braucht CGO (Commit 96d2069).
- **Vorschlag:** Klarstellen: „Ausgelieferte Binaries und Images ohne CGO. Tests dürfen CGO nutzen (Race-Detector).“
- **Begründung:** Der Race-Detector findet Datenrennen in Verbindungsmanager, Ereignisbus und Agent-Hub (M2, M3), die sonst kaum auffallen. Die Binaries bleiben unberührt; die Image-Builds in der CI laufen weiter mit `CGO_ENABLED=0`.

## 7. Anforderung für Lieferkette und CI

- **Fundstelle:** 01-gesamtkonzept.md, D-040 (signierte Images mit SBOM); 04-technologie.md, Kapitel 4 (Lieferkette: syft, cosign, govulncheck, Renovate; CI: GitHub Actions); 07-plan-r1.md, M0 („CI mit Lint, Tests, govulncheck; Multi-Arch-Build und signierte Images“). Keine Anforderungs-ID; Commits tragen behelfsweise PRJ-001 (96d2069, b2403c1, 3dc0007).
- **Vorschlag:** Neue Anforderung, z. B. SEC-013 „Lieferkette“: Images für amd64 und arm64, signiert (cosign keyless) mit SBOM (syft); govulncheck als Pflicht-Check; Actions auf Commit-SHA gepinnt; Abhängigkeits-Updates per Renovate ohne Automerge. Abnahme: Review plus `cosign verify` eines Release-Images. Release-Tags erst nach Veröffentlichung des Repositories (Rekor-Log öffentlich).
- **Begründung:** Die Arbeit ist im Plan vorgesehen, aber keiner Anforderung zugeordnet; damit fehlen Issue, Abnahmekriterium und Nachweis. PRJ-001 (Open Source) passt nur lose.

## 8. Socket-Zugriff des Agent-Images

- **Fundstelle:** 04-technologie.md, Kapitel 5, offener Punkt 1 („Root oder docker-Gruppe des Hosts; wird bei der Umsetzung festgelegt“). Agent-Image in M0 als Stub mit Benutzer 65532 (Commit d6498ce).
- **Vorschlag:** In M3 entscheiden und in Technologie 4 sowie Design 5.3 festhalten. Empfehlung zur Prüfung: Nicht-Root mit `group_add` auf die GID des Docker-Sockets (in Compose per Variable); Root nur als dokumentierte Ausnahme; mit Socket-Proxy (SEC-010) entfällt der direkte Socket-Zugriff.
- **Begründung:** Die Entscheidung bestimmt Compose-Dateien, README und das Restrisiko T-022 im Bedrohungsmodell. Bis M3 hat der Stub keinen Socket-Zugriff.

## 9. Verzeichnis scripts/

- **Fundstelle:** 04-technologie.md, Kapitel 4 (Repository: cmd/panel, cmd/agent, internal, web, profiles, migrations, api/openapi.yaml). In CLAUDE.md mit Commit 3dc0007 ergänzt; enthält scripts/gh_plan.py (Commit b2403c1).
- **Vorschlag:** Technologie 4, Zeile Repository, um `scripts` (Hilfsskripte zur Projektpflege, nicht Teil der Images) und `deploy/compose` ergänzen.
- **Begründung:** Repository-Struktur in Konzept und CLAUDE.md sollen übereinstimmen; deploy/compose steht bisher nur in CLAUDE.md.

## 10. Session-Cookie: Secure nur bei HTTPS, __Host-Präfix, SameSite=Lax

- **Fundstelle:** 03-design.md, Kapitel 2.1 (Cookie mit HttpOnly, Secure, SameSite=Strict); 01-gesamtkonzept.md, D-011 (kein HTTPS für die Weboberfläche) und NFR-003; 06-sicherheit.md, T-001 und T-007; 07-plan-r1.md (NFR-003 in M5).
- **Entscheidung (M1):** Das Cookie ist immer HttpOnly und SameSite=Lax. Secure und das Präfix `__Host-` werden gesetzt, wenn die Oberfläche per HTTPS erreicht wird: `RCD_PUBLIC_URL` beginnt mit https oder ein vertrauenswürdiger Proxy (`RCD_TRUSTED_PROXIES`) meldet `X-Forwarded-Proto: https`. Ohne HTTPS warnt das Panel beim Start im Log. Die Auswertung von `RCD_TRUSTED_PROXIES` mit `X-Forwarded-For` und `X-Forwarded-Proto` wird aus M5 (NFR-003) nach M1 vorgezogen, weil Cookie, Login-Schutz und Sessions davon abhängen; Basispfad und WebSocket-Upgrade bleiben in M5.
- **Vorschlag:** Design 2.1 entsprechend anpassen; Plan R1 vermerkt den vorgezogenen Teil von NFR-003.
- **Begründung:** Browser senden Secure-Cookies nicht über reines HTTP; mit Design 2.1 wäre eine Anmeldung ohne TLS-Proxy unmöglich, obwohl D-011 diesen Betrieb vorsieht. SameSite=Lax statt Strict lässt das Cookie bei einer Navigation von außen (z. B. Link in einem Chat) mitgehen, sodass der Benutzer angemeldet ankommt; CSRF bleibt durch die Origin-Prüfung aller schreibenden Anfragen und beim WebSocket-Aufbau (Design 2.1) abgedeckt, denn Lax schickt das Cookie nur bei sicheren Methoden über Seitengrenzen.

## 11. Rechteänderung beendet Sessions

- **Fundstelle:** 01-gesamtkonzept.md, AUTH-006; 05-abnahme-r1.md, AUTH-006 („Abmelden, Sperren oder Rechteänderung beenden betroffene Sessions sofort“); 03-design.md, Kapitel 2.1 (nur Deaktivierung beendet Sessions, Rechte bei jeder Anfrage neu geprüft) und 2.3 (entzogene Abos enden mit error).
- **Entscheidung (M1):** Die Anforderung gilt. Ändern sich Rolle, Scope, Gruppenmitgliedschaft oder Zuweisungen eines Benutzers, beendet das Panel alle seine Sessions (`auth.session.revoked`); ab M2 auch seine WebSockets.
- **Vorschlag:** Design 2.1 an AUTH-006 angleichen.
- **Begründung:** Anforderung und Design widersprechen sich; die strengere Regel der Anforderung vermeidet, dass eine offene Oberfläche mit veraltetem Stand weiterarbeitet.

## 12. Sichtbarkeit der Profilfelder

- **Fundstelle:** 01-gesamtkonzept.md, AUTH-009 („für andere Rollen nur laut Rechtemodell sichtbar“); Haupttab 3 und Design 3 regeln Profilfelder nicht.
- **Entscheidung (M1):** Echtname, Profilbild und Kontaktangaben sieht der Benutzer selbst, der RCON-Admin immer und jeder, der den Benutzer verwalten darf (höhere Rolle in dessen Scope, Design 3.6). Alle anderen sehen nur das Pseudonym.
- **Vorschlag:** In Design 3.6 aufnehmen.
- **Begründung:** Die Anforderung verweist auf eine Regel, die es nicht gibt. Die gewählte Regel folgt dem Rechtefluss nach unten (ROLE-008) und gibt Kontaktdaten nur an Verantwortliche weiter.

## 13. Profilbild als BLOB in der Datenbank

- **Fundstelle:** 01-gesamtkonzept.md, NFR-010 („/data (SQLite, Profilbilder, eigene Profile)“) und AUTH-009; 03-design.md, Kapitel 4.2 (users.avatar\_path).
- **Entscheidung (M1):** Das Profilbild liegt als BLOB in der Datenbank statt als Datei unter /data. Beim Hochladen wird es am Inhalt geprüft, auf eine Pixelgrenze geprüft, neu kodiert (Metadaten wie EXIF entfallen) und mit nosniff und restriktiver Content-Security-Policy ausgeliefert.
- **Vorschlag:** NFR-010 (Profilbilder entfallen aus /data) und Design 4.2 (avatar statt avatar\_path, dazu Medientyp) anpassen; neue Revision von NFR-010 und AUTH-009 nur, falls sich das Abnahmekriterium ändert.
- **Begründung:** Ein Backup der Datenbank enthält dann alle veränderlichen Daten (NFR-008); es gibt keine zweite Ablage, die beim Wiederherstellen fehlen kann.

## 14. Tabelle grants: Moderatorgruppe immer genannt

- **Fundstelle:** 03-design.md, Kapitel 4.2 („genau eines von user\_id oder user\_group\_id“) gegen Kapitel 4.3 („Ein direkter Grant auf einen Moderator nennt deshalb immer auch die Moderatorgruppe“). Umsetzung: migrations/sqlite/00002\_identity.sql.
- **Entscheidung (M1):** user\_group\_id ist Pflicht (Quelle der Liste, Eigentümer = zuständiger Server-Admin); user\_id leer bedeutet alle Mitglieder, gesetzt nur dieses Mitglied, das Mitglied der Gruppe sein muss.
- **Vorschlag:** Design 4.2 an 4.3 angleichen.
- **Begründung:** Die beiden Stellen widersprechen sich; 4.3 beschreibt die nötige Information.

## 15. Benutzer nur per Einladung, kein POST /users

- **Fundstelle:** 03-design.md, Kapitel 2.2 (/users mit POST); 01-gesamtkonzept.md, UI-008 („neue Benutzer per Einladungslink … statt Passwortvergabe“).
- **Entscheidung (M1):** Benutzer entstehen nur beim Einlösen einer Einladung, dazu der Notfall-Admin; POST /users entfällt in R1.
- **Vorschlag:** Design 2.2 ohne POST bei /users; /invites als einziger Weg.
- **Begründung:** Zwei Wege mit fast gleicher Wirkung; die Einladung vermeidet, dass jemand ein Passwort für andere vergibt.

## 16. Wer verwaltet wen

- **Fundstelle:** 03-design.md, Kapitel 3.6 („nur Benutzer mit niedrigerer Rolle in seinem Scope“), ohne Definition des Scopes für Benutzer. Umsetzung: internal/perm/manage.go.
- **Umsetzung (M1):** RCON-Admin alle niedrigeren Rollen; Server-Admin die Moderatoren, die er eingeladen hat oder die in einer seiner Moderatorgruppen sind, deren Supporter und die von ihm eingeladenen Supporter; Moderator nur seine Supporter (eingeladen oder zugewiesen) und nur bei eingeschalteter Supporter-Freigabe; Supporter niemanden.
- **Vorschlag:** In Design 3.6 aufnehmen.
- **Begründung:** Ohne diese Regel ist ROLE-008 nicht prüfbar; sie folgt der Zuweisungskette aus Haupttab 3.

## 17. Zuweisungen: nur eigene Einträge ersetzen, Servergruppe nur ganz im Scope

- **Fundstelle:** 03-design.md, Kapitel 2.2 (/users/{id}/assignments PUT) und 3.6 („Zuweisungen an andere sind immer eine Teilmenge des eigenen Scopes“).
- **Umsetzung (M1):** Ein PUT ersetzt je Abschnitt nur die Einträge, über die der Aufrufer verfügt (eigene Gruppen, eigene Moderatoren); Einträge anderer bleiben. Eine Servergruppe darf nur zugewiesen werden, wenn alle ihre Server im Scope liegen.
- **Vorschlag:** In Design 2.2 und 3.6 aufnehmen.
- **Begründung:** Sonst könnte ein Server-Admin die Zuweisungen eines anderen löschen oder über eine teilweise fremde Servergruppe mehr vergeben, als er hat.

## 18. Rollenwechsel entfernt die Zuweisungen

- **Fundstelle:** 01-gesamtkonzept.md, ROLE-008; 03-design.md, Kapitel 3.6. Umsetzung: internal/users/users.go.
- **Umsetzung (M1):** Ändert sich die Rolle eines Benutzers, entfallen seine Scopes, Gruppenmitgliedschaften und Grants; seine Sessions enden.
- **Vorschlag:** In Design 3.6 aufnehmen.
- **Begründung:** Zuweisungen gehören zu einer Rolle (Scopes nur für Server-Admins, Grants nur für Moderatoren); stehen gelassene Reste würden nach einem Rückwechsel unbemerkt wieder wirken.

## 19. Grenze von W2: Listenänderung beendet keine Sessions

- **Fundstelle:** Punkt 11; 01-gesamtkonzept.md, AUTH-006.
- **Umsetzung (M1):** Änderungen an Rolle, Scope, Gruppenmitgliedschaft, Grants, Supporter-Freigabe und Sperre beenden Sessions; Änderungen am Inhalt einer Befehlsliste wirken ab der nächsten Anfrage, ohne Abmeldung.
- **Vorschlag:** In AUTH-006 bzw. Design 2.1 festhalten, was als Rechteänderung zählt.
- **Begründung:** Sonst meldet jede Listenpflege alle Moderatoren ab.

## 20. Login-Schutz: Zähler im Speicher, Ablauf

- **Fundstelle:** 01-gesamtkonzept.md, SEC-004; 05-abnahme-r1.md, SEC-004. Umsetzung: internal/auth/throttle.go.
- **Umsetzung (M1):** Verzögerung ab dem dritten Fehlversuch eines Kontos (1, 2, 4 s); Sperre 15 min je (Konto, Client-Adresse) nach 5 Fehlversuchen; unbekannte Pseudonyme zählen wie echte; Zähler im Speicher (höchstens 10 000 Einträge), sie enden nach der Sperrdauer ohne Fehlversuch, mit erfolgreicher Anmeldung und beim Neustart.
- **Vorschlag:** In SEC-004 bzw. Design aufnehmen, auch dass ein Neustart die Zähler leert.
- **Begründung:** Werte und Verhalten waren nur als „Verzögerung bzw. zeitweise Sperre“ beschrieben.

## 21. Neue Variable RCD\_PASSWORD\_HASH\_CONCURRENCY

- **Fundstelle:** 03-design.md, Kapitel 5.2. Umsetzung: internal/config, internal/auth/password.
- **Umsetzung (M1):** Höchstzahl gleichzeitiger Argon2id-Berechnungen (Standard 2, 1 bis 64); wer länger als 5 s auf einen Platz wartet, erhält 503 mit Retry-After.
- **Vorschlag:** In Design 5.2 aufnehmen.
- **Begründung:** Jede Berechnung braucht 64 MiB; ohne Grenze reicht eine Login-Flut für Speichermangel, besonders auf kleinen Hosts.

## 22. Initialpasswort auf stderr

- **Fundstelle:** 03-design.md, Kapitel 8.1 („außerhalb der Ereignisfelder“); 01-gesamtkonzept.md, AUTH-007.
- **Umsetzung (M1):** Das Passwort steht einmal als Hinweisblock auf stderr; auf stdout erscheint nur auth.initial\_password.issued ohne Passwort.
- **Vorschlag:** In Design 8.1 so festhalten.
- **Begründung:** stdout bleibt reines JSON und lässt sich ohne Sonderfall auswerten; im Container-Log stehen beide Ströme.

## 23. Neue Laufzeit-Einstellungen und Standardwerte

- **Fundstelle:** 03-design.md, Kapitel 5.1 (Laufzeit-Einstellungen in settings), ohne Schlüsselliste.
- **Umsetzung (M1):** session.idle\_timeout (12 h), session.max\_lifetime (7 Tage), login.max\_failures (5), login.lockout\_duration (15 min), invite.lifetime (72 h), supporters.enabled (aus), audit.retention\_days (90); dazu die Backup-Schlüssel aus M0. Änderbar nur durch den RCON-Admin über /settings, mit Typ- und Bereichsprüfung.
- **Vorschlag:** Schlüsselliste mit Standardwerten in Design 5 aufnehmen.
- **Begründung:** Die Namen sind Teil der Schnittstelle von /settings.

## 24. Grund-Codes außerhalb von Befehlen

- **Fundstelle:** 03-design.md, Kapitel 3.5 (Grund-Codes nur für Befehle) und 8.2 (reason wie in der API).
- **Umsetzung (M1):** Verwaltungs-Ablehnungen antworten 403 ohne Grund-Code bzw. 404 mit not\_found; perm.denied und das Audit tragen reason forbidden; 503 bei ausgelasteter Passwortberechnung ohne Grund-Code; 403 bei fehlender Origin ohne Grund-Code.
- **Vorschlag:** Grund-Code forbidden (und ggf. busy) in Design 3.5 ergänzen.
- **Begründung:** Log und Audit brauchen einen festen Wert; derzeit ist forbidden nur im Log und Audit definiert.

## 25. Zusätzliche API-Pfade

- **Fundstelle:** 03-design.md, Kapitel 2.2.
- **Umsetzung (M1):** /user-groups/{id}, /command-lists/{id}, /invites/{id}, /invites/redeem, /me/password, /me/avatar, /users/{id}/avatar.
- **Vorschlag:** Tabelle in Design 2.2 ergänzen; api/openapi.yaml ist die Quelle.
- **Begründung:** Einzelzugriff, Einlösen und Passwortwechsel brauchen eigene Pfade.

## 26. Profilbild: nur PNG und JPEG, Verkleinerung auf 512 Pixel

- **Fundstelle:** Punkt 13; 01-gesamtkonzept.md, AUTH-009.
- **Umsetzung (M1):** PNG und JPEG, höchstens 1 MiB und 4096 × 4096 Pixel; gespeichert verkleinert auf höchstens 512 × 512.
- **Vorschlag:** WebP erst mit golang.org/x/image (BSD-3-Clause) als neuer Abhängigkeit, falls gewünscht.
- **Begründung:** Die Standardbibliothek dekodiert kein WebP; die Verkleinerung hält die BLOBs und damit die Backups klein.

## 27. Teilabnahmen in M1 ohne Umhängen

- **Fundstelle:** 05-abnahme-r1.md, ROLE-003 („Server außerhalb liefern 404“) und AUD-001 („Befehle, … Agent-Aktionen … erzeugen je einen Eintrag“); 07-plan-r1.md, M1.
- **Stand (M1):** Scope-Berechnung und Audit-Grundgerüst sind fertig und getestet; Server-Endpunkte (M2), Befehle (M2) und Agent-Aktionen (M3) fehlen noch.
- **Vorschlag:** Wie bei NFR-001: Issues ROLE-003 nach M2 und AUD-001 nach M3 umhängen, oder im Plan R1 als teilweise in M1 vermerken.
- **Begründung:** Die Abnahmekriterien setzen spätere Meilensteine voraus.
