# Übergabe

Stand: 04.11.2026, Meilenstein M4, Schritt 9 (Abbruch auf Wunsch, Stand gesichert)

## Repository

- main: e84ea2d (Entsperren mit Dialog, Suchfeld auf der Befehlsseite), CI grün.
- m4-wip: 8d3d832, zwei WIP-Commits auf main, mit --no-verify, make check nicht gelaufen.
  - 2b52548: Kartenlisten statt Tabellen auf dem Handy, Umbruch statt Abschneiden.
  - 8d3d832: E2E-Prüfung auf abgeschnittenen Text und auf Tabellen.

## Erledigt seit der letzten Übergabe

- #106 behoben und geschlossen (33c7da4): `scripts/pull-test-images.sh` vor Integrations- und Minecraft-Tests.
- Schritt 9 auf main: 44 px, Avatar im Kopf, ⋮-Menü, Konsole, Befehle, Verwaltung nur lesend, Projekt `mobile`.
- Kandidaten 25 und 26; Entsperren mit Dialog; Suchfeld „Search commands“ (Desktop und Handy).
- Neue Abhängigkeit: `@radix-ui/react-dropdown-menu` 2.2.1 (MIT). Neue Ereigniscodes: keine.

## Korrektur zur letzten Übergabe

- 889a1d4: Das Log war abrufbar, nur `--log-failed` blieb leer; die Job-API lieferte es. Die Aussage „nicht mehr abrufbar“ war falsch.
- Ursache: `TestAgentThroughSocketProxy` mit „No such image“, sporadisch; der Neustart auf demselben Commit war grün. Siehe #106.

## Offen

- Auf m4-wip ungeprüft: Die letzte Suite wurde abgebrochen, ihr Ergebnis ist unbekannt. Vorher waren die 13 Tests im Projekt `mobile` lokal grün.
- Screenshots m4-9 sind veraltet: Sie zeigen noch die Tabellen und liegen auf main. Neu aufnehmen.
- Freigabe der Screenshots m4-9 steht aus.
- #103: Renovate hat rebased (9d3a7e5), CI lief noch (8 grün, 1 offen). Noch nicht gemergt.
- Kandidaten 14 bis 26 warten auf die Revision 2.4.
- Issue #101 (/backups) folgt nach M5.

## Offene CI-Läufe

- main: keine, e84ea2d ist grün.
- m4-wip: Die CI läuft nur für main und PRs, also kein Lauf.
- #103: ein Job offen.

## Nächster Handgriff

1. Auf m4-wip: `make check`, dann `npx playwright test` in web/ (volle Suite).
2. Grün: Änderungen als saubere Commits nach main bringen, ohne Force-Push auf main.
3. `SCREENSHOTS=9 npx playwright test screenshots --project root -g phone`, Screenshots committen, auf OK warten.
4. #103 mergen, wenn die CI grün ist.
