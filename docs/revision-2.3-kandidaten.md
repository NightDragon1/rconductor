# Kandidaten für Revision 2.3

Stand: Beginn M3 (02.11.2026). Abweichungen, Lücken und Festlegungen, die nach der Freigabe von Dokumentversion 2.2 aufgefallen sind. Keiner der Punkte ändert das freigegebene Konzept 2.2; über die Übernahme entscheidet Rob. Nr. 1 bis 9 sind von Rob für M3 entschieden (Freigabe des M3-Plans).

| Nr. | Thema | Betroffen |
| --- | --- | --- |
| 1 | Container über den Namen statt über die ID zuordnen | Design 4.2, SRV-001 |
| 2 | Zahlenwerte der Agent-Grenzen | SEC-007, Design 1.2, 1.5, 1.6 |
| 3 | Schalter agents.fingerprint\_pairing mit Selbstabschaltung | AGT-005, Design 1.2, 5.6 |
| 4 | Format des Kopplungs-Tokens, \_FILE-Variante, Agent-ID | Design 1.2, 1.3, 5.3, D-033 |
| 5 | Ereigniscode agent.disconnected | NFR-005, Design 8.3 |
| 6 | Grund-Code agent\_unavailable | AGT-003, AGT-009, Design 3.5 |
| 7 | Socket-Zugriff des Agents und Socket-Proxy | AGT-001, SEC-010, Technologie 5 |
| 8 | Verweise und Release-Angaben im Cache-Kapitel | NFR-005, Gesamtkonzept 4.6, Design 1.5 |
| 9 | Teilabnahme AGT-005 in M4 | Plan R1 Kapitel 3, Abnahme AGT-005 |

## 1. Container über den Namen statt über die ID zuordnen

- **Fundstelle:** 03-design.md, Kapitel 4.2 (servers.container\_id); 01-gesamtkonzept.md, SRV-001.
- **Befund:** Docker vergibt bei jedem Neuanlegen eines Containers (compose up mit geändertem Image oder --force-recreate, Image-Update) eine neue ID. Eine gespeicherte ID verliert dann die Zuordnung, obwohl derselbe Dienst weiterläuft.
- **Umsetzung (M3, nach Vorgabe):** Gespeichert wird der Containername; die ID hält der Agent-Hub nur im Speicher und löst sie bei jedem Snapshot und Ereignis neu auf. Bei jeder Auflösung wird die Label-Freigabe erneut geprüft; ein Container ohne Label ist nicht auflösbar.
- **Vorschlag:** Design 4.2: servers.container\_name statt container\_id.
- **Begründung:** Der Name ist in Compose stabil und für Menschen lesbar; die Label-Prüfung bei jeder Auflösung verhindert, dass ein gleichnamiger Container ohne Freigabe übernommen wird.

## 2. Zahlenwerte der Agent-Grenzen

- **Fundstelle:** 01-gesamtkonzept.md, SEC-007 (ohne Werte); 03-design.md, Kapitel 1.2 (wartende Anfragen begrenzt), 1.5 (log.lines), 1.6 (Rate je Container).
- **Umsetzung (M3, nach Vorgabe):**

  | Grenze | Wert |
  | --- | --- |
  | Nachricht vom Agent | höchstens 1 MiB; darüber trennt das Panel die Verbindung |
  | Auftrag vom Panel | höchstens 64 KiB |
  | log.lines | höchstens 500 Zeilen und höchstens 512 KiB Nutzdaten je Nachricht, sonst gekürzt mit truncated=true; je Zeile höchstens 8 KiB |
  | Verbindungsaufbau je Quelladresse | 10 je Minute, kurzzeitig 5 am Stück |
  | Wartende Kopplungen (Weg B) | höchstens 10, verworfen nach 24 h |
  | Aufträge je Container im Agent | 10 je Minute, kurzzeitig 3 am Stück; darüber Ergebniscode rate\_limited |
  | Laufende Logs (logs.follow) je Agent | höchstens 16 Container gleichzeitig |

  Zusätzlich zu den Fehlercodes aus Design 1.5: rate\_limited (Rate je Container überschritten) und error (Docker hat die Aktion abgelehnt).

- **Vorschlag:** Werte in Design 1.5 und 1.6 bzw. einem Abschnitt zu SEC-007 festhalten.
- **Begründung:** SEC-007 nennt keine Werte; die Abnahme braucht prüfbare Grenzen.

## 3. Schalter agents.fingerprint\_pairing mit Selbstabschaltung

- **Fundstelle:** 03-design.md, Kapitel 1.2 (Weg B standardmäßig aus, vom RCON-Admin einzuschalten) und 5.6 (kein Schlüssel); AGT-005.
- **Umsetzung (M3, nach Vorgabe):** Laufzeit-Einstellung agents.fingerprint\_pairing, Standard aus, nur RCON-Admin. Eingeschaltet schaltet sie sich nach 30 min selbst ab. Ein- und Ausschalten (auch das automatische) erzeugt einen Audit-Eintrag.
- **Vorschlag:** Zeile in Design 5.6; Selbstabschaltung in Design 1.2.
- **Begründung:** Weg B übernimmt die Panel-Identität per Trust on First Use; ein dauerhaft offenes Fenster lädt zu fremden Kopplungsanfragen ein.

## 4. Format des Kopplungs-Tokens, \_FILE-Variante, Agent-ID

- **Fundstelle:** 03-design.md, Kapitel 1.2 (Token enthält Panel-Adresse und Fingerprint, „Token-Nachweis“), 1.3 (hello mit Agent-ID), 5.3 (RCD\_PAIRING\_TOKEN / \_FILE); D-033.
- **Umsetzung (M3, nach Vorgabe):** Token = rcd1. + base64url(Panel-Adresse, Panel-Fingerprint, 32 Byte Zufall). Der Agent prüft beim TLS-Handshake den Fingerprint und sendet das Geheimnis erst danach; das Panel speichert nur den SHA-256-Hash. RCD\_PAIRING\_TOKEN\_FILE liest das Token aus einer Datei. Die Agent-ID vergibt das Panel bei der Kopplung; der Agent speichert sie in /secrets und sendet sie im hello. Das Geheimnis des Tokens geht im hello mit (Feld token). Eine Kopplung über Weg B, die auf die Freigabe wartet, beantwortet das Panel mit reject und dem zusätzlichen Grund pending; der Agent versucht es mit Backoff erneut.
- **Vorschlag:** Format und Ablauf in Design 1.2 und 1.3 festhalten.
- **Begründung:** „Token-Nachweis“ und Herkunft der Agent-ID sind im Design offen.

## 5. Ereigniscode agent.disconnected

- **Fundstelle:** 03-design.md, Kapitel 8.3 (kein Code für die Trennung eines Agents); 05-abnahme-r1.md, NFR-005 (Messwerte bei Agent-Trennung sofort veraltet).
- **Umsetzung (M3, nach Vorgabe):** Neuer Code agent.disconnected mit reason (closed, timeout, revoked, too\_large, protocol).
- **Vorschlag:** In Design 8.3 unter „Agent im Panel“ ergänzen.
- **Begründung:** Ohne den Code ist die Trennung im Ereignis-Log nicht prüfbar (NFR-016).

## 6. Grund-Code agent\_unavailable

- **Fundstelle:** 03-design.md, Kapitel 3.5 (kein Code für fehlenden Agent); 2.2 (/servers/{id}/actions).
- **Umsetzung (M3, nach Vorgabe):** POST /servers/{id}/actions für einen Server mit Agent, dessen Agent nicht verbunden ist, antwortet 503 mit Grund-Code agent\_unavailable. Ein Server ohne Agent nutzt für restart den stop-Befehl aus dem Profil (AGT-009); start und stop ohne Agent sind nicht möglich (ebenfalls agent\_unavailable).
- **Vorschlag:** Zeile in Design 3.5.
- **Begründung:** server\_unavailable meint die RCON-Verbindung; die Oberfläche muss beide Fälle unterscheiden.

## 7. Socket-Zugriff des Agents und Socket-Proxy

- **Fundstelle:** 04-technologie.md, Kapitel 5 Punkt 1 (Entscheidung in M3); SEC-010.
- **Umsetzung (M3, nach Vorgabe):** Der Agent läuft als Nicht-Root-Benutzer; Zugriff auf den Socket über group\_add mit der GID des Sockets. Das README sagt offen, dass Zugriff auf den Docker-Socket root-äquivalent ist und Nicht-Root nur Defense-in-Depth bietet. Beispiel für SEC-010 mit linuxserver/docker-socket-proxy (aktiver gepflegt als Tecnativa/docker-socket-proxy: wöchentliche Builds, letzter Push 01.11.2026, 3 offene Issues gegenüber Release vom 27.07.2026 und 59 offenen Issues; GPL-3.0, nur als eigenes Image genutzt). Freigegeben werden nur CONTAINERS, EVENTS, ALLOW\_START, ALLOW\_STOP und ALLOW\_RESTARTS, kein POST allgemein.
- **Vorschlag:** Technologie 5 Punkt 1 schließen; Abschnitt im README.
- **Begründung:** Entscheidung war für M3 vorgesehen.

## 8. Verweise und Release-Angaben im Cache-Kapitel

- **Fundstelle:** 05-abnahme-r1.md, NFR-005 („Frist aus Design 4.6“, Design hat kein Kapitel 4.6); 01-gesamtkonzept.md, Kapitel 4.6 (Spielversion über den Agent ohne Release-Angabe, AGT-006 und Design 7.2 version.pattern sind R2; Zeile CPU, RAM, Netz ohne Disk-I/O); 03-design.md, Kapitel 1.5 (metrics mit Disk-I/O).
- **Umsetzung (M3, nach Vorgabe):** Spielversion nicht in M3; Disk-I/O wird gesammelt und gepuffert wie CPU, RAM und Netz.
- **Vorschlag:** Abnahme NFR-005 auf Gesamtkonzept 4.6 verweisen; in 4.6 die Spielversion als R2 kennzeichnen und Disk-I/O in die Zeile der Messwerte aufnehmen.
- **Begründung:** Verweis ins Leere und widersprüchliche Release-Angaben.

## 9. Teilabnahme AGT-005 in M4

- **Fundstelle:** 07-plan-r1.md, Kapitel 3 (keine Teilabnahme für AGT-005); 05-abnahme-r1.md, AGT-005 (Prüfart E2E, Kopplungsdialog).
- **Umsetzung (M3, nach Vorgabe):** Kopplung im Backend in M3; Issue nach Umsetzung nach M4.
- **Vorschlag:** Zeile „AGT-005 | M3 | M4 | Kopplungsdialog, E2E“ in Plan R1 Kapitel 3.
- **Begründung:** Die Prüfart E2E setzt die Oberfläche voraus.

Außerdem für W5 (keine Konzeptänderung): Ein falscher Schlüssel scheitert beim TLS-Handshake; es gibt dann keine reject-Nachricht. Beide Seiten loggen agent.rejected mit reason pin\_mismatch.
