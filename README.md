# pikasplit

Piloter un Mac depuis une fenêtre sur un PC Windows, sur le réseau local, pour
ne plus avoir à changer la source de l'écran.

- `pikasplit-host` tourne sur le Mac : il reçoit le clavier et la souris et les
  injecte.
- `pikasplit-view` tourne sur le PC : quand sa fenêtre a le focus, le clavier
  et la souris du PC partent vers le Mac.

État : jalon 2 (clavier/souris seulement). La vidéo du Mac dans la fenêtre
arrive aux jalons suivants.

## Build

Sur le Mac (Xcode Command Line Tools + Go) :

```sh
go build -o bin/pikasplit-host ./cmd/pikasplit-host
```

Sur le PC (Go seul, pas de cgo) :

```sh
go build -o bin/pikasplit-view.exe ./cmd/pikasplit-view
```

## Premier lancement (appairage)

1. Sur le Mac : `./bin/pikasplit-host -pair`. Au premier lancement, macOS
   demande la permission **Accessibilité** pour l'app qui lance la commande
   (Terminal, iTerm…). Accordez-la dans Réglages Système > Confidentialité et
   sécurité > Accessibilité, puis relancez.
2. Le Mac affiche la commande à lancer sur le PC, par exemple :
   `pikasplit-view -host 192.168.1.20:7878 -pair K7QF-2M4X-PZ3A-9HTD -pin 123456`
3. Ensuite, sans appairage : `./bin/pikasplit-host` sur le Mac et
   `pikasplit-view -host 192.168.1.20` sur le PC.

## Utilisation

- Cliquez dans la fenêtre pour piloter le Mac (ce premier clic n'est pas
  transmis). Toutes les touches partent vers le Mac, y compris Win, Alt+Tab et
  Alt+F4.
- **Ctrl+Alt+Fin** : met la capture en pause et rend le clavier à Windows. Un
  clic dans la fenêtre la reprend.
- La touche Windows correspond à Cmd, Alt à Option, AltGr à Option droite.
  `pikasplit-host -swap-ctrl-cmd` fait de Ctrl du PC la touche Cmd (Ctrl+C =
  Cmd+C).
- `-iso=false` sur le host si le Mac utilise une disposition ANSI (US).

## Développement

`go test ./...` tourne sur les deux OS. Hors macOS, `pikasplit-host` se
contente de journaliser les événements reçus, ce qui permet de tester le viewer
sur un seul PC. `PIKASPLIT_DIR` change le dossier de configuration (identité,
appairages).
